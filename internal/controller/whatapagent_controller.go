package controller

import (
	"context"
	"os"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up Whatap agents and resources")

	// Delete Master Agent Deployment
	if err := r.Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-master-agent", Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete Master Agent Deployment")
		}
	}

	// Delete Node Agent DaemonSet
	if err := r.Delete(ctx, &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-node-agent", Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete Node Agent DaemonSet")
		}
	}

	// Delete OpenAgent resources
	// Delete OpenAgent Deployment
	if err := r.Delete(ctx, &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent", Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent Deployment")
		}
	}

	// Delete OpenAgent ConfigMap
	if err := r.Delete(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-config", Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ConfigMap")
		}
	}

	// Delete OpenAgent ServiceAccount
	if err := r.Delete(ctx, &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-sa", Namespace: r.DefaultNamespace},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ServiceAccount")
		}
	}

	// Delete OpenAgent ClusterRole
	if err := r.Delete(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-role"},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ClusterRole")
		}
	}

	// Delete OpenAgent ClusterRoleBinding
	if err := r.Delete(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "whatap-open-agent-role-binding"},
	}); err != nil {
		// Ignore NotFound errors
		if client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete OpenAgent ClusterRoleBinding")
		}
	}

	logger.Info("Cleanup completed")
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
		mwc.Webhooks = []admissionregistrationv1.MutatingWebhook{
			{
				Name: "mpod.kb.io",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      webhookServiceName,
						Namespace: r.DefaultNamespace,
						Path:      strPtr("/whatap-injection--v1-pod"),
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
				FailurePolicy:           failurePtr(admissionregistrationv1.Ignore),
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             &sideEffectNone,
			},
			{
				Name: "whatapagent.kb.io",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					Service: &admissionregistrationv1.ServiceReference{
						Name:      webhookServiceName,
						Namespace: r.DefaultNamespace,
						Path:      strPtr("/whatap-validation--v2alpha1-whatapagent"),
					},
					CABundle: r.WebhookCABundle,
				},
				Rules: []admissionregistrationv1.RuleWithOperations{{
					Operations: []admissionregistrationv1.OperationType{
						admissionregistrationv1.Create,
						admissionregistrationv1.Update,
					},
					Rule: admissionregistrationv1.Rule{
						APIGroups:   []string{"monitoring.whatap.com"},
						APIVersions: []string{"v2alpha1"},
						Resources:   []string{"whatapagents"},
					},
				}},
				FailurePolicy:           failurePtr(admissionregistrationv1.Ignore),
				AdmissionReviewVersions: []string{"v1"},
				SideEffects:             &sideEffectNone,
			},
		}
		return nil
	})
	return err
}

func strPtr(s string) *string { return &s }
func failurePtr(p admissionregistrationv1.FailurePolicyType) *admissionregistrationv1.FailurePolicyType {
	return &p
}

var sideEffectNone = admissionregistrationv1.SideEffectClassNone

// populateCredentialsFromEnv populates the CR.Spec fields from environment variables if they are empty
func (r *WhatapAgentReconciler) populateCredentialsFromEnv(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) (bool, error) {
	logger := log.FromContext(ctx)
	updated := false

	// Populate License if empty
	if whatapAgent.Spec.License == "" {
		license := os.Getenv("WHATAP_LICENSE")
		if license != "" {
			whatapAgent.Spec.License = license
			logger.Info("Populated License from environment variable", "license", license)
			updated = true
		}
	}

	// Populate Host if empty
	if whatapAgent.Spec.Host == "" {
		host := os.Getenv("WHATAP_HOST")
		if host != "" {
			whatapAgent.Spec.Host = host
			logger.Info("Populated Host from environment variable", "host", host)
			updated = true
		}
	}

	// Populate Port if empty
	if whatapAgent.Spec.Port == "" {
		port := os.Getenv("WHATAP_PORT")
		if port != "" {
			whatapAgent.Spec.Port = port
			logger.Info("Populated Port from environment variable", "port", port)
			updated = true
		}
	}

	// Update the CR if any fields were populated
	if updated {
		if err := r.Update(ctx, whatapAgent); err != nil {
			logger.Error(err, "Failed to update WhatapAgent CR with populated credentials")
			return false, err
		}
		logger.Info("Successfully updated WhatapAgent CR with credentials from environment variables")
	}

	return updated, nil
}

// Reconcile
func (r *WhatapAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	whatapAgent := &monitoringv2alpha1.WhatapAgent{}
	if err := r.Get(ctx, req.NamespacedName, whatapAgent); err != nil {
		logger.Error(err, "Failed to get WhatapAgent CR")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Apply finalizer
	if whatapAgent.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(whatapAgent, whatapFinalizer) {
			controllerutil.AddFinalizer(whatapAgent, whatapFinalizer)
			if err := r.Update(ctx, whatapAgent); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		// The object is being deleted
		if controllerutil.ContainsFinalizer(whatapAgent, whatapFinalizer) {
			// our finalizer is present, so let's handle any external dependency
			if err := r.cleanupAgents(ctx); err != nil {
				logger.Error(err, "Failed to clean up agents")
				// Continue with finalizer removal even if cleanup fails
			}
		}
		// remove our finalizer from the list and update it.
		controllerutil.RemoveFinalizer(whatapAgent, whatapFinalizer)
		if err := r.Update(ctx, whatapAgent); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	logger.Info("Reconciling WhatapAgent", "Name", whatapAgent.Name)

	// Populate credentials from environment variables if they are empty
	if updated, err := r.populateCredentialsFromEnv(ctx, whatapAgent); err != nil {
		logger.Error(err, "Failed to populate credentials from environment variables")
		return ctrl.Result{}, err
	} else if updated {
		// If credentials were updated, requeue to process with the updated CR
		logger.Info("Credentials were populated from environment variables, requeuing")
		return ctrl.Result{Requeue: true}, nil
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

	// Kubernetes Monitoring
	k8sAgentSpec := whatapAgent.Spec.Features.K8sAgent
	openAgentSpec := whatapAgent.Spec.Features.OpenAgent
	// GPU ConfigMap is now created by Helm, so we don't need to create it here
	// if k8sAgentSpec.GpuMonitoring.Enabled {
	// 	logger.Info("createOrUpdate Whatap GPU Monitoring ConfigMap/dcgm-exporter-csv")
	// 	if err := createOrUpdateGpuConfigMap(ctx, r, logger, whatapAgent); err != nil {
	// 		logger.Error(err, "Failed to createOrUpdate GPU Monitoring ConfigMap")
	// 	}
	// }

	if k8sAgentSpec.MasterAgent.Enabled {
		logger.Info("createOrUpdate Whatap Master Agent")
		if err := createOrUpdateMasterAgent(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to createOrUpdate Master Agent")
		}
	}
	if k8sAgentSpec.NodeAgent.Enabled {
		logger.Info("createOrUpdate Whatap Node Agent")
		if err := createOrUpdateNodeAgent(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to createOrUpdate Node Agent")
		}
	}
	if k8sAgentSpec.ApiserverMonitoring.Enabled {
		logger.Info("Installing Apiserver Monitoring Agent")
		if err := installApiserverMonitor(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Apiserver Monitor")
		}
	}
	if k8sAgentSpec.EtcdMonitoring.Enabled {
		logger.Info("Installing Etcd Monitoring Agent")
		if err := installEtcdMonitor(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Etcd Monitor")
		}
	}
	if k8sAgentSpec.SchedulerMonitoring.Enabled {
		logger.Info("Installing Scheduler Monitoring Agent")
		if err := installSchedulerMonitor(ctx, r, logger, whatapAgent); err != nil {
			logger.Error(err, "Failed to install Scheduler Monitor")
		}
	}
	// OpenAgent
	if openAgentSpec.Enabled {
		logger.Info("Installing Open Agent")
		if err := installOpenAgent(ctx, r, logger, whatapAgent); err != nil {
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
