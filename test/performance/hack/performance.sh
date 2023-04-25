#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"

source "${REPO_DIR}/test/bin/util.sh"

workdir=$REPO_DIR/_output/performance
output_suffix=${PERF_TEST_OUTPUT_SUFFIX:-"$(date '+%Y%m%dT%H%M%S')"}

mkdir -p $workdir

if [ -z "$KUBECONFIG" ]; then
  echo "KUBECONFIG is required for running controlplane"
  exit 1
fi

echo "##Deploy controlplane on $KUBECONFIG"
rm -f $REPO_DIR/multicluster-controlplane.kubeconfig
rm -f $REPO_DIR/hack/deploy/controlplane/ocmconfig.yaml

# deploy multicluster-controlplane
kubectl delete ns multicluster-controlplane --ignore-not-found --wait
kubectl create ns multicluster-controlplane

helm install charts/multicluster-controlplane \
    -n multicluster-controlplane \
    --set route.enabled=true \
    --set enableSelfManagement=false \
    --generate-name

# wait for multicluster-controlplane ready
wait_command "kubectl -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig"
hubkubeconfig="${workdir}/multicluster-controlplane.kubeconfig"
kubectl -n multicluster-controlplane get secrets multicluster-controlplane-kubeconfig -ojsonpath='{.data.kubeconfig}' | base64 -d > ${hubkubeconfig}

wait_command "kubectl --kubeconfig ${hubkubeconfig} get crds"

# perpare spoke cluster for test
kind delete clusters perf
kind create cluster --name perf --kubeconfig ${workdir}/perf.kubeconfig

kubectl --kubeconfig ${workdir}/perf.kubeconfig delete namespace open-cluster-management-agent --ignore-not-found
kubectl --kubeconfig ${workdir}/perf.kubeconfig create namespace open-cluster-management-agent

rm -rf /tmp/performance-test-agent

perftool=$REPO_DIR/bin/perftool
logfile="${workdir}/perf-tool-${output_suffix}.log"

$perftool create \
    --kubeconfig=$KUBECONFIG \
    --controlplane-kubeconfig=${hubkubeconfig} \
    --spoke-kubeconfig=${workdir}/perf.kubeconfig \
    --count=1000 \
    --work-count=5 \
    --output-file-suffix=$output_suffix \
    --output-dir=$workdir 2>$logfile
