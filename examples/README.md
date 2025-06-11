# Whatap Operator Examples

This directory contains example configurations for the Whatap Operator. These examples demonstrate different ways to configure the Whatap monitoring solution in your Kubernetes cluster.

## WhatapAgent Examples

The `whatapagent` directory contains example configurations for the `WhatapAgent` custom resource, which is used to deploy and configure Whatap monitoring agents.

### Basic Configuration

[whatap-agent-basic.yaml](whatapagent/whatap-agent-basic.yaml) - A minimal configuration that enables the Whatap master agent and node agent for basic Kubernetes monitoring.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

### Template with Explanatory Comments

[whatap-agent-template.yaml](whatapagent/whatap-agent-template.yaml) - A template configuration with commented-out sections for all features, including detailed explanatory comments for each option.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    ### APM 자동 설치 사용시 주석 해제 - APM 에이전트를 애플리케이션 Pod에 자동으로 주입하여 애플리케이션 성능 모니터링을 활성화합니다.
    # apm:
    #   instrumentation:
    #     targets:
    #       - name: hello-world
    #         enabled: true
    #         language: "java"          # 지원 언어: java, python, php, dotnet, nodejs, golang
    #         whatapApmVersions:
    #           java: "2.2.58"          # 사용할 APM 에이전트 버전
    # ... (see the file for complete template with comments)
```

This template is designed to help you understand the purpose of each configuration section with inline comments. Uncomment the sections you need and customize them according to your requirements.

### APM Instrumentation Only

[whatap-agent-apm-only.yaml](whatapagent/whatap-agent-apm-only.yaml) - Configures APM instrumentation for a Java application without enabling the Kubernetes monitoring agents.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    apm:
      instrumentation:
        targets:
          - name: hello-world
            enabled: true
            language: "java"
            whatapApmVersions:
              java: "2.2.58"
            namespaceSelector:
              matchNames:
                - default
            podSelector:
              matchLabels:
                app: "hello-world"
            config:
              mode: default
```

### OpenAgent Only

[whatap-agent-openagent-only.yaml](whatapagent/whatap-agent-openagent-only.yaml) - Configures only the OpenAgent component for collecting Prometheus-style metrics without enabling the Kubernetes monitoring agents or APM instrumentation.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    openAgent:
        enabled: true
        globalInterval: "60s"
        globalPath: "/metrics"
        targets:
          - targetName: kube-apiserver
            type: ServiceMonitor
            namespaceSelector:
              matchNames:
                - "default"
            selector:
              matchLabels:
                component: apiserver
                provider: kubernetes
            endpoints:
              - port: "https"
                path: "/metrics"
                interval: "30s"
                scheme: "https"
                tlsConfig:
                  insecureSkipVerify: true
            metricRelabelConfigs:
              - source_labels: ["__name__"]
                regex: "apiserver_request_total"
                action: "keep"
```

### Kubernetes Monitoring with APM Instrumentation

[whatap-agent-k8s-apm.yaml](whatapagent/whatap-agent-k8s-apm.yaml) - Combines Kubernetes monitoring with APM instrumentation for a Java application.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    apm:
      instrumentation:
        targets:
          - name: hello-world
            enabled: true
            language: "java"
            whatapApmVersions:
              java: "2.2.58"
            namespaceSelector:
              matchNames:
                - default
            podSelector:
              matchLabels:
                app: "hello-world"
            config:
              mode: default
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

### Complete Configuration

[whatap-agent-complete.yaml](whatapagent/whatap-agent-complete.yaml) - A comprehensive configuration that enables Kubernetes monitoring, APM instrumentation, and OpenMetric collection.

```yaml
# See the file for the complete example
```

### K8s Agent with Custom Resource Requirements

[whatap-agent-k8s-resources.yaml](whatapagent/whatap-agent-k8s-resources.yaml) - Configures custom resource requirements (CPU and memory) for the Whatap master agent and node agent.

#### Understanding Kubernetes Resource Requirements

In Kubernetes, resource requirements help the scheduler make decisions about which nodes to place pods on and ensure that pods have the resources they need to run effectively:

- **Requests**: The minimum amount of resources that the container needs. The scheduler uses this to find a node with enough resources available.
- **Limits**: The maximum amount of resources that the container can use. This prevents a container from using more than its fair share of resources on a node.

Setting appropriate resource requirements is important for:
- Ensuring stable performance of monitoring agents
- Preventing resource contention with other workloads
- Optimizing resource utilization across your cluster

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    k8sAgent:
      masterAgent:
        enabled: true
        resources:
          requests:
            cpu: "200m"        # Request 200 millicores (0.2 CPU cores)
            memory: "400Mi"    # Request 400 MiB of memory
          limits:
            cpu: "500m"        # Limit to 500 millicores (0.5 CPU cores)
            memory: "600Mi"    # Limit to 600 MiB of memory
      nodeAgent:
        enabled: true
        resources:
          requests:
            cpu: "150m"        # Request 150 millicores (0.15 CPU cores)
            memory: "350Mi"    # Request 350 MiB of memory
          limits:
            cpu: "300m"        # Limit to 300 millicores (0.3 CPU cores)
            memory: "500Mi"    # Limit to 500 MiB of memory
```

If you don't specify resource requirements, the Whatap Operator will apply default values to ensure the agents have sufficient resources to operate properly. The default values are:

- Master Agent:
  - Requests: CPU: 100m, Memory: 300Mi
  - Limits: CPU: 200m, Memory: 350Mi

- Node Agent:
  - Requests: CPU: 100m, Memory: 300Mi
  - Limits: CPU: 200m, Memory: 350Mi

### K8s Agent with Custom Tolerations

[whatap-agent-k8s-tolerations.yaml](whatapagent/whatap-agent-k8s-tolerations.yaml) - Shows how to add custom tolerations to the Whatap agents.

#### Understanding Kubernetes Tolerations

In Kubernetes, **taints** and **tolerations** work together to ensure that pods are not scheduled onto inappropriate nodes:

- **Taints** are applied to nodes and allow a node to repel certain pods.
- **Tolerations** are applied to pods and allow (but do not require) pods to be scheduled onto nodes with matching taints.

Tolerations are crucial for monitoring agents because:
- They ensure monitoring coverage across all nodes, including those with special taints
- They allow monitoring of specialized workloads (e.g., GPU nodes, dedicated nodes)
- They help maintain monitoring even during node maintenance or issues

Each toleration consists of:
- **key**: The taint key to match
- **operator**: Either `Equal` (must match the key and value) or `Exists` (only needs to match the key)
- **value**: The taint value to match (only used with `Equal` operator)
- **effect**: What happens to pods that don't tolerate the taint (`NoSchedule`, `PreferNoSchedule`, or `NoExecute`)

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"
  host: "whatap-server"
  port: "6600"
  features:
    k8sAgent:
      masterAgent:
        enabled: true
        # Tolerations for the master agent
        tolerations:
          - key: "dedicated"            # Match nodes with the "dedicated" taint
            operator: "Equal"           # Must match both key and value
            value: "monitoring"         # The taint value to match
            effect: "NoSchedule"        # Tolerate NoSchedule effect
          - key: "special-workload"     # Match nodes with the "special-workload" taint
            operator: "Exists"          # Match any value for this key
            effect: "NoSchedule"        # Tolerate NoSchedule effect
      nodeAgent:
        enabled: true
        # Tolerations for the node agent
        # Note: These are in addition to the default tolerations for master and control-plane nodes
        tolerations:
          - key: "dedicated"            # Match nodes with the "dedicated" taint
            operator: "Equal"           # Must match both key and value
            value: "monitoring"         # The taint value to match
            effect: "NoSchedule"        # Tolerate NoSchedule effect
          - key: "gpu"                  # Match nodes with the "gpu" taint
            operator: "Exists"          # Match any value for this key
            effect: "NoSchedule"        # Tolerate NoSchedule effect
```

#### How Tolerations Work in Whatap Operator

The WhatapAgent CR directly supports specifying tolerations for both the master agent and node agent. The tolerations specified in the CR will be applied to the respective pods.

For the node agent, the specified tolerations are added to the default tolerations for master and control-plane nodes:
- `key: "node-role.kubernetes.io/master", effect: "NoSchedule"`
- `key: "node-role.kubernetes.io/control-plane", effect: "NoSchedule"`

This ensures that the node agent will run on all nodes, including master/control-plane nodes and nodes with custom taints.

#### Common Use Cases for Custom Tolerations

1. **Monitoring Dedicated Nodes**: If you have nodes dedicated to specific workloads (e.g., with `dedicated=workload-type` taints), add matching tolerations to ensure monitoring coverage.

2. **GPU Nodes**: Nodes with GPUs often have special taints to ensure only GPU workloads run on them. Add tolerations to monitor these specialized nodes.

3. **Production vs. Development**: If you use taints to separate production and development workloads, ensure your monitoring agents can run in both environments.

4. **Node Maintenance**: When nodes are cordoned or marked for maintenance, monitoring agents with appropriate tolerations can continue to run and provide visibility during the maintenance process.

### Secret-based Configuration

[whatap-agent-secret.yaml](whatapagent/whatap-agent-secret.yaml) - Uses a Kubernetes secret to store Whatap credentials instead of specifying them directly in the CR.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  # No license, host, or port specified here
  # These values will be retrieved from the "whatap-credentials" secret
  features:
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

## Usage

### Using Direct Configuration

To apply an example configuration with credentials directly in the CR, use the following command:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-basic.yaml
```

Replace the URL with the specific example you want to use.

Make sure to replace the placeholder values (`your-license-key`, `whatap-server`, etc.) with your actual Whatap credentials before applying the configuration.

### Using Secret-based Configuration

To use the secret-based approach, first create a secret with your Whatap credentials:

```bash
kubectl create secret generic whatap-credentials --namespace whatap-monitoring \
  --from-literal=license=$WHATAP_LICENSE \
  --from-literal=host=$WHATAP_HOST \
  --from-literal=port=$WHATAP_PORT
```

Then apply the configuration that uses the secret:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-secret.yaml
```
