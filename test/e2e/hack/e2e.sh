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
pushd ${REPO_DIR}
export KUBECONFIG=${kubeconfig}
STORAGE_CLASS_NAME="standard" make deploy-etcd
unset KUBECONFIG
popd

namespace=multicluster-controlplane
echo "Deploy standalone controlplane in namespace $namespace ..."

${KUBECTL} --kubeconfig ${kubeconfig} delete ns $namespace --ignore-not-found
${KUBECTL} --kubeconfig ${kubeconfig} create ns $namespace

pushd ${REPO_DIR}
export HUB_NAME=${namespace}
export EXTERNAL_HOSTNAME=${external_host_ip}
export NODE_PORT="${external_host_port}"
export SELF_MANAGEMENT=true
export KUBECONFIG=${kubeconfig}
make deploy
unset KUBECONFIG
unset HUB_NAME
unset EXTERNAL_HOSTNAME
unset NODE_PORT
unset SELF_MANAGEMENT
popd

wait_command "${KUBECTL} --kubeconfig $kubeconfig -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig"
${KUBECTL} --kubeconfig $kubeconfig -n multicluster-controlplane -n multicluster-controlplane logs -l app=multicluster-controlplane --tail=-1

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
