#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"
IMAGE_NAME=${IMAGE_NAME:-quay.io/open-cluster-management/multicluster-controlplane:latest}

source "${REPO_DIR}/test/bin/util.sh"

output="${REPO_DIR}/_output"
cluster_dir="${output}/kubeconfig"
deploy_dir="${output}/controlplane/deploy"
agent_deploy_dir="${output}/agent/deploy"

mkdir -p ${cluster_dir}
mkdir -p ${deploy_dir}
mkdir -p ${agent_deploy_dir}

echo "Create a cluster with kind ..."
cluster="e2e-test"
external_host_port="30443"
kubeconfig="${cluster_dir}/${cluster}.kubeconfig"
cat << EOF | kind create cluster --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig --name ${cluster} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: ${external_host_port}
    hostPort: 443
EOF

echo "Load $IMAGE_NAME to the cluster $cluster ..."
kind load docker-image $IMAGE_NAME --name $cluster

echo "Deploy etcd in the cluster $cluster ..."
cp $REPO_DIR/hack/deploy/etcd/statefulset.yaml $REPO_DIR/hack/deploy/etcd/statefulset.yaml.tmp
sed -i "s/gp2/standard/g" $REPO_DIR/hack/deploy/etcd/statefulset.yaml
pushd ${REPO_DIR}
export KUBECONFIG=${kubeconfig}
make deploy-etcd
unset KUBECONFIG
popd
mv $REPO_DIR/hack/deploy/etcd/statefulset.yaml.tmp $REPO_DIR/hack/deploy/etcd/statefulset.yaml

# TODO use helm instead of kustomize way
namespace=multicluster-controlplane
echo "Deploy standalone controlplane in namespace $namespace ..."
cp -r ${REPO_DIR}/hack/deploy/controlplane/* $deploy_dir

kubectl --kubeconfig ${kubeconfig} delete ns $namespace --ignore-not-found
kubectl --kubeconfig ${kubeconfig} create ns $namespace

# expose apiserver
sed -i 's/ClusterIP/NodePort/' $deploy_dir/service.yaml
sed -i '/route\.yaml/d' $deploy_dir/kustomization.yaml
sed -i "/targetPort.*/a  \ \ \ \ \ \ nodePort: $external_host_port" $deploy_dir/service.yaml

# append etcd certs
certs_dir=$deploy_dir/certs
mkdir -p ${certs_dir}
cp -f ${REPO_DIR}/multicluster_ca/ca.pem ${certs_dir}/etcd-ca.crt
cp -f ${REPO_DIR}/multicluster_ca/client.pem ${certs_dir}/etcd-client.crt
cp -f ${REPO_DIR}/multicluster_ca/client-key.pem ${certs_dir}/etcd-client.key
sed -i "$(sed -n  '/  - ocmconfig.yaml/=' $deploy_dir/kustomization.yaml) a \  - ${certs_dir}/etcd-client.key" $deploy_dir/kustomization.yaml
sed -i "$(sed -n  '/  - ocmconfig.yaml/=' $deploy_dir/kustomization.yaml) a \  - ${certs_dir}/etcd-client.crt" $deploy_dir/kustomization.yaml
sed -i "$(sed -n  '/  - ocmconfig.yaml/=' $deploy_dir/kustomization.yaml) a \  - ${certs_dir}/etcd-ca.crt" $deploy_dir/kustomization.yaml

# create multicluster-controlplane configfile
cat > ${deploy_dir}/ocmconfig.yaml <<EOF
dataDirectory: /.ocm
apiserver:
  externalHostname: 127.0.0.1
etcd:
  mode: external
  prefix: $namespace
  caFile: /controlplane_config/etcd-ca.crt
  certFile: /controlplane_config/etcd-client.crt
  keyFile: /controlplane_config/etcd-client.key
  servers:
  - http://etcd-0.etcd.multicluster-controlplane-etcd:2379
  - http://etcd-1.etcd.multicluster-controlplane-etcd:2379
  - http://etcd-2.etcd.multicluster-controlplane-etcd:2379
EOF
sed -i "s@ocmconfig.yaml@${deploy_dir}/ocmconfig.yaml@g" $deploy_dir/kustomization.yaml

pushd $deploy_dir
kustomize edit set namespace $namespace
kustomize edit set image quay.io/open-cluster-management/multicluster-controlplane=${IMAGE_NAME}
popd

kustomize build $deploy_dir | kubectl --kubeconfig ${kubeconfig} -n $namespace apply -f -

wait_command "kubectl --kubeconfig $kubeconfig -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig"
hubkubeconfig="${cluster_dir}/controlplane.kubeconfig"
kubectl --kubeconfig $kubeconfig -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig -ojsonpath='{.data.kubeconfig}' | base64 -d > ${hubkubeconfig}

echo "Deploy standalone controlplane agents ..."
cp -r ${REPO_DIR}/hack/deploy/agent/* $agent_deploy_dir

agent_namespace="multicluster-controlplane-agent"
kubectl --kubeconfig ${kubeconfig} delete ns ${agent_namespace} --ignore-not-found
kubectl --kubeconfig ${kubeconfig} create ns ${agent_namespace}

cp -f ${hubkubeconfig} ${agent_deploy_dir}/hub-kubeconfig
kubectl --kubeconfig ${agent_deploy_dir}/hub-kubeconfig config set-cluster multicluster-controlplane --server=https://multicluster-controlplane.multicluster-controlplane.svc:443

pushd $agent_deploy_dir
kustomize edit set image quay.io/open-cluster-management/multicluster-controlplane=${IMAGE_NAME}
popd
kustomize build ${agent_deploy_dir} | kubectl --kubeconfig $kubeconfig -n ${agent_namespace} apply -f -

export HUBKUBECONFIG=${hubkubeconfig}
export SPOKEKUBECONFIG=${kubeconfig}
${output}/e2e.test -test.v -ginkgo.v
