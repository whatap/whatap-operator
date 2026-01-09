/*
Copyright 2025 whatapK8s.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	"github.com/whatap/whatap-operator/internal/config"
	"github.com/whatap/whatap-operator/internal/controller"
	webhookmonitoringv2alpha1 "github.com/whatap/whatap-operator/internal/webhook/v2alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	scheme    = runtime.NewScheme()
	setupLog  = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(monitoringv2alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// generateSelfSignedCert creates a CA and a server cert for "serviceName.ns.svc"
func generateSelfSignedCert(serviceName, namespace string) (caCertPEM, caKeyPEM, serverCertPEM, serverKeyPEM []byte, err error) {
	// 1) CA 키·인증서
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	caTpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "whatap-webhook-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1000, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		return
	}
	caCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)})

	// 2) 서버 키·인증서 (CA로 서명)
	srvKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	srvTpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: serviceName + "." + namespace + ".svc"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1000, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{
			serviceName + "." + namespace + ".svc",
			serviceName + "." + namespace + ".svc.cluster.local",
		},
	}
	caCert, _ := x509.ParseCertificate(caDER)
	serverDER, err := x509.CreateCertificate(rand.Reader, srvTpl, caCert, &srvKey.PublicKey, caKey)
	if err != nil {
		return
	}
	serverCertPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER})
	serverKeyPEM = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(srvKey)})

	return
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var enableGpuMemCheck bool
	var tlsOpts []func(*tls.Config)

	//env에서 기본 네임스페이스 읽기
	defaultNS := config.GetWhatapDefaultNamespace()
	if defaultNS == "" {
		// (안전장치) ServiceAccount 토큰에 붙은 파일 경로로도 읽을 수 있음
		if b, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
			defaultNS = strings.TrimSpace(string(b))
		}
	}
	if defaultNS == "" {
		defaultNS = "whatap-monitoring"
	}
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")

	enableGpuMemCheckDefault := false
	if val := os.Getenv("ENABLED_WHATAP_DCGM_EXPORTER_MEMORY_CHECK"); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			enableGpuMemCheckDefault = parsed
		}
	}
	flag.BoolVar(&enableGpuMemCheck, "enable-gpu-memory-check", enableGpuMemCheckDefault,
		"Enable monitoring of dcgm-exporter memory usage and restart pod if needed")

	// Development mode configuration
	// Default is false (Production mode: JSON logging, Info level)
	// Can be enabled via DEBUG or debug env var or --zap-devel flag
	enableDevMode := false
	if val := config.GetDebugMode(); val != "" {
		if parsed, err := strconv.ParseBool(val); err == nil {
			enableDevMode = parsed
		}
	}

	opts := zap.Options{
		Development: enableDevMode,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog.Info("Starting whatap Operator",
		"version", Version,
		"buildTime", BuildTime,
	)
	setupLog.Info("GPU Memory Check Configuration", "ENABLED_WHATAP_DCGM_EXPORTER_MEMORY_CHECK", enableGpuMemCheck)
	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}
	// 1) 인증서 한 번만 생성
	caCert, caKey, serverPEM, serverKeyPEM, err := generateSelfSignedCert("whatap-admission-controller", defaultNS)
	if err != nil {
		setupLog.Error(err, "unable to generate self-signed cert for webhook")
		os.Exit(1)
	}

	// 2) 디스크에 TLS용 cert/key 내려쓰기
	certDir := "/etc/webhook/certs"
	if err := os.MkdirAll(certDir, 0700); err != nil {
		setupLog.Error(err, "unable to create cert directory", "path", certDir)
		os.Exit(1)
	}

	// CA bundle
	caCertFile := filepath.Join(certDir, "ca.crt")
	if err := os.WriteFile(caCertFile, caCert, 0o644); err != nil {
		setupLog.Error(err, "Failed to write caCertFile file", "file", caCertFile)
		os.Exit(1)
	}

	caKeyFile := filepath.Join(certDir, "ca.key")
	if err := os.WriteFile(caKeyFile, caKey, 0o644); err != nil {
		setupLog.Error(err, "Failed to write caKeyFile file", "file", caKeyFile)
		os.Exit(1)
	}

	// server.crt
	certFile := filepath.Join(certDir, "tls.crt")
	if err := os.WriteFile(certFile, serverPEM, 0o600); err != nil {
		setupLog.Error(err, "Failed to write serverCert file", "file", certFile)
		os.Exit(1)
	}

	// server.key
	keyFile := filepath.Join(certDir, "tls.key")
	if err := os.WriteFile(keyFile, serverKeyPEM, 0o644); err != nil {
		setupLog.Error(err, "Failed to write serverKey file", "file", keyFile)
		os.Exit(1)
	}

	// 3) Webhook 서버 생성 (파일 읽기)
	webhookServer := webhook.NewServer(webhook.Options{
		Port:     9443,
		CertDir:  certDir,
		CertName: "tls.crt",
		KeyName:  "tls.key",
		TLSOpts:  tlsOpts, // 필요하다면 동적 갱신 콜백 포함
	})

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		// TODO(user): TLSOpts is used to allow configuring the TLS config used for the server. If certificates are
		// not provided, self-signed certificates will be generated by default. This option is not recommended for
		// production environments as self-signed certificates do not offer the same level of trust and security
		// as certificates issued by a trusted Certificate Authority (CA). The primary risk is potentially allowing
		// unauthorized access to sensitive metrics data. Consider replacing with CertDir, CertName, and KeyName
		// to provide certificates, ensuring the server communicates using trusted and secure certificates.
		TLSOpts: tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "19e8a60c.whatap.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	if err = (&controller.WhatapAgentReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Recorder:         mgr.GetEventRecorderFor("whatap-operator"),
		DefaultNamespace: defaultNS,
		WebhookCABundle:  caCert,
		CaKey:            caKey,
		ServerCert:       serverPEM,
		ServerKey:        serverKeyPEM,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "WhatapAgent")
		os.Exit(1)
	}
	// nolint:goconst
	if config.GetEnableWebhooks() != "false" {
		if err = webhookmonitoringv2alpha1.SetupWhatapAgentWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "WhatapAgent")
			os.Exit(1)
		}
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	if enableGpuMemCheck {
		setupLog.Info("enabling GPU memory check")
		// Create kubernetes clientset
		clientset, err := kubernetes.NewForConfig(mgr.GetConfig())
		if err != nil {
			setupLog.Error(err, "unable to create kubernetes clientset")
			os.Exit(1)
		}

		if err := mgr.Add(&controller.GpuMemChecker{
			ClientSet: clientset,
			Interval:  30 * time.Second,
		}); err != nil {
			setupLog.Error(err, "unable to add GPU memory checker")
			os.Exit(1)
		}
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
