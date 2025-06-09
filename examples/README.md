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
            cpu: "200m"
            memory: "400Mi"
          limits:
            cpu: "500m"
            memory: "600Mi"
      nodeAgent:
        enabled: true
        resources:
          requests:
            cpu: "150m"
            memory: "350Mi"
          limits:
            cpu: "300m"
            memory: "500Mi"
```

### K8s Agent with Custom Tolerations

[whatap-agent-k8s-tolerations.yaml](whatapagent/whatap-agent-k8s-tolerations.yaml) - Shows how to add custom tolerations to the Whatap agents.

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
          - key: "dedicated"
            operator: "Equal"
            value: "monitoring"
            effect: "NoSchedule"
          - key: "special-workload"
            operator: "Exists"
            effect: "NoSchedule"
      nodeAgent:
        enabled: true
        # Tolerations for the node agent
        # Note: These are in addition to the default tolerations for master and control-plane nodes
        tolerations:
          - key: "dedicated"
            operator: "Equal"
            value: "monitoring"
            effect: "NoSchedule"
          - key: "gpu"
            operator: "Exists"
            effect: "NoSchedule"
```

The WhatapAgent CR now directly supports specifying tolerations for both the master agent and node agent. The tolerations specified in the CR will be applied to the respective pods.

For the node agent, the specified tolerations are added to the default tolerations for master and control-plane nodes:
- `key: "node-role.kubernetes.io/master", effect: "NoSchedule"`
- `key: "node-role.kubernetes.io/control-plane", effect: "NoSchedule"`

This ensures that the node agent will run on all nodes, including master/control-plane nodes and nodes with custom taints.

## Usage

To apply an example configuration, use the following command:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-basic.yaml
```

Replace the URL with the specific example you want to use.

Make sure to replace the placeholder values (`your-license-key`, `whatap-server`, etc.) with your actual Whatap credentials before applying the configuration.
