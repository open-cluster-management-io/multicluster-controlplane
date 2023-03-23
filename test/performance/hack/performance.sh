#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"

workdir=$REPO_DIR/_output/performance
mkdir -p $workdir

if [ -z "$KUBECONFIG" ]; then
  echo "KUBECONFIG is required for running controlplane"
  exit 1
fi

echo "##Deploy controlplane on $KUBECONFIG"
rm -f $REPO_DIR/multicluster-controlplane.kubeconfig
rm -f $REPO_DIR/hack/deploy/controlplane/ocmconfig.yaml

kubectl delete ns multicluster-controlplane --ignore-not-found --wait
kubectl create ns multicluster-controlplane

pushd ${REPO_DIR}
make deploy
if [ 0 -ne $? ]; then
  echo "Failed to start controlplane"
  exit 1
fi
popd

kind delete clusters perf
kind create cluster --name perf --kubeconfig ${workdir}/perf.kubeconfig

kubectl --kubeconfig ${workdir}/perf.kubeconfig delete namespace open-cluster-management-agent --ignore-not-found
kubectl --kubeconfig ${workdir}/perf.kubeconfig create namespace open-cluster-management-agent

rm -rf /tmp/performance-test-agent

perftool=$REPO_DIR/bin/perftool
resmetricsfile="${workdir}/res-metrics-$(date '+%Y%m%dT%H%M%S').csv"
logfile="${workdir}/perf-tool-$(date '+%Y%m%dT%H%M%S').log"

$perftool create \
    --kubeconfig=$KUBECONFIG \
    --controlplane-kubeconfig=$REPO_DIR/multicluster-controlplane.kubeconfig \
    --spoke-kubeconfig=${workdir}/perf.kubeconfig \
    --count=1000 \
    --work-count=5 \
    --res-metrics-file-name=$resmetricsfile 2>$logfile
