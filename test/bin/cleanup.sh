#!/bin/bash

set -o nounset
set -o pipefail

host=${HOST_CLUSTER_NAME:-"hosting"}
number=${CONTROLPLANE_NUMBER:-2}
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
echo "Controlplane number : $number"

kind delete cluster --name $host
for i in $(seq 1 "${number}"); do
  namespace=controlplane$i
  kind delete cluster --name ${namespace}-mc1
  rm -r $project_dir/hack/deploy/cert-${namespace}
done
rm -r $project_dir/test/resources/controlplane/cert
rm -r $project_dir/test/resources/controlplane/deployment.yaml
rm -r $project_dir/test/resources/controlplane/kustomization.yaml
rm -r $project_dir/test/resources/controlplane/service.yaml
rm -r $project_dir/test/resources/kubeconfig







