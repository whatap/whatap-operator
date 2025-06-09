# Whatap Operator

The Whatap Operator is a Kubernetes operator that simplifies the deployment and management of Whatap monitoring agents in your Kubernetes cluster.

## Description

The Whatap Operator automates the installation, configuration, and lifecycle management of Whatap monitoring components:

- **Master Agent**: Collects and processes monitoring data from node agents
- **Node Agent**: Monitors Kubernetes nodes and containers
- **Open Agent**: Collects Prometheus-style metrics from various sources
- **APM Instrumentation**: Automatically injects APM agents into application pods

The operator uses a Custom Resource Definition (CRD) to define the desired state of the monitoring agents, making it easy to deploy and configure Whatap monitoring in a Kubernetes-native way.

## Documentation

### Quick Start

For a quick introduction to deploying and using the Whatap Operator, see the [Quick Start Guide](docs/quick-start.md).

### For Users

If you want to configure Whatap monitoring for your applications and infrastructure, see the [User Guide](docs/user-guide.md).

### For Administrators

If you need to deploy and manage the Whatap Operator in your Kubernetes cluster, see the [Administrator Guide](docs/admin-guide.md).

### Configuration Examples

For examples of different monitoring configurations, see the [Configuration Examples](examples/README.md).

### Customization

For information on how to customize resources created by the operator, see the [Customization Documentation](docs/customization.md).

## Contributing

Contributions to the Whatap Operator are welcome! Here are some ways you can contribute:

- Report bugs or suggest features by creating issues
- Improve documentation
- Submit pull requests with bug fixes or new features
- Share your experiences using the operator

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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
