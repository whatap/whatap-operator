# User Guide: Configuring Whatap Monitoring

This guide explains how to configure Whatap monitoring for your applications and infrastructure using the Whatap Operator.

## Overview

If you want to configure Whatap monitoring for your applications and infrastructure, you don't need to worry about how the operator is deployed. You just need to create a `WhatapAgent` custom resource with the appropriate configuration.

## Quick Start

1. Make sure the Whatap Operator is installed in your cluster (ask your cluster administrator if you're not sure)

2. Create a basic configuration file (e.g., `whatap-config.yaml`):

```yaml
apiVersion: monitoring.whatap.com/v2alpha1
kind: WhatapAgent
metadata:
  name: whatap
spec:
  license: "your-license-key"  # Replace with your actual license key
  host: "whatap-server"        # Replace with your Whatap server address
  port: "6600"                 # Replace with your Whatap server port
  features:
    k8sAgent:
      masterAgent:
        enabled: true
      nodeAgent:
        enabled: true
```

3. Apply the configuration:

```sh
kubectl apply -f whatap-config.yaml
```

## Configuration Options

The Whatap Operator supports various monitoring configurations:

### Basic Kubernetes Monitoring

Enables the master agent and node agent for basic Kubernetes monitoring:

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

### APM Instrumentation

Automatically injects APM agents into your application pods:

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

### OpenAgent

Collects Prometheus-style metrics from various sources:

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

### Combined Monitoring

You can combine multiple monitoring types for comprehensive observability. See the [Configuration Examples](../examples/README.md) for more detailed examples.

## Advanced Configuration

For advanced configuration options, including:

- Custom resource requirements
- Custom tolerations
- Combined monitoring configurations

See the [Configuration Examples Documentation](../examples/README.md).

## Customizing Resources

The Whatap Operator preserves custom labels, annotations, tolerations, and environment variables when reconciling resources. This allows you to customize the resources created by the operator to fit your specific needs.

For detailed information on how to customize resources, including examples of adding custom labels, annotations, and tolerations, see the [Configuration Examples Documentation](../examples/README.md).
