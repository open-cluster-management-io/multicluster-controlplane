[comment]: # ( Copyright Contributors to the Open Cluster Management project )
# Get started 

## Install multicluster-controlplane

### Option 1: Deploy multicluster-controlplane with embed etdcd on Openshift Cluster

#### Build image

```bash
$ export IMAGE_NAME=<customized image. default is quay.io/open-cluster-management/multicluster-controlplane:latest>
$ make image
```

#### Install 

Set environment variables firstly and then deploy controlplane.
* `HUB_NAME` (optional) is the namespace where the controlplane is deployed in. The default is `multicluster-controlplane`.
* `IMAGE_NAME` (optional) is the customized image which can override the default image `quay.io/open-cluster-management/multicluster-controlplane:latest`.

For example: 

    ```bash
    $ export HUB_NAME=<hub name>
    $ export IMAGE_NAME=<your image>
    $ make deploy
    ```

### Option 2: Run controlplane as a local binary

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

### Option 3: Deploy multicluster-controlplane with external etcd on Openshift Cluster 

#### Install etcd
Set environmrnt variables and deploy etcd.
* `ETCD_NS` (optional) is the namespace where the etcd is deployed in. The default is `multicluster-controlplane-etcd`.

For example:
```bash
$ export ETCD_NS=<etcd namespace>
$ make deploy-etcd
```

#### Build image

```bash
$ export IMAGE_NAME=<customized image. default is quay.io/open-cluster-management/multicluster-controlplane:latest>
$ make image
```

#### Install 
Set environment variables and deploy controlplane.
* `HUB_NAME` (optional) is the namespace where the controlplane is deployed in. The default is `multicluster-controlplane`.
* `IMAGE_NAME` (optional) is the customized image which can override the default image `quay.io/open-cluster-management/multicluster-controlplane:latest`.

For example: 

    ```bash
    $ export HUB_NAME=<hub name>
    $ export IMAGE_NAME=<your image>
    $ make deploy-with-external-etcd
    ```

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
