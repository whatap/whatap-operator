package controller

import (
	"context"
	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	webhookServiceName       = "whatap-admission-controller"
	webhookSecretName        = "whatap-webhook-certificate"
	webhookConfigurationName = "whatap-webhook"
	whatapFinalizer          = "whatapagent.finalizers.monitoring.whatap.com"
)

// WhatapAgentReconciler reconciles a WhatapAgent object
type WhatapAgentReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	DefaultNamespace string
	// from main.go
	WebhookCABundle []byte
	CaKey           []byte
	ServerCert      []byte
	ServerKey       []byte
}

func (r *WhatapAgentReconciler) ensureWebhookTLSSecret(ctx context.Context) error {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookSecretName,
			Namespace: r.DefaultNamespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
		secret.Data = map[string][]byte{
			"cert.pem": r.WebhookCABundle, // CA 번들
			"key.pem":  r.CaKey,
			"tls.crt":  r.ServerCert,
			"tls.key":  r.ServerKey,
		}
		return nil
	})
	return err
}

func (r *WhatapAgentReconciler) cleanupAgents(ctx context.Context) error {
	// ex) whatap-master-agent Deployment 삭제
	_ = r.Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-master-agent", Namespace: r.DefaultNamespace},
	})
	_ = r.Delete(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-node-agent", Namespace: r.DefaultNamespace},
	})
	// node-agent DaemonSet, GPU, api-server, etcd, scheduler, openAgent 등도 모두 Delete
	// ignore NotFound 에러
	return nil
}

func (r *WhatapAgentReconciler) ensureWebhookService(ctx context.Context) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      webhookServiceName,
			Namespace: r.DefaultNamespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "whatap-operator",
				"app.kubernetes.io/managed-by": "whatap-operator",
			},
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Spec = corev1.ServiceSpec{
			Selector: map[string]string{
				"app.kubernetes.io/name": "whatap-operator",
			},
			Ports: []corev1.ServicePort{{
				Port: 443,

				TargetPort: intstr.FromInt32(9443),
				Protocol:   corev1.ProtocolTCP,
			}},
		}
		return nil
	})
	return err
}
func (r *WhatapAgentReconciler) ensureMutatingWebhookConfiguration(ctx context.Context) error {
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: webhookConfigurationName,
		},
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, mwc, func() error {
		mwc.Webhooks = []admissionregistrationv1.MutatingWebhook{{
			Name: "mpod.kb.io",
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{
					Name:      webhookServiceName,
					Namespace: r.DefaultNamespace,
					Path:      strPtr("/mutate--v1-pod"),
				},
				CABundle: r.WebhookCABundle,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{""},
					APIVersions: []string{"v1"},
					Resources:   []string{"pods"},
				},
			}},
			FailurePolicy:           failurePtr(admissionregistrationv1.Fail),
			AdmissionReviewVersions: []string{"v1"},
			SideEffects:             &sideEffectNone,
		}}
		return nil
	})
	return err
}

func strPtr(s string) *string { return &s }
func failurePtr(p admissionregistrationv1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	return &p
}

var sideEffectNone = admissionregistrationv1.SideEffectClassNone

// Reconcile
func (r *WhatapAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var whatapAgent monitoringv2alpha1.WhatapAgent
	if err := r.Get(ctx, req.NamespacedName, &whatapAgent); err != nil {
		logger.Error(err, "Failed to get WhatapAgent CR")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("Reconciling WhatapAgent", "Name", whatapAgent.Name)

	// Apply finalizer
	if whatapAgent.DeletionTimestamp.IsZero() && !containsString(whatapAgent.Finalizers, whatapFinalizer) {
		whatapAgent.Finalizers = append(whatapAgent.Finalizers, whatapFinalizer)
		if err := r.Update(ctx, &whatapAgent); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Handle deletion
	if !whatapAgent.ObjectMeta.DeletionTimestamp.IsZero() {
		// 1-1) cleanup: CR가 install한 리소스들 삭제
		if err := r.cleanupAgents(ctx); err != nil {
			return ctrl.Result{}, err
		}
		// 1-2) finalizer 제거
		whatapAgent.Finalizers = removeString(whatapAgent.Finalizers, whatapFinalizer)
		if err := r.Update(ctx, &whatapAgent); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// --- 1. create webhook service
	if err := r.ensureWebhookService(ctx); err != nil {
		logger.Error(err, "failed to ensure ensureWebhookService")
		return ctrl.Result{}, err
	}

	// --- 2. create webhook secret
	if err := r.ensureWebhookTLSSecret(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// 5) WebhookConfiguration 생성/업데이트
	if err := r.ensureMutatingWebhookConfiguration(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// ---  Kubernetes Monitoring  ---
	kubeMon := whatapAgent.Spec.Features.KubernetesMonitoring

	if kubeMon.MasterAgentEnabled == "true" {
		logger.Info("Installing Whatap Master Agent")
		err := installMasterAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Master Agent")
		}
	}

	if kubeMon.NodeAgentEnabled == "true" {
		logger.Info("Installing Whatap Node Agent")
		err := installNodeAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Node Agent")
		}
	}

	if kubeMon.GpuEnabled == "true" {
		logger.Info("Installing GPU Monitoring Agent")
		err := installGpuAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install GPU Agent")
		}
	}
	if kubeMon.ApiserverEnabled == "true" {
		logger.Info("Installing Apiserver Monitoring Agent")
		err := installApiserverMonitor(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Apiserver Monitor")
		}
	}
	if kubeMon.EtcdEnabled == "true" {
		logger.Info("Installing Etcd Monitoring Agent")
		err := installEtcdMonitor(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Etcd Monitor")
		}
	}
	if kubeMon.SchedulerEnabled == "true" {
		logger.Info("Installing Scheduler Monitoring Agent")
		err := installSchedulerMonitor(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Scheduler Monitor")
		}
	}
	if kubeMon.OpenAgentEnabled == "true" {
		logger.Info("Installing Open Agent")
		err := installOpenAgent(ctx, r, logger, whatapAgent)
		if err != nil {
			logger.Error(err, "Failed to install Open Agent")
		}
	}
	return ctrl.Result{}, nil
}

// 헬퍼: 슬라이스에 문자열이 있는지 확인
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// 헬퍼: 슬라이스에서 문자열 제거
func removeString(slice []string, s string) []string {
	result := []string{}
	for _, v := range slice {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}

func (r *WhatapAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		// 1) Watch the cluster-scoped WhatapAgent so CR changes still reconcile
		For(&monitoringv2alpha1.WhatapAgent{}).
		Complete(r)
}
