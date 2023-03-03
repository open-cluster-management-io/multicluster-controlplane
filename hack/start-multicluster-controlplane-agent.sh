#!/usr/bin/env bash

KUBE_ROOT=$(pwd)
GO_OUT=${GO_OUT:-"${KUBE_ROOT}/bin"}

KUBECONFIG=${KUBECONFIG:-"$HOME/.kube/config"}
CLUSTER_NAME=${CLUSTER_NAME:-"cluster1"}

BOOTSTRAP_KUBECONFIG=""
if [ ! "$BOOTSTRAP_KUBECONFIG" ]; then
    echo "BOOTSTRAP_KUBECONFIG is required"
    exit 1
fi

hubkubeconfigdir="${KUBE_ROOT}/_output/spoke/hub-kubeconfig"
mkdir -p $hubkubeconfigdir

kubectl delete ns open-cluster-management-agent --ignore-not-found
kubectl create ns open-cluster-management-agent

"${GO_OUT}/multicluster-controlplane" \
"agent" \
--cluster-name="${CLUSTER_NAME}" \
--bootstrap-kubeconfig="${BOOTSTRAP_KUBECONFIG}" \
--hub-kubeconfig-dir="${hubkubeconfigdir}" \
--spoke-kubeconfig="$KUBECONFIG"
