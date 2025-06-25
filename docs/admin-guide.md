# Administrator Guide: Deploying the Whatap Operator

This guide is for cluster administrators who need to deploy the Whatap Operator.

## Prerequisites

Before deploying the Whatap Operator, ensure you have:

- go version v1.22.0+
- docker version 17.03+
- kubectl version v1.11.3+
- Access to a Kubernetes v1.11.3+ cluster

## Deployment Options

### Option 1: Using the Pre-built Installer

The simplest way to deploy the Whatap Operator is to use the pre-built installer:

```sh
kubectl apply -f https://raw.githubusercontent.com/whatap/whatap-operator/<version>/dist/install.yaml
```

Replace `<version>` with the desired version of the operator.

### Option 2: Building and Deploying from Source

1. Clone the repository:

```sh
git clone https://github.com/whatap/whatap-operator.git
cd whatap-operator
```

2. Build and push the operator image:

```sh
make docker-build docker-push IMG=<some-registry>/whatap-operator:tag
```

3. Install the CRDs:

```sh
make install
```

4. Deploy the operator:

```sh
make deploy IMG=<some-registry>/whatap-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as admin.

## Uninstalling the Operator

1. Delete any WhatapAgent custom resources:

```sh
kubectl delete whatapagent --all
```

2. Uninstall the CRDs:

```sh
make uninstall
```

3. Undeploy the controller:

```sh
make undeploy
```

## Building the Installer

To build an installer for distribution:

```sh
make build-installer IMG=<some-registry>/whatap-operator:tag
```

This generates an `install.yaml` file in the `dist` directory that can be used to install the operator.

## Verifying the Installation

To verify that the operator is running correctly:

```sh
kubectl get pods -n whatap-monitoring
```

You should see the Whatap Operator pod running.

## Troubleshooting

### Common Issues

1. **RBAC Errors**: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges:

```sh
kubectl create clusterrolebinding cluster-admin-binding \
  --clusterrole=cluster-admin \
  --user=$(gcloud config get-value core/account)  # For GKE
```

2. **Image Pull Errors**: If the operator pod fails to start due to image pull errors, ensure that:
   - The image exists in the specified registry
   - The Kubernetes cluster has access to the registry
   - You have provided the correct image name and tag

3. **CRD Installation Failures**: If CRD installation fails, check for conflicts with existing CRDs:

```sh
kubectl get crd | grep whatap
```

## Next Steps

After deploying the Whatap Operator, you can:

1. Create a WhatapAgent custom resource to start monitoring your cluster
2. Configure APM instrumentation for your applications
3. Set up OpenAgent to collect Prometheus-style metrics

See the [User Guide](user-guide.md) for details on configuring Whatap monitoring.