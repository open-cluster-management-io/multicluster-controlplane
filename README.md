[comment]: # ( Copyright Contributors to the Open Cluster Management project )
# multicluster-controlplane

The `multicluster-controlplane` is a lightweight Open Cluster Management (OCM) control plane that is easy to install and has a small footprint. It is more efficient, lightweight, and cost-effective, while improving OCM scalability and support for edge scenarios.

![Architecture Diagram](./arch.png)

## Features

- Starts the OCM hub control plane in standalone mode
- Combines the registration and work agent into a single entity
- Reduces the footprint of both the control plane and agent

## Benefits

The benefits of these improvements include:

- **Quick Startup**: The lightweight control plane instance can be started within a very short time, making it easier to consume. This reduces resource consumption and lowers costs.
- **Multi-Tenancy**: Multiple OCM instances can run in different namespaces within a single Kubernetes cluster. Each instance operates in a pod within its respective namespace. By exposing an endpoint, each OCM instance allows clusters to register as managed clusters.
- **Enhanced Edge Scenarios**: A single cluster with multiple OCM instances can support more managed clusters compared to a single OCM cluster. This capability is particularly useful in edge scenarios where managing multiple clusters efficiently is crucial.
- **Platform Compatibility**: The multicluster-controlplane offers broader platform compatibility, including support for k8s platforms (e.g., EKS). Moreover, it can even be executed as a standalone binary without the need for deployment on a Kubernetes cluster.

## Getting Started

### Prerequisites

- Go 1.22.8 or later
- Kubernetes cluster (for deployment mode)
- kubectl configured (for deployment mode)
- Helm 3.x (for Helm deployment)

## Build

### Build Binary

```bash
make vendor
make build
```

### Build Image

```bash
export IMAGE_NAME=<customized image. default is quay.io/open-cluster-management/multicluster-controlplane:latest>
make image
```

## Run Controlplane as a Local Binary

```bash
export CONFIG_DIR=<the directory of the controlplane configuration file. default is ./_output/controlplane>
make run
```

You can customize the controlplane configurations by creating a config file and using the environment variable `CONFIG_DIR` to specify your config file directory.

**NOTE**: The controlplane config file name must be `ocmconfig.yaml`

### Configuration File

Here is a sample file of `ocmconfig.yaml`:

```yaml
dataDirectory: "/.ocm"
apiserver:
  externalHostname: "http://abcdefg.com"
  port: 9443
  caFile: "ca.crt"
  caKeyFile: "ca.key"
etcd:
  mode: external
  prefix: "/registry"
  servers:
  - http://etcd-1:2379
  - http://etcd-2:2379
  caFile: "etcd-trusted-ca.crt"
  certFile: "etcd-client.crt"
  keyFile: "etcd-client.key"
```

### Configuration Fields

The yaml content shown above is a config file with all fields filled in. The following describes each field:

#### Data Directory

Field `dataDirectory` is a string variable indicating the directory to store generated certificates, embedded etcd data, and kubeconfig files. If this field is omitted in the config file, the default value `/.ocm` is used.

#### API Server Configuration

Field `apiserver` contains configuration for the controlplane apiserver:
- `externalHostname` - String variable indicating the hostname for external access
- `port` - Integer variable indicating the binding port of multicluster controlplane apiserver. The default value is `9443`
- `caFile` - String variable indicating the CA file provided by user to sign all the serving/client certificates
- `caKeyFile` - String variable indicating the CA Key file for `caFile`

#### Etcd Configuration

Field `etcd` contains configuration for the controlplane etcd:
- `mode` - Should be `embed` or `external` indicating the multicluster controlplane etcd deploy mode. The value defaults to `embed` if this field is omitted
- `prefix` - String variable indicating controlplane data prefix in etcd. The default value is `"/registry"`
- `servers` - String array indicating etcd endpoints. The default value is `[]string{"http://127.0.0.1:2379"}`
- `caFile` - String variable indicating an etcd trusted ca file
- `certFile` - String variable indicating a client cert file signed by `caFile`
- `keyFile` - String variable indicating client key file for `certFile`

**NOTE**: For the `apiserver` field: If you want to use your own CA pair to sign the certificates, the `caFile` and `caKeyFile` should be set together. If one of the two fields is missing or empty, the controlplane will self-generate a CA pair to sign the necessary certificates.

## Deploy Controlplane Using Helm

### Prerequisites

1. Set the environment variable KUBECONFIG to your cluster kubeconfig path:

   ```bash
   export KUBECONFIG=<the kubeconfig path of your cluster>
   ```

2. (Optional) By default, the controlplane will have an embedded etcd. You can use the following command to deploy an external etcd:

   ```bash
   make deploy-etcd
   ```

   This external etcd will be deployed in the namespace `multicluster-controlplane-etcd`, its certificates will be created at `./_output/etcd/deploy/cert-etcd` and its service URLs will be: `http://etcd-0.etcd.multicluster-controlplane-etcd:2379`, `http://etcd-1.etcd.multicluster-controlplane-etcd:2379`, and `http://etcd-2.etcd.multicluster-controlplane-etcd:2379`

### Install

Run the following command to deploy a controlplane:

```bash
helm repo add ocm https://open-cluster-management.io/helm-charts/
helm repo update
helm search repo ocm
helm install -n multicluster-controlplane multicluster-controlplane ocm/multicluster-controlplane --create-namespace --set <values to set>
```

#### Configuration Options

- To provide your own CA pairs for controlplane:

  ```bash
  --set-file apiserver.ca="<path-to-ca>",apiserver.cakey="<path-to-ca-key>"
  ```

- To use external etcd:

  ```bash
  --set-file etcd.ca="<path-to-etcd-ca>",etcd.cert="<path-to-etcd-client-cert>",etcd.certkey="<path-to-etcd-client-cert-key>"
  --set etcd.mode="external",etcd.servers={server1,server2,...}
  ```

- To use the OpenShift route:

  ```bash
  --set route.enabled=true
  ```

- To use the load balancer service:

  ```bash
  --set loadbalancer.enabled=true
  ```

- To use the node port service:

  ```bash
  --set nodeport.enabled=true
  --set nodeport.port=<your-node-port>
  ```

- To enable self-management:

  ```bash
  --set enableSelfManagement=true
  ```

- To delegate authentication with kube-apiserver:

  ```bash
  --set enableDelegatingAuthentication=true
  ```

More available configuration values can be found [here](https://github.com/open-cluster-management-io/multicluster-controlplane/blob/main/charts/multicluster-controlplane/values.yaml).

### Uninstall

```bash
helm uninstall -n multicluster-controlplane multicluster-controlplane
```

## Access the Controlplane

### Binary Mode

If you run the controlplane as a binary, the controlplane kubeconfig file is located at `_output/controlplane/.ocm/cert/kube-aggregator.kubeconfig`

### Cluster Deployment Mode

If you deploy the controlplane in a cluster, run the following command to get the controlplane kubeconfig:

```bash
kubectl -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig -ojsonpath='{.data.kubeconfig}' | base64 -d > multicluster-controlplane.kubeconfig
```

#### Authentication Delegation

If you enable authentication delegation, you can set a context for your controlplane in your cluster kubeconfig with the following commands:

```bash
external_host_name=<your controlplane external host name>
# If you want to add the CA of your cluster kube-apiserver, use the command:
# kubectl config set-cluster multicluster-controlplane --server="https://${external_host_name}" --embed-certs --certificate-authority=<the ca path of your cluster kube-apiserver>
kubectl config set-cluster multicluster-controlplane --server="https://${external_host_name}" --insecure-skip-tls-verify
kubectl config set-context multicluster-controlplane --cluster=multicluster-controlplane --user=kube:admin --namespace=default
```

## Join a Cluster

You can use clusteradm to access and join a cluster.

### Prerequisites

**Note**: clusteradm version should be v0.4.1 or later

### Steps

1. Get the join token from controlplane:

   ```bash
   clusteradm --kubeconfig=<controlplane kubeconfig file> get token --use-bootstrap-token
   ```

2. Join a cluster using the controlplane agent (available in clusteradm - see this [PR](https://github.com/open-cluster-management-io/clusteradm/pull/338) for more details, you should build the latest code).

   Add the `--singleton` flag in the join command to use the controlplane agent, rather than klusterlet, to join a cluster:

   ```bash
   clusteradm join --hub-token <controlplane token> --hub-apiserver <controlplane apiserver> --cluster-name <cluster name> --singleton
   ```

3. Access the controlplane apiserver to accept the managed cluster:

   ```bash
   clusteradm --kubeconfig=<controlplane kubeconfig file> accept --clusters <cluster name>
   ```