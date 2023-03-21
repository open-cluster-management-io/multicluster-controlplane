[comment]: # ( Copyright Contributors to the Open Cluster Management project )
# Get started 

## Config file
By default, the multicluster controlplane would try to find ocmconfig.yaml in the current directory. You can use the customized one by setting the flag --config-file=<config-file.yaml>.

Here is a simple file of config-file.yaml:
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

The yaml content shown above is a config file with all fields filled in. Following this to better understand the config file.

Field `dataDirectory` is a string variable indicating the directory to store generated certs ,embed etcd data and kubeconfig, etc. While this field is missed in the config file, the default value `/.ocm` makes sense.

Field `apiserver` contains config for the controlplane apiserver:
- `externalHostname` is a string variable indicating the hostname for external access.
- `port` is a integer variable indicating the binding port of multicluster controlplane apiserver. The default value is `9443`.
- `caFile` is a string variable indicating the CA file provided by user to sign all the serving/client certificates. 
- `caKeyFile` is a string variable indicating the CA Key file for `caFile`.

Field `etcd` contains config for the controlplane etcd:
- `mode` should be `embed` or `external` indicating the multicluster controlplane etcd deploy mode. The value would be `embed` if field `mode` is missed.
- `prefix` is a string variable indicating controlplane data prefix in etcd. The default value is `"/registry"`.
- `servers` is a string array indicating etcd endpoints. The default value is `[]string{"http://127.0.0.1:2379"}`.
- `caFile` is a string variable indicating an etcd trusted ca file.
- `certFile` is a string variable indicating a client cert file signed by `caFile`.
- `keyFile` is a string variable indicating client key file for `certFile`.

> **NOTE:**
> For `apiserver` field: If you want to use your own CA pair to sign the certificates, the `caFile` and `caKeyFile` should be set together. Which means that if one of the two fields is missed/empty, the controlplane would self-generate CA pair to sign the necessary certificates. 


## Optional: Deploy etcd on Cluster 

#### Install etcd
Set environment variables and deploy etcd.
* `ETCD_NS` (optional) is the namespace where the etcd is deployed in. The default is `multicluster-controlplane-etcd`.

For example:
```bash
$ export ETCD_NS=<etcd namespace>
$ make deploy-etcd
```

## Install multicluster-controlplane
Before start the controlplane, we should make sure config file is in path.

### Option 1: Deploy multicluster-controlplane on Openshift/EKS Cluster

#### Build image

```bash
$ export IMAGE_NAME=<customized image. default is quay.io/open-cluster-management/multicluster-controlplane:latest>
$ make image
```

#### Install 

Set environment variables firstly and then deploy controlplane.
* `HUB_NAME` (optional) is the namespace where the controlplane is deployed in. The default is `multicluster-controlplane`.
* `IMAGE_NAME` (optional) is the customized image which can override the default image `quay.io/open-cluster-management/multicluster-controlplane:latest`.

Edit the config file:
1. externalHostname can be missed/set to empty.
2. port should be set to 443 

For example: 

```bash
$ export HUB_NAME=<hub name>
$ export IMAGE_NAME=<your image>
$ make deploy
```

### Option 2: Deploy multicluster-controlplane on Kind Cluster

Edit the config file:
1. externalHostname can be missed/set to empty.
2. The range of valid port is 30000-32767

Most steps are similar with Option 1, the only difference is deploy command:
```bash
$ make deploy
```

### Option 3: Run controlplane as a local binary

```bash
$ make vendor
$ make build
$ make run 
```

## Access the controlplane

The kubeconfig file of the controlplane is in the dir `hack/deploy/cert-${HUB_NAME}/kubeconfig`.

You can use clusteradm to access and join a cluster.
```bash
$ clusteradm --kubeconfig=<kubeconfig file> get token --use-bootstrap-token
$ clusteradm join --hub-token <hub token> --hub-apiserver <hub apiserver> --cluster-name <cluster_name>
$ clusteradm --kubeconfig=<kubeconfig file> accept --clusters <cluster_name>
```

> **Warning**
> clusteradm version should be v0.4.1 or later


## Install add-on

Currently we support to install work-manager and managed-serviceaccount add-on on the controlplane.

```bash
$ make deploy-work-manager-addon
$ make deploy-managed-serviceaccount-addon
```

## Clean up the deploy

```bash
$ make destory
```

## Install the multicluster-controlplane and add-ons

```bash
$ export HUB_NAME=<hub name>
$ export IMAGE_NAME=<your image>
$ make deploy-all
```
