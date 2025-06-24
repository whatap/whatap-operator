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

### Operator Example Configuration

[whatap-agent-operator-example.yaml](whatapagent/whatap-agent-operator-example.yaml) - A practical example configuration for using with the Whatap Operator, including APM, K8sAgent, and OpenAgent with Korean comments.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    ### APM 설정 - 애플리케이션 성능 모니터링을 위한 에이전트 자동 주입
    apm:
      instrumentation:
        targets:
          - name: "sample-app"
            enabled: true
            language: "java"
            whatapApmVersions:
              java: "latest"
            # ... (see the file for complete example)
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

### K8s Agent with Custom Environment Variables

[whatap-agent-k8s-envs.yaml](whatapagent/whatap-agent-k8s-envs.yaml) - Shows how to add custom environment variables to the Whatap agents.

### K8s Agent with Container-Specific Configuration

[whatap-agent-k8s-container-config.yaml](whatapagent/whatap-agent-k8s-container-config.yaml) - Shows how to configure the whatap-node-agent and whatap-node-helper containers separately.

#### Understanding Container-Specific Configuration

The NodeAgent daemonset consists of two containers:
- **whatap-node-agent**: The main container that collects node metrics
- **whatap-node-helper**: A helper container that assists with container metrics collection

You can configure these containers separately by using the `nodeAgentContainer` and `nodeHelperContainer` fields in the NodeAgent spec. This allows you to:

- Set different resource requirements for each container
- Configure different environment variables for each container
- Optimize each container according to its specific role

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    k8sAgent:
      nodeAgent:
        enabled: true
        # Configuration specific to the whatap-node-agent container
        nodeAgentContainer:
          resources:
            requests:
              cpu: "150m"
              memory: "350Mi"
            limits:
              cpu: "300m"
              memory: "500Mi"
          envs:
            - name: NODE_AGENT_CUSTOM_ENV
              value: "custom-value"

        # Configuration specific to the whatap-node-helper container
        nodeHelperContainer:
          resources:
            requests:
              cpu: "100m"
              memory: "150Mi"
            limits:
              cpu: "200m"
              memory: "300Mi"
          envs:
            - name: NODE_HELPER_CUSTOM_ENV
              value: "helper-value"
```

This feature is particularly useful when:
1. You need to fine-tune resource allocation between the two containers
2. You need to set specific environment variables for one container but not the other
3. You want to optimize performance by allocating resources according to each container's workload

#### Understanding Environment Variables in Kubernetes

Environment variables in Kubernetes pods provide a way to pass configuration to applications running in containers. For monitoring agents, environment variables can be used to:

- Configure agent behavior and features
- Set monitoring parameters and thresholds
- Connect to external services or data sources
- Enable or disable specific monitoring capabilities

The Whatap Operator allows you to specify custom environment variables for both the master agent and node agent through the WhatapAgent CR.

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  features:
    k8sAgent:
      masterAgent:
        enabled: true
        # Custom environment variables for MasterAgent
        envs:
          - name: CUSTOM_ENV_VAR1
            value: "value1"
          - name: CUSTOM_ENV_VAR2
            value: "value2"
      nodeAgent:
        enabled: true
        # Custom environment variables for NodeAgent
        envs:
          - name: NODE_CUSTOM_ENV_VAR1
            value: "node_value1"
          - name: NODE_CUSTOM_ENV_VAR2
            value: "node_value2"
          # Environment variable from ConfigMap
          - name: CONFIG_ENV_VAR
            valueFrom:
              configMapKeyRef:
                name: my-config-map
                key: config-key
          # Environment variable from Secret
          - name: SECRET_ENV_VAR
            valueFrom:
              secretKeyRef:
                name: my-secret
                key: secret-key
```

#### Types of Environment Variable Sources

You can specify environment variables in several ways:

1. **Direct Value**: Set the value directly in the CR
   ```yaml
   - name: ENV_NAME
     value: "env_value"
   ```

2. **From ConfigMap**: Reference a value from a ConfigMap
   ```yaml
   - name: ENV_NAME
     valueFrom:
       configMapKeyRef:
         name: my-config-map
         key: config-key
   ```

3. **From Secret**: Reference a value from a Secret
   ```yaml
   - name: ENV_NAME
     valueFrom:
       secretKeyRef:
         name: my-secret
         key: secret-key
   ```

4. **From Field**: Reference a field from the pod or container
   ```yaml
   - name: NODE_NAME
     valueFrom:
       fieldRef:
         fieldPath: spec.nodeName
   ```

#### Common Use Cases for Custom Environment Variables

1. **Agent Configuration**: Set agent-specific configuration parameters
2. **Proxy Settings**: Configure proxies for outbound connections
3. **Debug Levels**: Set logging or debug levels for troubleshooting
4. **Feature Flags**: Enable or disable specific monitoring features
5. **Integration Settings**: Configure integration with other systems

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
  --from-literal=WHATAP_LICENSE=$WHATAP_LICENSE \
  --from-literal=WHATAP_HOST=$WHATAP_HOST \
  --from-literal=WHATAP_PORT=$WHATAP_PORT
```

Then apply the configuration that uses the secret:

```bash
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/main/examples/whatapagent/whatap-agent-secret.yaml
```
