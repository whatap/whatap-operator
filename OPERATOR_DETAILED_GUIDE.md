# Whatap Operator 상세 가이드

## 목차
1. [에이전트 배포 관리 (Reconciler)](#1-에이전트-배포-관리-reconciler)
2. [APM 자동 주입 (Webhook)](#2-apm-자동-주입-webhook)

---

# 1. 에이전트 배포 관리 (Reconciler)

## 1.1 개요

Reconciler는 WhatapAgent CR(Custom Resource)을 감시하고, CR에 정의된 상태대로 클러스터 리소스를 생성/수정/삭제하는 역할을 합니다.

```
사용자 → CR 생성 → etcd 저장 → Reconciler 감지 → 리소스 생성
```

### 관련 파일

| 파일 | 역할 |
|------|------|
| `internal/controller/whatapagent_controller.go` | Reconcile 루프, 웹훅 인프라 |
| `internal/controller/install_agents.go` | 에이전트 Deployment/DaemonSet 생성 |

---

## 1.2 Reconciler 구조체

**파일:** `internal/controller/whatapagent_controller.go:40-51`

```go
type WhatapAgentReconciler struct {
    client.Client                    // Kubernetes API 클라이언트
    Scheme           *runtime.Scheme  // 타입 스킴 (GVK 매핑)
    Recorder         record.EventRecorder  // 이벤트 기록기
    DefaultNamespace string           // 에이전트 배포 네임스페이스 (기본: whatap-monitoring)

    // 웹훅 TLS 인증서 (main.go에서 생성하여 전달)
    WebhookCABundle []byte  // CA 인증서
    CaKey           []byte  // CA 개인키
    ServerCert      []byte  // 서버 인증서
    ServerKey       []byte  // 서버 개인키
}
```

---

## 1.3 Reconcile 메인 루프

**파일:** `internal/controller/whatapagent_controller.go:362-603`

### 전체 흐름도

```
Reconcile(ctx, req) 호출
         │
         ▼
┌─────────────────────────────────────────┐
│ 1단계: CR 조회                          │
│    r.Get(ctx, req.NamespacedName, cr)   │
└─────────────────────┬───────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────┐
│ 2단계: Finalizer 처리                   │
│    - 생성 시: Finalizer 추가            │
│    - 삭제 시: cleanupAgents() 후 제거   │
└─────────────────────┬───────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────┐
│ 3단계: 상태를 "Progressing"으로 설정    │
└─────────────────────┬───────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────┐
│ 4단계: 웹훅 인프라 생성                 │
│    - ensureWebhookService()             │
│    - ensureWebhookTLSSecret()           │
│    - ensureMutatingWebhookConfiguration()│
└─────────────────────┬───────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────┐
│ 5단계: 에이전트 배포                    │
│    - createOrUpdateMasterAgent()        │
│    - createOrUpdateNodeAgent()          │
│    - installOpenAgent()                 │
└─────────────────────┬───────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────┐
│ 6단계: 상태를 "Available"로 설정        │
│        5분 후 RequeueAfter              │
└─────────────────────────────────────────┘
```

### 코드 상세

```go
func (r *WhatapAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    //──────────────────────────────────────────────────────────
    // 1단계: CR 조회
    //──────────────────────────────────────────────────────────
    whatapAgent := &monitoringv2alpha1.WhatapAgent{}
    if err := r.Get(ctx, req.NamespacedName, whatapAgent); err != nil {
        logger.Error(err, "Failed to get WhatapAgent CR")
        // NotFound 에러는 무시 (이미 삭제된 경우)
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    //──────────────────────────────────────────────────────────
    // 2단계: Finalizer 처리
    //──────────────────────────────────────────────────────────
    if whatapAgent.DeletionTimestamp.IsZero() {
        // CR이 삭제 중이 아님 → Finalizer 추가
        if !controllerutil.ContainsFinalizer(whatapAgent, whatapFinalizer) {
            controllerutil.AddFinalizer(whatapAgent, whatapFinalizer)
            if err := r.Update(ctx, whatapAgent); err != nil {
                return ctrl.Result{}, err
            }
        }
    } else {
        // CR이 삭제 중 → 리소스 정리 후 Finalizer 제거
        if controllerutil.ContainsFinalizer(whatapAgent, whatapFinalizer) {
            if err := r.cleanupAgents(ctx); err != nil {
                logger.Error(err, "Failed to clean up agents")
            }
        }
        controllerutil.RemoveFinalizer(whatapAgent, whatapFinalizer)
        if err := r.Update(ctx, whatapAgent); err != nil {
            return ctrl.Result{}, err
        }
        return ctrl.Result{}, nil
    }

    logger.Info("Reconciling WhatapAgent", "Name", whatapAgent.Name)

    //──────────────────────────────────────────────────────────
    // 3단계: 상태를 "Progressing"으로 설정
    //──────────────────────────────────────────────────────────
    apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
        Type:    "Available",
        Status:  metav1.ConditionFalse,
        Reason:  "Reconciling",
        Message: "Reconciling WhatapAgent resources",
    })
    _ = r.Status().Update(ctx, whatapAgent)

    //──────────────────────────────────────────────────────────
    // 4단계: 웹훅 인프라 생성
    //──────────────────────────────────────────────────────────

    // 4-1. 웹훅 Service 생성
    if err := r.ensureWebhookService(ctx, whatapAgent); err != nil {
        logger.Error(err, "failed to ensure ensureWebhookService")
        r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed",
            "Failed to ensure Webhook Service: "+err.Error())
        return ctrl.Result{}, err
    }

    // 4-2. 웹훅 TLS Secret 생성
    if err := r.ensureWebhookTLSSecret(ctx, whatapAgent); err != nil {
        r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed",
            "Failed to ensure Webhook Secret: "+err.Error())
        return ctrl.Result{}, err
    }

    // 4-3. MutatingWebhookConfiguration 생성
    if err := r.ensureMutatingWebhookConfiguration(ctx, whatapAgent); err != nil {
        r.Recorder.Event(whatapAgent, corev1.EventTypeWarning, "InstallFailed",
            "Failed to ensure MutatingWebhookConfiguration: "+err.Error())
        return ctrl.Result{}, err
    }

    //──────────────────────────────────────────────────────────
    // 5단계: 에이전트 배포
    //──────────────────────────────────────────────────────────
    k8sAgentSpec := whatapAgent.Spec.Features.K8sAgent
    openAgentSpec := whatapAgent.Spec.Features.OpenAgent

    // 5-1. Master Agent
    if k8sAgentSpec.MasterAgent.Enabled {
        logger.V(1).Info("createOrUpdate Whatap Master Agent")
        if err := createOrUpdateMasterAgent(ctx, r, logger, whatapAgent); err != nil {
            logger.Error(err, "Failed to createOrUpdate Master Agent")
            return ctrl.Result{}, err
        }
    } else {
        // 비활성화 시 정리
        logger.V(1).Info("Cleaning up Whatap Master Agent (disabled)")
        if err := r.cleanupMasterAgent(ctx); err != nil {
            logger.Error(err, "Failed to cleanup Master Agent")
        }
    }

    // 5-2. Node Agent
    if k8sAgentSpec.NodeAgent.Enabled {
        logger.V(1).Info("createOrUpdate Whatap Node Agent")
        if err := createOrUpdateNodeAgent(ctx, r, logger, whatapAgent); err != nil {
            logger.Error(err, "Failed to createOrUpdate Node Agent")
            return ctrl.Result{}, err
        }
    } else {
        logger.V(1).Info("Cleaning up Whatap Node Agent (disabled)")
        if err := r.cleanupNodeAgent(ctx); err != nil {
            logger.Error(err, "Failed to cleanup Node Agent")
        }
    }

    // 5-3. 컨트롤플레인 모니터링 (선택)
    if k8sAgentSpec.ApiserverMonitoring.Enabled {
        installApiserverMonitor(ctx, r, logger, whatapAgent)
    }
    if k8sAgentSpec.EtcdMonitoring.Enabled {
        installEtcdMonitor(ctx, r, logger, whatapAgent)
    }
    if k8sAgentSpec.SchedulerMonitoring.Enabled {
        installSchedulerMonitor(ctx, r, logger, whatapAgent)
    }

    // 5-4. OpenAgent
    if openAgentSpec.Enabled {
        logger.V(1).Info("Installing Open Agent")
        if err := installOpenAgent(ctx, r, logger, whatapAgent); err != nil {
            logger.Error(err, "Failed to install Open Agent")
            return ctrl.Result{}, err
        }
    } else {
        logger.V(1).Info("Cleaning up Whatap Open Agent (disabled)")
        if err := r.cleanupOpenAgent(ctx); err != nil {
            logger.Error(err, "Failed to cleanup Open Agent")
        }
    }

    //──────────────────────────────────────────────────────────
    // 6단계: 상태를 "Available"로 설정 & 재큐
    //──────────────────────────────────────────────────────────
    err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
        if err := r.Get(ctx, req.NamespacedName, whatapAgent); err != nil {
            return err
        }
        apimeta.SetStatusCondition(&whatapAgent.Status.Conditions, metav1.Condition{
            Type:    "Available",
            Status:  metav1.ConditionTrue,
            Reason:  "Installed",
            Message: "WhatapAgent installed successfully",
        })
        whatapAgent.Status.ObservedGeneration = whatapAgent.Generation
        return r.Status().Update(ctx, whatapAgent)
    })
    if err != nil {
        logger.Error(err, "Failed to update WhatapAgent status")
        return ctrl.Result{}, err
    }

    // 5분 후 다시 Reconcile (상태 유지 보장)
    return ctrl.Result{RequeueAfter: time.Minute * 5}, nil
}
```

---

## 1.4 Finalizer 상세

### Finalizer란?

CR 삭제 시 관련 리소스를 먼저 정리하기 위한 메커니즘입니다.

```
Finalizer 없이 삭제:
  CR 삭제 → CR 즉시 삭제됨 → 하위 리소스 고아됨 (문제!)

Finalizer 있을 때:
  CR 삭제 요청 → DeletionTimestamp 설정 → Reconcile 호출
                → cleanupAgents() 실행 → Finalizer 제거
                → CR 실제 삭제
```

### cleanupAgents() 함수

**파일:** `internal/controller/whatapagent_controller.go:157-208`

```go
func (r *WhatapAgentReconciler) cleanupAgents(ctx context.Context) error {
    logger := log.FromContext(ctx)
    logger.Info("Cleaning up Whatap agents and resources")

    // 1. Master Agent Deployment 삭제
    r.cleanupMasterAgent(ctx)

    // 2. Node Agent DaemonSet 삭제
    r.cleanupNodeAgent(ctx)

    // 3. OpenAgent 리소스 삭제
    r.cleanupOpenAgent(ctx)

    // 4. MutatingWebhookConfiguration 삭제
    r.Delete(ctx, &admissionregistrationv1.MutatingWebhookConfiguration{
        ObjectMeta: metav1.ObjectMeta{Name: webhookConfigurationName},
    })

    // 5. Webhook Service 삭제
    r.Delete(ctx, &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      webhookServiceName,
            Namespace: r.DefaultNamespace,
        },
    })

    // 6. Webhook Secret 삭제
    r.Delete(ctx, &corev1.Secret{
        ObjectMeta: metav1.ObjectMeta{
            Name:      webhookSecretName,
            Namespace: r.DefaultNamespace,
        },
    })

    logger.Info("Cleanup completed")
    return nil
}
```

---

## 1.5 웹훅 인프라 생성

### ensureWebhookService()

**파일:** `internal/controller/whatapagent_controller.go:210-248`

```go
func (r *WhatapAgentReconciler) ensureWebhookService(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
    svc := &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      webhookServiceName,  // "whatap-admission-controller"
            Namespace: r.DefaultNamespace,
            Labels: map[string]string{
                "app.kubernetes.io/name":       "whatap-operator",
                "app.kubernetes.io/managed-by": "whatap-operator",
            },
        },
    }

    _, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
        // Owner Reference 설정 (CR 삭제 시 같이 삭제)
        if err := controllerutil.SetControllerReference(whatapAgent, svc, r.Scheme); err != nil {
            return err
        }

        svc.Spec = corev1.ServiceSpec{
            Selector: map[string]string{
                "app.kubernetes.io/name": "whatap-operator",
            },
            Ports: []corev1.ServicePort{{
                Port:       443,                      // 외부 포트
                TargetPort: intstr.FromInt32(9443),   // Operator Pod 포트
                Protocol:   corev1.ProtocolTCP,
            }},
        }
        return nil
    })
    return err
}
```

**생성되는 Service:**
```yaml
apiVersion: v1
kind: Service
metadata:
  name: whatap-admission-controller
  namespace: whatap-monitoring
  labels:
    app.kubernetes.io/name: whatap-operator
    app.kubernetes.io/managed-by: whatap-operator
spec:
  selector:
    app.kubernetes.io/name: whatap-operator
  ports:
    - port: 443
      targetPort: 9443
      protocol: TCP
```

### ensureWebhookTLSSecret()

**파일:** `internal/controller/whatapagent_controller.go:53-75`

```go
func (r *WhatapAgentReconciler) ensureWebhookTLSSecret(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
    secret := &corev1.Secret{
        ObjectMeta: metav1.ObjectMeta{
            Name:      webhookSecretName,  // "whatap-webhook-certificate"
            Namespace: r.DefaultNamespace,
        },
    }

    _, err := controllerutil.CreateOrUpdate(ctx, r.Client, secret, func() error {
        if err := controllerutil.SetControllerReference(whatapAgent, secret, r.Scheme); err != nil {
            return err
        }
        secret.Data = map[string][]byte{
            "cert.pem": r.WebhookCABundle,  // CA 인증서
            "key.pem":  r.CaKey,            // CA 개인키
            "tls.crt":  r.ServerCert,       // 서버 인증서
            "tls.key":  r.ServerKey,        // 서버 개인키
        }
        return nil
    })
    return err
}
```

### ensureMutatingWebhookConfiguration()

**파일:** `internal/controller/whatapagent_controller.go:249-325`

```go
func (r *WhatapAgentReconciler) ensureMutatingWebhookConfiguration(ctx context.Context, whatapAgent *monitoringv2alpha1.WhatapAgent) error {
    mwc := &admissionregistrationv1.MutatingWebhookConfiguration{
        ObjectMeta: metav1.ObjectMeta{
            Name: webhookConfigurationName,  // "whatap-webhook"
        },
    }

    _, err := controllerutil.CreateOrUpdate(ctx, r.Client, mwc, func() error {
        if err := controllerutil.SetControllerReference(whatapAgent, mwc, r.Scheme); err != nil {
            return err
        }

        // 1. Pod Mutation Webhook (APM 주입)
        mpod := admissionregistrationv1.MutatingWebhook{
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
                    admissionregistrationv1.Create,  // Pod 생성 시에만
                },
                Rule: admissionregistrationv1.Rule{
                    APIGroups:   []string{""},
                    APIVersions: []string{"v1"},
                    Resources:   []string{"pods"},
                },
            }},
            FailurePolicy:            failurePtr(admissionregistrationv1.Ignore),  // 실패해도 Pod 생성 허용
            AdmissionReviewVersions:  []string{"v1"},
            SideEffects:              &sideEffectNone,
        }

        // 2. WhatapAgent Validation Webhook
        whatap := admissionregistrationv1.MutatingWebhook{
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
            FailurePolicy:            failurePtr(admissionregistrationv1.Ignore),
            AdmissionReviewVersions:  []string{"v1"},
            SideEffects:              &sideEffectNone,
        }

        mwc.Webhooks = []admissionregistrationv1.MutatingWebhook{mpod, whatap}
        return nil
    })
    return err
}
```

---

## 1.6 Master Agent 생성

**파일:** `internal/controller/install_agents.go:147-350`

### 이미지 결정 로직

```go
func createOrUpdateMasterAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
    var img string

    // 우선순위: CustomImageFullName > CustomAgentImageFullName > name+version
    if cr.Spec.Features.K8sAgent.CustomImageFullName != "" {
        img = cr.Spec.Features.K8sAgent.CustomImageFullName
    } else if cr.Spec.Features.K8sAgent.CustomAgentImageFullName != "" {
        img = cr.Spec.Features.K8sAgent.CustomAgentImageFullName
    } else {
        imgName := cr.Spec.Features.K8sAgent.AgentImageName
        if imgName == "" {
            imgName = "public.ecr.aws/whatap/kube_agent"  // 기본값
        }
        ver := cr.Spec.Features.K8sAgent.AgentImageVersion
        if ver == "" {
            ver = "latest"
        }
        img = fmt.Sprintf("%s:%s", imgName, ver)
    }
```

### 리소스 기본값 설정

```go
    // CR에서 리소스 설정 가져오기
    resources := cr.Spec.Features.K8sAgent.MasterAgent.Resources.DeepCopy()

    // 기본값 적용 (설정 안 된 항목만)
    setDefaultResource(resources,
        // Requests 기본값
        corev1.ResourceList{
            corev1.ResourceCPU:    resourceMustParse("100m"),
            corev1.ResourceMemory: resourceMustParse("300Mi"),
        },
        // Limits 기본값
        corev1.ResourceList{
            corev1.ResourceCPU:    resourceMustParse("200m"),
            corev1.ResourceMemory: resourceMustParse("350Mi"),
        },
    )
```

### Deployment 생성

```go
    masterSpec := cr.Spec.Features.K8sAgent.MasterAgent

    deploy := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "whatap-master-agent",
            Namespace: r.DefaultNamespace,
        },
    }

    op, err := controllerutil.CreateOrUpdate(ctx, r.Client, deploy, func() error {
        // 커스텀 Labels 적용
        if masterSpec.Labels != nil {
            if deploy.Labels == nil {
                deploy.Labels = make(map[string]string)
            }
            for k, v := range masterSpec.Labels {
                deploy.Labels[k] = v
            }
        }

        // 커스텀 Annotations 적용
        if masterSpec.Annotations != nil {
            if deploy.Annotations == nil {
                deploy.Annotations = make(map[string]string)
            }
            for k, v := range masterSpec.Annotations {
                deploy.Annotations[k] = v
            }
        }

        // Deployment Spec 설정
        deploy.Spec = appsv1.DeploymentSpec{
            Replicas: int32Ptr(1),
            Selector: &metav1.LabelSelector{
                MatchLabels: map[string]string{
                    "app": "whatap-master-agent",
                },
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{
                        "app": "whatap-master-agent",
                    },
                },
                Spec: corev1.PodSpec{
                    ServiceAccountName: "whatap-master-agent",

                    // 컨테이너 정의
                    Containers: []corev1.Container{{
                        Name:      "whatap-master-agent",
                        Image:     img,
                        Resources: *resources,
                        Env: []corev1.EnvVar{
                            {Name: "WHATAP_LICENSE", Value: license},
                            {Name: "WHATAP_HOST", Value: host},
                            {Name: "WHATAP_PORT", Value: port},
                            // CR에서 추가 환경변수
                        },
                    }},

                    // Tolerations (CR에서 가져옴)
                    Tolerations: masterSpec.Tolerations,

                    // Affinity (CR에서 가져옴)
                    Affinity: masterSpec.Affinity,

                    // NodeSelector (CR에서 가져옴)
                    NodeSelector: masterSpec.NodeSelector,

                    // ImagePullSecrets (프라이빗 레지스트리용)
                    ImagePullSecrets: masterSpec.ImagePullSecrets,

                    // PriorityClassName
                    PriorityClassName: masterSpec.PriorityClassName,
                },
            },
        }

        // Owner Reference 설정
        return controllerutil.SetControllerReference(cr, deploy, r.Scheme)
    })

    logResult(logger, "Deployment", "whatap-master-agent", op)
    return err
}
```

### 생성되는 Deployment 예시

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: whatap-master-agent
  namespace: whatap-monitoring
  ownerReferences:
    - apiVersion: monitoring.whatap.com/v2alpha1
      kind: WhatapAgent
      name: whatap
spec:
  replicas: 1
  selector:
    matchLabels:
      app: whatap-master-agent
  template:
    metadata:
      labels:
        app: whatap-master-agent
    spec:
      serviceAccountName: whatap-master-agent
      containers:
        - name: whatap-master-agent
          image: public.ecr.aws/whatap/kube_agent:latest
          resources:
            requests:
              cpu: 100m
              memory: 300Mi
            limits:
              cpu: 200m
              memory: 350Mi
          env:
            - name: WHATAP_LICENSE
              value: "xxx-xxx-xxx"
            - name: WHATAP_HOST
              value: "whatap.server.com"
            - name: WHATAP_PORT
              value: "6600"
```

---

## 1.7 Node Agent 생성

**파일:** `internal/controller/install_agents.go`

### DaemonSet 생성

```go
func createOrUpdateNodeAgent(ctx context.Context, r *WhatapAgentReconciler, logger logr.Logger, cr *monitoringv2alpha1.WhatapAgent) error {
    nodeSpec := cr.Spec.Features.K8sAgent.NodeAgent

    daemonSet := &appsv1.DaemonSet{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "whatap-node-agent",
            Namespace: r.DefaultNamespace,
        },
    }

    op, err := controllerutil.CreateOrUpdate(ctx, r.Client, daemonSet, func() error {
        daemonSet.Spec = appsv1.DaemonSetSpec{
            Selector: &metav1.LabelSelector{
                MatchLabels: map[string]string{
                    "app": "whatap-node-agent",
                },
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{
                        "app": "whatap-node-agent",
                    },
                },
                Spec: corev1.PodSpec{
                    // 노드 네트워크/PID 접근
                    HostNetwork: boolValueOrDefault(nodeSpec.HostNetwork, true),
                    HostPID:     nodeSpec.HostPID,

                    ServiceAccountName: "whatap-node-agent",

                    Containers: []corev1.Container{
                        // 메인 컨테이너
                        {
                            Name:  "whatap-node-agent",
                            Image: img,
                            Env: []corev1.EnvVar{
                                {Name: "WHATAP_LICENSE", Value: license},
                                {Name: "WHATAP_HOST", Value: host},
                                {Name: "WHATAP_PORT", Value: port},
                                // NODE_NAME, NODE_IP 등 Downward API
                                {
                                    Name: "NODE_NAME",
                                    ValueFrom: &corev1.EnvVarSource{
                                        FieldRef: &corev1.ObjectFieldSelector{
                                            FieldPath: "spec.nodeName",
                                        },
                                    },
                                },
                            },
                            VolumeMounts: []corev1.VolumeMount{
                                // 컨테이너 런타임 소켓
                                {
                                    Name:      "containerd-sock",
                                    MountPath: "/var/run/containerd/containerd.sock",
                                },
                                // 노드 루트 파일시스템
                                {
                                    Name:      "rootfs",
                                    MountPath: "/rootfs",
                                    ReadOnly:  true,
                                },
                                // proc 파일시스템
                                {
                                    Name:      "proc",
                                    MountPath: "/host/proc",
                                    ReadOnly:  true,
                                },
                                // sys 파일시스템
                                {
                                    Name:      "sys",
                                    MountPath: "/host/sys",
                                    ReadOnly:  true,
                                },
                            },
                            SecurityContext: &corev1.SecurityContext{
                                Privileged: boolPtr(true),
                            },
                        },
                        // 헬퍼 컨테이너
                        {
                            Name:  "whatap-node-helper",
                            Image: helperImg,
                            // ...
                        },
                    },

                    Volumes: []corev1.Volume{
                        {
                            Name: "containerd-sock",
                            VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                    Path: getSocketPath(nodeSpec.Runtime),
                                },
                            },
                        },
                        {
                            Name: "rootfs",
                            VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                    Path: "/",
                                },
                            },
                        },
                        {
                            Name: "proc",
                            VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                    Path: "/proc",
                                },
                            },
                        },
                        {
                            Name: "sys",
                            VolumeSource: corev1.VolumeSource{
                                HostPath: &corev1.HostPathVolumeSource{
                                    Path: "/sys",
                                },
                            },
                        },
                    },

                    Tolerations: nodeSpec.Tolerations,
                    Affinity:    nodeSpec.Affinity,
                },
            },
        }

        return controllerutil.SetControllerReference(cr, daemonSet, r.Scheme)
    })

    logResult(logger, "DaemonSet", "whatap-node-agent", op)
    return err
}
```

### 런타임별 소켓 경로

```go
func getSocketPath(runtime string) string {
    switch runtime {
    case "docker":
        return "/var/run/docker.sock"
    case "crio":
        return "/var/run/crio/crio.sock"
    default: // containerd
        return "/var/run/containerd/containerd.sock"
    }
}
```

### 생성되는 DaemonSet 예시

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: whatap-node-agent
  namespace: whatap-monitoring
spec:
  selector:
    matchLabels:
      app: whatap-node-agent
  template:
    metadata:
      labels:
        app: whatap-node-agent
    spec:
      hostNetwork: true
      hostPID: true
      serviceAccountName: whatap-node-agent
      containers:
        - name: whatap-node-agent
          image: public.ecr.aws/whatap/kube_agent:latest
          securityContext:
            privileged: true
          env:
            - name: WHATAP_LICENSE
              value: "xxx-xxx-xxx"
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          volumeMounts:
            - name: containerd-sock
              mountPath: /var/run/containerd/containerd.sock
            - name: rootfs
              mountPath: /rootfs
              readOnly: true
            - name: proc
              mountPath: /host/proc
              readOnly: true
        - name: whatap-node-helper
          # 헬퍼 컨테이너
      volumes:
        - name: containerd-sock
          hostPath:
            path: /var/run/containerd/containerd.sock
        - name: rootfs
          hostPath:
            path: /
        - name: proc
          hostPath:
            path: /proc
```

---

## 1.8 SetupWithManager (워치 설정)

**파일:** `internal/controller/whatapagent_controller.go:626-655`

```go
func (r *WhatapAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
    lp := loggingPredicate(mgr.GetLogger().WithName("event-watcher"))

    return ctrl.NewControllerManagedBy(mgr).
        // 1. WhatapAgent CR 워치 (메인)
        For(&monitoringv2alpha1.WhatapAgent{},
            builder.WithPredicates(predicate.GenerationChangedPredicate{}, lp)).

        // 2. 소유한 리소스 워치 (변경/삭제 시 Reconcile 트리거)
        Owns(&appsv1.Deployment{}, builder.WithPredicates(lp)).
        Owns(&appsv1.DaemonSet{}, builder.WithPredicates(lp)).
        Owns(&corev1.Service{}, builder.WithPredicates(lp)).
        Owns(&corev1.ConfigMap{}, builder.WithPredicates(lp)).
        Owns(&corev1.Secret{}, builder.WithPredicates(lp)).
        Owns(&corev1.ServiceAccount{}, builder.WithPredicates(lp)).
        Owns(&rbacv1.ClusterRole{}, builder.WithPredicates(lp)).
        Owns(&rbacv1.ClusterRoleBinding{}, builder.WithPredicates(lp)).
        Owns(&admissionregistrationv1.MutatingWebhookConfiguration{}, builder.WithPredicates(lp)).

        // 3. WhatapPodMonitor/ServiceMonitor 워치 (변경 시 Reconcile 트리거)
        Watches(
            &monitoringv2alpha1.WhatapPodMonitor{},
            handler.EnqueueRequestsFromMapFunc(r.findWhatapAgents),
            builder.WithPredicates(lp),
        ).
        Watches(
            &monitoringv2alpha1.WhatapServiceMonitor{},
            handler.EnqueueRequestsFromMapFunc(r.findWhatapAgents),
            builder.WithPredicates(lp),
        ).
        Complete(r)
}
```

### 워치 동작 설명

```
┌─────────────────────────────────────────────────────────────┐
│                    Controller 워치                          │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  For(WhatapAgent)                                           │
│    └─ CR 변경 시 → Reconcile() 호출                         │
│                                                             │
│  Owns(Deployment, DaemonSet, ...)                           │
│    └─ 소유한 리소스 변경/삭제 시 → Reconcile() 호출         │
│    └─ 예: 누군가 whatap-master-agent 삭제 → 재생성          │
│                                                             │
│  Watches(WhatapPodMonitor, WhatapServiceMonitor)            │
│    └─ 모니터 리소스 변경 시 → OpenAgent 설정 재생성         │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## 1.9 CreateOrUpdate 패턴

모든 리소스 생성에서 사용하는 핵심 패턴:

```go
op, err := controllerutil.CreateOrUpdate(ctx, r.Client, resource, func() error {
    // 리소스 스펙 설정
    resource.Spec = ...

    // Owner Reference 설정
    return controllerutil.SetControllerReference(cr, resource, r.Scheme)
})
```

### 동작 방식

```
CreateOrUpdate 호출
        │
        ▼
리소스가 존재하는가?
        │
   ┌────┴────┐
   │         │
  No        Yes
   │         │
   ▼         ▼
Create    Compare
   │         │
   │    변경됐는가?
   │    ┌───┴───┐
   │   No      Yes
   │    │       │
   │    ▼       ▼
   │  Skip   Update
   │    │       │
   └────┴───────┘
        │
        ▼
    결과 반환
    (Created/Updated/Unchanged)
```

---

# 2. APM 자동 주입 (Webhook)

## 2.1 개요

Webhook은 Pod 생성 요청을 가로채서 APM 에이전트를 자동으로 주입하는 역할을 합니다.

```
Pod 생성 요청 → API Server → Webhook → Pod 수정 → etcd 저장
```

### 관련 파일

| 파일 | 역할 |
|------|------|
| `internal/webhook/v2alpha1/whatapagent_webhook.go` | 웹훅 등록 및 메인 핸들러 |
| `internal/webhook/v2alpha1/process_deployments.go` | Init Container 생성 |
| `internal/webhook/v2alpha1/injector_java.go` | Java APM 주입 |
| `internal/webhook/v2alpha1/injector_python.go` | Python APM 주입 |
| `internal/webhook/v2alpha1/injector_nodejs.go` | Node.js APM 주입 |
| `internal/webhook/v2alpha1/utils.go` | 유틸리티 함수 |
| `internal/webhook/v2alpha1/constants.go` | 상수 정의 |

---

## 2.2 웹훅 등록

**파일:** `internal/webhook/v2alpha1/whatapagent_webhook.go`

### SetupWebhookWithManager

```go
func SetupWhatapAgentWebhookWithManager(mgr ctrl.Manager) error {
    // Pod Mutation Webhook 등록
    mgr.GetWebhookServer().Register(
        "/whatap-injection--v1-pod",
        &webhook.Admission{
            Handler: &WhatapAgentCustomDefaulter{
                Client: mgr.GetClient(),
            },
        },
    )

    // WhatapAgent Validation Webhook 등록
    mgr.GetWebhookServer().Register(
        "/whatap-validation--v2alpha1-whatapagent",
        &webhook.Admission{
            Handler: &WhatapAgentCustomValidator{
                Client: mgr.GetClient(),
            },
        },
    )

    return nil
}
```

---

## 2.3 Pod Mutation Webhook

### WhatapAgentCustomDefaulter 구조체

```go
type WhatapAgentCustomDefaulter struct {
    Client client.Client
}

// webhook.CustomDefaulter 인터페이스 구현
func (d *WhatapAgentCustomDefaulter) Default(ctx context.Context, obj runtime.Object) error {
    pod, ok := obj.(*corev1.Pod)
    if !ok {
        return fmt.Errorf("expected a Pod but got %T", obj)
    }

    return d.mutatePod(ctx, pod)
}
```

### mutatePod 메인 로직

```go
func (d *WhatapAgentCustomDefaulter) mutatePod(ctx context.Context, pod *corev1.Pod) error {
    logger := log.FromContext(ctx)

    //──────────────────────────────────────────────────────────
    // 1. WhatapAgent CR 조회
    //──────────────────────────────────────────────────────────
    whatapAgent := &monitoringv2alpha1.WhatapAgent{}
    if err := d.Client.Get(ctx, types.NamespacedName{Name: "whatap"}, whatapAgent); err != nil {
        logger.Error(err, "Failed to get WhatapAgent CR")
        return nil  // CR 없으면 주입 안 함
    }

    //──────────────────────────────────────────────────────────
    // 2. APM Instrumentation 활성화 확인
    //──────────────────────────────────────────────────────────
    apmSpec := whatapAgent.Spec.Features.APM
    if apmSpec == nil || !apmSpec.Instrumentation.Enabled {
        return nil  // 비활성화면 주입 안 함
    }

    //──────────────────────────────────────────────────────────
    // 3. 각 Target에 대해 매칭 확인 및 주입
    //──────────────────────────────────────────────────────────
    for _, target := range apmSpec.Instrumentation.Targets {
        if !target.Enabled {
            continue
        }

        // 3-1. Namespace 매칭 확인
        if !matchesNamespace(pod.Namespace, target.NamespaceSelector) {
            continue
        }

        // 3-2. Pod Label 매칭 확인
        if !matchesPodSelector(pod.Labels, target.PodSelector) {
            continue
        }

        logger.Info("Pod matches target, injecting APM agent",
            "pod", pod.Name,
            "namespace", pod.Namespace,
            "target", target.Name,
            "language", target.Language)

        // 3-3. Init Container 생성 및 주입
        initContainer := createAgentInitContainer(target, whatapAgent)
        pod.Spec.InitContainers = append(pod.Spec.InitContainers, initContainer)

        // 3-4. Volume 추가
        pod.Spec.Volumes = appendIfNotExists(pod.Spec.Volumes, corev1.Volume{
            Name: VolumeNameWhatapAgent,
            VolumeSource: corev1.VolumeSource{
                EmptyDir: &corev1.EmptyDirVolumeSource{},
            },
        })

        // 3-5. 각 컨테이너에 환경변수 및 VolumeMount 주입
        for i := range pod.Spec.Containers {
            injectEnvAndVolume(&pod.Spec.Containers[i], target, whatapAgent, logger)
        }

        break  // 첫 번째 매칭된 타겟만 적용
    }

    return nil
}
```

---

## 2.4 Namespace/Pod 셀렉터 매칭

**파일:** `internal/webhook/v2alpha1/utils.go`

### Namespace 매칭

```go
func matchesNamespace(namespace string, selector *monitoringv2alpha1.NamespaceSelector) bool {
    if selector == nil {
        return true  // 셀렉터 없으면 모든 네임스페이스 매칭
    }

    // matchNames 확인
    if len(selector.MatchNames) > 0 {
        for _, name := range selector.MatchNames {
            if name == namespace {
                return true
            }
        }
        return false
    }

    // matchLabels는 네임스페이스 레이블 조회 필요 (생략)

    return true
}
```

### Pod Label 매칭

```go
func matchesPodSelector(podLabels map[string]string, selector *monitoringv2alpha1.PodSelector) bool {
    if selector == nil {
        return true
    }

    // matchLabels 확인
    for key, value := range selector.MatchLabels {
        if podLabels[key] != value {
            return false
        }
    }

    // matchExpressions 확인
    for _, expr := range selector.MatchExpressions {
        if !matchesLabelExpression(podLabels, expr) {
            return false
        }
    }

    return true
}

func matchesLabelExpression(labels map[string]string, expr monitoringv2alpha1.LabelSelectorRequirement) bool {
    value, exists := labels[expr.Key]

    switch expr.Operator {
    case "In":
        for _, v := range expr.Values {
            if value == v {
                return true
            }
        }
        return false
    case "NotIn":
        for _, v := range expr.Values {
            if value == v {
                return false
            }
        }
        return true
    case "Exists":
        return exists
    case "DoesNotExist":
        return !exists
    }

    return false
}
```

---

## 2.5 Init Container 생성

**파일:** `internal/webhook/v2alpha1/process_deployments.go`

```go
func createAgentInitContainer(target monitoringv2alpha1.APMTarget, cr *monitoringv2alpha1.WhatapAgent) corev1.Container {
    // 이미지 결정
    image := getAgentImage(target)

    // 환경변수 설정
    env := []corev1.EnvVar{
        {
            Name:  "WHATAP_LICENSE",
            Value: getWhatapLicense(cr),
        },
        {
            Name:  "WHATAP_HOST",
            Value: getWhatapHost(cr),
        },
        {
            Name:  "WHATAP_PORT",
            Value: getWhatapPort(cr),
        },
    }

    // 보안 컨텍스트 설정
    securityContext := getInitContainerSecurityContext(target, cr)

    return corev1.Container{
        Name:            InitContainerName,  // "whatap-agent-init"
        Image:           image,
        ImagePullPolicy: corev1.PullIfNotPresent,
        Env:             env,
        VolumeMounts: []corev1.VolumeMount{
            {
                Name:      VolumeNameWhatapAgent,  // "whatap-agent-volume"
                MountPath: MountPathWhatapAgent,   // "/whatap-agent"
            },
        },
        SecurityContext: securityContext,
    }
}
```

### 이미지 결정 로직

```go
func getAgentImage(target monitoringv2alpha1.APMTarget) string {
    // 커스텀 이미지가 지정된 경우
    if target.CustomImageFullName != "" {
        return target.CustomImageFullName
    }

    // 언어별 기본 이미지
    version := target.WhatapApmVersions[target.Language]
    if version == "" {
        version = "latest"
    }

    switch target.Language {
    case "java":
        return fmt.Sprintf("whatap/apm-java:%s", version)
    case "python":
        return fmt.Sprintf("whatap/apm-python:%s", version)
    case "nodejs":
        return fmt.Sprintf("whatap/apm-nodejs:%s", version)
    case "php":
        return fmt.Sprintf("whatap/apm-php:%s", version)
    case "dotnet":
        return fmt.Sprintf("whatap/apm-dotnet:%s", version)
    case "golang":
        return fmt.Sprintf("whatap/apm-golang:%s", version)
    default:
        return fmt.Sprintf("whatap/apm-%s:%s", target.Language, version)
    }
}
```

### 보안 컨텍스트 결정

```go
func getInitContainerSecurityContext(target monitoringv2alpha1.APMTarget, cr *monitoringv2alpha1.WhatapAgent) *corev1.SecurityContext {
    // 타겟 레벨 설정 우선
    if target.InitContainerSecurity != nil {
        return &corev1.SecurityContext{
            RunAsNonRoot: target.InitContainerSecurity.RunAsNonRoot,
            RunAsUser:    target.InitContainerSecurity.RunAsUser,
        }
    }

    // Instrumentation 레벨 설정
    instrSec := cr.Spec.Features.APM.Instrumentation.InitContainerSecurity
    if instrSec != nil {
        return &corev1.SecurityContext{
            RunAsNonRoot: instrSec.RunAsNonRoot,
            RunAsUser:    instrSec.RunAsUser,
        }
    }

    // 기본값: non-root (OpenShift 호환)
    return &corev1.SecurityContext{
        RunAsNonRoot: boolPtr(true),
    }
}
```

---

## 2.6 Java APM 주입

**파일:** `internal/webhook/v2alpha1/injector_java.go`

```go
func injectJavaEnvVars(container *corev1.Container, target monitoringv2alpha1.APMTarget, cr *monitoringv2alpha1.WhatapAgent, logger logr.Logger) {
    // 1. VolumeMount 추가
    container.VolumeMounts = appendVolumeMountIfNotExists(container.VolumeMounts, corev1.VolumeMount{
        Name:      VolumeNameWhatapAgent,
        MountPath: MountPathWhatapAgent,
    })

    // 2. 환경변수 준비
    envVars := []corev1.EnvVar{
        // JAVA_TOOL_OPTIONS: JVM 에이전트 설정
        {
            Name:  "JAVA_TOOL_OPTIONS",
            Value: "-javaagent:/whatap-agent/whatap.agent.java.jar",
        },
        // Whatap 설정
        {
            Name:  "license",
            Value: getWhatapLicense(cr),
        },
        {
            Name:  "whatap.server.host",
            Value: getWhatapHost(cr),
        },
        {
            Name:  "whatap.server.port",
            Value: getWhatapPort(cr),
        },
        {
            Name:  "whatap.micro.enabled",
            Value: "true",
        },
        // Kubernetes 메타데이터 (Downward API)
        {
            Name: "NODE_IP",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "status.hostIP",
                },
            },
        },
        {
            Name: "NODE_NAME",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "spec.nodeName",
                },
            },
        },
        {
            Name: "POD_NAME",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "metadata.name",
                },
            },
        },
    }

    // 3. 타겟에서 추가 환경변수
    for _, env := range target.Envs {
        envVars = append(envVars, env)
    }

    // 4. 기존 환경변수와 병합 (기존 값 유지)
    container.Env = mergeEnvVars(container.Env, envVars)

    logger.Info("Injected Java APM environment variables", "container", container.Name)
}
```

### 환경변수 병합 로직

```go
func mergeEnvVars(existing, toAdd []corev1.EnvVar) []corev1.EnvVar {
    result := make([]corev1.EnvVar, len(existing))
    copy(result, existing)

    existingNames := make(map[string]bool)
    for _, env := range existing {
        existingNames[env.Name] = true
    }

    // 기존에 없는 것만 추가 (기존 값 덮어쓰지 않음)
    for _, env := range toAdd {
        if !existingNames[env.Name] {
            result = append(result, env)
        }
    }

    return result
}
```

---

## 2.7 Python APM 주입

**파일:** `internal/webhook/v2alpha1/injector_python.go`

```go
func injectPythonEnvVars(container *corev1.Container, target monitoringv2alpha1.APMTarget, cr *monitoringv2alpha1.WhatapAgent, version string, logger logr.Logger) {
    // 1. VolumeMount 추가
    container.VolumeMounts = appendVolumeMountIfNotExists(container.VolumeMounts, corev1.VolumeMount{
        Name:      VolumeNameWhatapAgent,
        MountPath: MountPathWhatapAgent,
    })

    // 2. 앱 설정 읽기 (타겟 환경변수에서)
    appName := getEnvValue(target.Envs, "app_name")
    if appName == "" {
        appName = container.Name
    }
    appProcessName := getEnvValue(target.Envs, "app_process_name")

    // 3. 환경변수 준비
    envVars := []corev1.EnvVar{
        // Whatap 설정
        {
            Name:  "license",
            Value: getWhatapLicense(cr),
        },
        {
            Name:  "whatap_server_host",
            Value: getWhatapHost(cr),
        },
        {
            Name:  "whatap_server_port",
            Value: getWhatapPort(cr),
        },
        // 앱 설정
        {
            Name:  "app_name",
            Value: appName,
        },
        {
            Name:  "app_process_name",
            Value: appProcessName,
        },
        // Python 에이전트 경로
        {
            Name:  "WHATAP_HOME",
            Value: "/whatap-agent",
        },
        // PYTHONPATH에 whatap 부트스트랩 추가
        {
            Name:  "PYTHONPATH",
            Value: "/whatap-agent/whatap/bootstrap:$(PYTHONPATH)",
        },
        {
            Name:  "whatap.micro.enabled",
            Value: "true",
        },
        // Kubernetes 메타데이터
        {
            Name: "NODE_IP",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "status.hostIP",
                },
            },
        },
        {
            Name: "NODE_NAME",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "spec.nodeName",
                },
            },
        },
        {
            Name: "POD_NAME",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "metadata.name",
                },
            },
        },
    }

    // 4. 기존 환경변수와 병합
    container.Env = mergeEnvVars(container.Env, envVars)

    logger.Info("Injected Python APM environment variables", "container", container.Name)
}
```

---

## 2.8 Node.js APM 주입

**파일:** `internal/webhook/v2alpha1/injector_nodejs.go`

```go
func injectNodejsEnvVars(container *corev1.Container, target monitoringv2alpha1.APMTarget, cr *monitoringv2alpha1.WhatapAgent, logger logr.Logger) {
    // 1. VolumeMount 추가
    container.VolumeMounts = appendVolumeMountIfNotExists(container.VolumeMounts, corev1.VolumeMount{
        Name:      VolumeNameWhatapAgent,
        MountPath: MountPathWhatapAgent,
    })

    // 2. 환경변수 준비
    envVars := []corev1.EnvVar{
        {
            Name:  "WHATAP_LICENSE",
            Value: getWhatapLicense(cr),
        },
        {
            Name:  "WHATAP_SERVER_HOST",
            Value: getWhatapHost(cr),
        },
        {
            Name:  "WHATAP_SERVER_PORT",
            Value: getWhatapPort(cr),
        },
        {
            Name:  "whatap.micro.enabled",
            Value: "true",
        },
        // Kubernetes 메타데이터
        {
            Name: "NODE_IP",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "status.hostIP",
                },
            },
        },
        {
            Name: "NODE_NAME",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "spec.nodeName",
                },
            },
        },
        {
            Name: "POD_NAME",
            ValueFrom: &corev1.EnvVarSource{
                FieldRef: &corev1.ObjectFieldSelector{
                    FieldPath: "metadata.name",
                },
            },
        },
    }

    // 3. 기존 환경변수와 병합
    container.Env = mergeEnvVars(container.Env, envVars)

    logger.Info("Injected Node.js APM environment variables", "container", container.Name)
}
```

---

## 2.9 언어별 주입 분기

**파일:** `internal/webhook/v2alpha1/whatapagent_webhook.go`

```go
func injectEnvAndVolume(container *corev1.Container, target monitoringv2alpha1.APMTarget, cr *monitoringv2alpha1.WhatapAgent, logger logr.Logger) {
    switch target.Language {
    case "java":
        injectJavaEnvVars(container, target, cr, logger)
    case "python":
        version := target.WhatapApmVersions["python"]
        injectPythonEnvVars(container, target, cr, version, logger)
    case "nodejs":
        injectNodejsEnvVars(container, target, cr, logger)
    case "php":
        injectPhpEnvVars(container, target, cr, logger)
    case "dotnet":
        injectDotnetEnvVars(container, target, cr, logger)
    case "golang":
        injectGolangEnvVars(container, target, cr, logger)
    default:
        logger.Info("Unknown language, skipping injection", "language", target.Language)
    }
}
```

---

## 2.10 상수 정의

**파일:** `internal/webhook/v2alpha1/constants.go`

```go
package v2alpha1

const (
    // Init Container 이름
    InitContainerName = "whatap-agent-init"

    // Volume 이름
    VolumeNameWhatapAgent = "whatap-agent-volume"

    // Mount 경로
    MountPathWhatapAgent = "/whatap-agent"

    // 환경변수 이름
    EnvWhatapLicense = "WHATAP_LICENSE"
    EnvWhatapHost    = "WHATAP_HOST"
    EnvWhatapPort    = "WHATAP_PORT"
    EnvNodeIP        = "NODE_IP"
    EnvNodeName      = "NODE_NAME"
    EnvPodName       = "POD_NAME"

    // Java 전용
    EnvJavaToolOptions = "JAVA_TOOL_OPTIONS"

    // Python 전용
    EnvPythonPath  = "PYTHONPATH"
    EnvWhatapHome  = "WHATAP_HOME"
    EnvAppName     = "app_name"
)
```

---

## 2.11 전체 주입 흐름도

```
┌─────────────────────────────────────────────────────────────┐
│              kubectl apply -f my-java-app.yaml              │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                      API Server                             │
│                          │                                  │
│   AdmissionReview 생성   │                                  │
│                          ▼                                  │
│   POST /whatap-injection--v1-pod                           │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│           WhatapAgentCustomDefaulter.Default()              │
│                                                             │
│   1. WhatapAgent CR 조회                                    │
│      └─ client.Get("whatap")                               │
│                                                             │
│   2. APM 활성화 확인                                        │
│      └─ apmSpec.Instrumentation.Enabled == true?           │
│                                                             │
│   3. 타겟 매칭                                              │
│      ├─ matchesNamespace(pod.Namespace, selector)          │
│      └─ matchesPodSelector(pod.Labels, selector)           │
│                                                             │
│   4. 매칭 시 주입                                           │
│      ├─ createAgentInitContainer()  → InitContainer 추가   │
│      ├─ appendVolume()              → Volume 추가          │
│      └─ injectJavaEnvVars()         → 환경변수 주입        │
│                                                             │
└─────────────────────────┬───────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────┐
│                   수정된 Pod 반환                           │
│                                                             │
│   Before:                    After:                         │
│   spec:                      spec:                          │
│     containers:                initContainers:              │
│       - name: app                - name: whatap-agent-init  │
│         image: my-app              image: whatap/apm-java   │
│                                    volumeMounts:            │
│                                      - whatap-agent-volume  │
│                                containers:                  │
│                                  - name: app                │
│                                    image: my-app            │
│                                    env:                     │
│                                      - JAVA_TOOL_OPTIONS    │
│                                      - license              │
│                                      - whatap.server.host   │
│                                    volumeMounts:            │
│                                      - whatap-agent-volume  │
│                                volumes:                     │
│                                  - name: whatap-agent-volume│
│                                    emptyDir: {}             │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

## 2.12 WhatapAgent Validation Webhook

**파일:** `internal/webhook/v2alpha1/whatapagent_webhook.go`

```go
type WhatapAgentCustomValidator struct {
    Client client.Client
}

func (v *WhatapAgentCustomValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
    cr, ok := obj.(*monitoringv2alpha1.WhatapAgent)
    if !ok {
        return nil, fmt.Errorf("expected WhatapAgent but got %T", obj)
    }

    return v.validate(cr)
}

func (v *WhatapAgentCustomValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
    cr, ok := newObj.(*monitoringv2alpha1.WhatapAgent)
    if !ok {
        return nil, fmt.Errorf("expected WhatapAgent but got %T", newObj)
    }

    return v.validate(cr)
}

func (v *WhatapAgentCustomValidator) validate(cr *monitoringv2alpha1.WhatapAgent) (admission.Warnings, error) {
    var warnings admission.Warnings
    var errs []error

    // 1. 언어 검증
    validLanguages := map[string]bool{
        "java": true, "python": true, "nodejs": true,
        "php": true, "dotnet": true, "golang": true,
    }

    for _, target := range cr.Spec.Features.APM.Instrumentation.Targets {
        if !validLanguages[target.Language] {
            errs = append(errs, fmt.Errorf("invalid language '%s' in target '%s'", target.Language, target.Name))
        }
    }

    // 2. 런타임 검증
    validRuntimes := map[string]bool{
        "containerd": true, "docker": true, "crio": true,
    }

    runtime := cr.Spec.Features.K8sAgent.NodeAgent.Runtime
    if runtime != "" && !validRuntimes[runtime] {
        errs = append(errs, fmt.Errorf("invalid runtime '%s'", runtime))
    }

    // 3. 에러 반환
    if len(errs) > 0 {
        return warnings, fmt.Errorf("validation failed: %v", errs)
    }

    return warnings, nil
}
```

---

## 2.13 CR 설정 예시

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "xxx-xxx-xxx"
  host: "whatap.server.com"
  port: "6600"

  features:
    apm:
      instrumentation:
        enabled: true

        # Init Container 보안 설정 (전체 적용)
        initContainerSecurity:
          runAsNonRoot: true

        targets:
          # Java 앱 타겟
          - name: java-services
            enabled: true
            language: java
            whatapApmVersions:
              java: "2.2.58"

            # 네임스페이스 선택
            namespaceSelector:
              matchNames:
                - production
                - staging

            # Pod 레이블 선택
            podSelector:
              matchLabels:
                app.kubernetes.io/part-of: my-app
                apm: enabled

            # 추가 환경변수
            envs:
              - name: whatap.trace.activestack.enabled
                value: "true"

          # Python 앱 타겟
          - name: python-services
            enabled: true
            language: python
            whatapApmVersions:
              python: "1.6.2"
            namespaceSelector:
              matchLabels:
                env: production
            podSelector:
              matchLabels:
                language: python
            envs:
              - name: app_name
                value: "my-python-app"
              - name: app_process_name
                value: "gunicorn"

          # Node.js 앱 타겟
          - name: nodejs-services
            enabled: true
            language: nodejs
            customImageFullName: "my-registry.com/whatap/apm-nodejs:custom"
            namespaceSelector:
              matchNames:
                - frontend
            podSelector:
              matchExpressions:
                - key: app
                  operator: In
                  values:
                    - web-frontend
                    - api-gateway
```

---

## 요약

### Reconciler (에이전트 배포 관리)

| 단계 | 동작 | 생성 리소스 |
|------|------|------------|
| CR 조회 | WhatapAgent CR 가져오기 | - |
| Finalizer | 삭제 시 정리 보장 | - |
| 웹훅 인프라 | Service, Secret, WebhookConfig | Service, Secret, MutatingWebhookConfiguration |
| Master Agent | Deployment 생성 | Deployment (Pod 1개) |
| Node Agent | DaemonSet 생성 | DaemonSet (노드당 Pod 1개) |
| OpenAgent | Deployment + RBAC 생성 | Deployment, SA, ClusterRole, ClusterRoleBinding, ConfigMap |
| 상태 업데이트 | Available 설정, 5분 후 재큐 | - |

### Webhook (APM 자동 주입)

| 단계 | 동작 |
|------|------|
| Pod 생성 요청 가로채기 | API Server → Webhook |
| CR 조회 | WhatapAgent CR에서 설정 읽기 |
| 타겟 매칭 | Namespace, Pod Label 확인 |
| Init Container 추가 | whatap-agent-init 컨테이너 |
| Volume 추가 | whatap-agent-volume (emptyDir) |
| 환경변수 주입 | 언어별 (Java, Python, Node.js) |
| 수정된 Pod 반환 | API Server로 반환 → etcd 저장 |

---

*문서 생성일: 2026-01-26*
