
#!/usr/bin/env bash
set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}

project_dir="$(cd "$(dirname ${BASH_SOURCE[0]})/../.." ; pwd -P)"
kubeconfig_dir=$project_dir/test/resources/integration/kubeconfig

cp="integration-cp"
mc="integration-mc"
$KUBECTL --kubeconfig=${kubeconfig_dir}/${cp} delete mcl $mc

pid=$(cat ${project_dir}/test/resources/integration/controlpane_pid)
if [ -n "$pid" ]; then
  echo "kill the controlplane process: $pid"
  sudo kill -9 $pid
fi

mc="integration-mc"
kind delete cluster --name $mc
rm -rf ${project_dir}/test/resources/integration