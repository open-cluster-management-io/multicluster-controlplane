#!/usr/bin/env bash

# TODO move this to e2e, for integration we should focus on code level test

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"

output="${REPO_DIR}/_output"
agent_dir="${output}/agent"

mkdir -p ${agent_dir}

managed_cluster="integration-test"
controlplane_kubeconfig="${output}/controlplane/.ocm/cert/kube-aggregator.kubeconfig"
kubeconfig="${agent_dir}/${managed_cluster}.kubeconfig"

source "${REPO_DIR}/test/bin/util.sh"
ensure_clusteradm

echo "Create a cluster with kind ..."
kind create cluster --name $managed_cluster --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig

echo "Start controlplane with command ..."
${REPO_DIR}/hack/start-multicluster-controlplane.sh &
pid=$!

wait_command "cat ${output}/controlplane/controlpane_pid"
if [ 0 -ne $? ]; then
  echo "Failed to start controlplane"
  cat /tmp/kube-apiserver.log
fi
cat ${output}/controlplane/controlpane_pid

apiserver=$(kubectl config view --kubeconfig ${controlplane_kubeconfig} -ojsonpath='{.clusters[0].cluster.server}')
echo "Joining the managed cluster $managed_cluster to ${apiserver} with clusteradm"
token_output=$(clusteradm --kubeconfig=${controlplane_kubeconfig} get token --use-bootstrap-token)
token=$(echo $token_output | awk -F ' ' '{print $1}' | awk -F '=' '{print $2}')
${output}/bin/clusteradm --kubeconfig=${kubeconfig} join --hub-token $token --hub-apiserver "${apiserver}" --cluster-name $managed_cluster --wait
${output}/bin/clusteradm --kubeconfig=${controlplane_kubeconfig} accept --clusters $managed_cluster
${output}/bin/clusteradm --kubeconfig=${kubeconfig} unjoin --cluster-name=$managed_cluster

echo "Stop the controlplane ..."
kill $pid
