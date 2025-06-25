# Quick Start Guide

This guide provides a quick introduction to deploying and using the Whatap Operator.

## For Administrators: Deploying the Operator

### Using the Pre-built Installer

The simplest way to deploy the Whatap Operator is to use the pre-built installer:

```sh
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/<version>/dist/install.yaml
```

Replace `<version>` with the desired version of the operator.

### Verifying the Installation

To verify that the operator is running correctly:

```sh
kubectl get pods -n whatap-monitoring
```

You should see the Whatap Operator pod running.

For more detailed deployment instructions, see the [Administrator Guide](admin-guide.md).

## For Users: Configuring Whatap Monitoring

### Basic Kubernetes Monitoring

1. Create a basic configuration file (e.g., `whatap-config.yaml`):

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

2. Apply the configuration:

```sh
kubectl apply -f whatap-config.yaml
```

### APM Instrumentation

To enable APM instrumentation for your applications:

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

### OpenAgent for Prometheus-style Metrics

To enable OpenAgent for collecting Prometheus-style metrics:

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
```

For more detailed configuration instructions, see the [User Guide](user-guide.md).

## Next Steps

- [Administrator Guide](admin-guide.md) - Detailed instructions for deploying and managing the Whatap Operator
- [User Guide](user-guide.md) - Comprehensive guide to configuring Whatap monitoring
- [Configuration Examples](../examples/README.md) - Examples of different monitoring configurations and how to customize resources
