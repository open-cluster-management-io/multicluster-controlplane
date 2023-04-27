#!/usr/bin/env bash

KIND=${KIND:-"kind"}
KUBECTL=${KUBECTL:-"kubectl"}
HELM=${HELM:-"helm"}
KUSTOMIZE=${KUSTOMIZE:-"kustomize"}

if ! command -v ${KIND} >/dev/null 2>&1; then
    echo "ERROR: command ${KIND} is not found"
    exit 1
fi

if ! command -v ${KUBECTL} >/dev/null 2>&1; then
    echo "ERROR: command ${KUBECTL} is not found"
    exit 1
fi

if ! command -v ${HELM} >/dev/null 2>&1; then
    echo "ERROR: command ${HELM} is not found"
    exit 1
fi

if ! command -v ${KUSTOMIZE} >/dev/null 2>&1; then
    echo "ERROR: command ${KUSTOMIZE} is not found"
    exit 1
fi

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"
IMAGE_NAME=${IMAGE_NAME:-quay.io/open-cluster-management/multicluster-controlplane:latest}

source "${REPO_DIR}/test/bin/util.sh"

output="${REPO_DIR}/_output"
cluster_dir="${output}/kubeconfig"
agent_deploy_dir="${output}/agent/deploy"

mkdir -p ${cluster_dir}
mkdir -p ${agent_deploy_dir}

echo "Create a cluster with kind ..."
cluster="e2e-test"
external_host_ip="127.0.0.1"
external_host_port="30443"
kubeconfig="${cluster_dir}/${cluster}.kubeconfig"
cat << EOF | ${KIND} create cluster --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig --name ${cluster} --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: ${external_host_port}
    hostPort: 443
EOF

echo "Load $IMAGE_NAME to the cluster $cluster ..."
${KIND} load docker-image $IMAGE_NAME --name $cluster

echo "Deploy etcd in the cluster $cluster ..."
cp $REPO_DIR/hack/deploy/etcd/statefulset.yaml $REPO_DIR/hack/deploy/etcd/statefulset.yaml.tmp
sed -i "s/gp2/standard/g" $REPO_DIR/hack/deploy/etcd/statefulset.yaml
pushd ${REPO_DIR}
export KUBECONFIG=${kubeconfig}
export CFSSL_DIR=${output}/etcd_ca
make deploy-etcd
unset KUBECONFIG
popd
mv $REPO_DIR/hack/deploy/etcd/statefulset.yaml.tmp $REPO_DIR/hack/deploy/etcd/statefulset.yaml

namespace=multicluster-controlplane
echo "Deploy standalone controlplane in namespace $namespace ..."

${KUBECTL} --kubeconfig ${kubeconfig} delete ns $namespace --ignore-not-found
${KUBECTL} --kubeconfig ${kubeconfig} create ns $namespace

# copy etcd ca to helm folder
ca_dir=${CFSSL_DIR}
ca="${ca_dir}/ca.pem"
cert="${ca_dir}/client.pem"
key="${ca_dir}/client-key.pem"
# use helm to install controlplane
${HELM} install charts/multicluster-controlplane --kubeconfig ${kubeconfig} \
    -n $namespace \
    --set route.enabled=false \
    --set nodeport.enabled=true \
    --set nodeport.port=${external_host_port} \
    --set apiserver.externalHostname=${external_host_ip} \
    --set enableSelfManagement=true \
    --set image=${IMAGE_NAME} \
    --set autoApprovalBootstrapUsers="system:admin" \
    --set etcd.mode=external \
    --set 'etcd.servers={"http://etcd-0.etcd.multicluster-controlplane-etcd:2379","http://etcd-1.etcd.multicluster-controlplane-etcd:2379","http://etcd-2.etcd.multicluster-controlplane-etcd:2379"}' \
    --set-file etcd.ca="${ca}" \
    --set-file etcd.cert="${cert}" \
    --set-file etcd.certkey="${key}" \
    --generate-name

wait_command "${KUBECTL} --kubeconfig $kubeconfig -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig"
hubkubeconfig="${cluster_dir}/controlplane.kubeconfig"
${KUBECTL} --kubeconfig $kubeconfig -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig -ojsonpath='{.data.kubeconfig}' | base64 -d > ${hubkubeconfig}

# wait the controlplane is ready
wait_for_url "https://127.0.0.1/readyz"

echo "Deploy standalone controlplane agents ..."
cp -r ${REPO_DIR}/hack/deploy/agent/* $agent_deploy_dir

agent_namespace="multicluster-controlplane-agent"
${KUBECTL}  --kubeconfig ${kubeconfig} delete ns ${agent_namespace} --ignore-not-found
${KUBECTL}  --kubeconfig ${kubeconfig} create ns ${agent_namespace}

kubectl --kubeconfig $kubeconfig -n multicluster-controlplane get secrets multicluster-controlplane-svc-kubeconfig -ojsonpath='{.data.kubeconfig}' | base64 -d > ${agent_deploy_dir}/hub-kubeconfig

pushd $agent_deploy_dir
${KUSTOMIZE} edit set image quay.io/open-cluster-management/multicluster-controlplane=${IMAGE_NAME}
popd
${KUSTOMIZE} build ${agent_deploy_dir} | ${KUBECTL} --kubeconfig $kubeconfig -n ${agent_namespace} apply -f -

export HUBKUBECONFIG=${hubkubeconfig}
export SPOKEKUBECONFIG=${kubeconfig}
${output}/e2e.test -test.v -ginkgo.v
