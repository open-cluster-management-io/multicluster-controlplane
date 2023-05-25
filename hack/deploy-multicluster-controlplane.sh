#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/.." ; pwd -P)"

set -o nounset
set -o pipefail
set -o errexit

source ${REPO_DIR}/hack/lib/deps.sh

check_kubectl
check_helm

uninstall=${1:-""}
only_render=${ONLY_RENDER:-false}
controlplane_namespace=${HUB_NAME}
self_management=${SELF_MANAGEMENT:-false}
image=${IMAGE_NAME:-"quay.io/stolostron/multicluster-controlplane:latest"}
external_hostname=${EXTERNAL_HOSTNAME:-""}
node_port=${NODE_PORT:-0}
etcd_mod=${ETCD_MOD:-""}

if [ "$uninstall"x = "uninstall"x ]; then
    helm -n ${HUB_NAME} uninstall multicluster-controlplane
    kubectl delete ns multicluster-controlplane --ignore-not-found
    exit 0
fi

echo "Deploy multicluster controlplane on the namespace $controlplane_namespace"
echo "Image: $image"

args="--create-namespace"
args="$args --set enableSelfManagement=${self_management}"
args="$args --set image=${image}"
args="$args --set autoApprovalBootstrapUsers=system:admin"

if [ 0 -eq $node_port ]; then
    args="$args --set route.enabled=true"
else
    args="$args --set route.enabled=false"
    args="$args --set nodeport.enabled=true"
    args="$args --set nodeport.port=${node_port}"
    args="$args --set apiserver.externalHostname=${external_hostname}"
    args="$args --set apiserver.externalPort=${node_port}"
fi

if [ "$etcd_mod"x = "external"x ]; then
    args="$args --set etcd.mode=external"
    args="$args --set etcd.servers={\"http://etcd-0.etcd.multicluster-controlplane-etcd:2379\",\"http://etcd-1.etcd.multicluster-controlplane-etcd:2379\",\"http://etcd-2.etcd.multicluster-controlplane-etcd:2379\"}"
    args="$args --set-file etcd.ca=${REPO_DIR}/_output/etcd/deploy/cert-etcd/ca.pem"
    args="$args --set-file etcd.cert=${REPO_DIR}/_output/etcd/deploy/cert-etcd/client.pem"
    args="$args --set-file etcd.certkey=${REPO_DIR}/_output/etcd/deploy/cert-etcd/client-key.pem"
fi

if [ "${only_render}" = true ]; then
    mkdir -p ${REPO_DIR}/_output/controlplane
    deploy_file=${REPO_DIR}/_output/controlplane/${controlplane_namespace}.yaml
    helm template multicluster-controlplane ${REPO_DIR}/charts/multicluster-controlplane -n $controlplane_namespace $args > ${deploy_file}
    exit 0
fi

helm install multicluster-controlplane ${REPO_DIR}/charts/multicluster-controlplane -n $controlplane_namespace $args
