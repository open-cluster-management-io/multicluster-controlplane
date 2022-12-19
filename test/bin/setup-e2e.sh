#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}
KUSTOMIZE=kustomize
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
hosting_cluster=${HOST_CLUSTER_NAME:-"hosting"}
number=${CONTROLPLANE_NUMBER:-1}
# Reuse certs will skip generate new ca/cert files under CERT_DIR
# it's useful with PRESERVE_ETCD=true because new ca will make existed service account secrets invalided
reuse_certs=${REUSE_CERTS:-false}
certs_dir=${CERT_DIR:-"$project_dir/test/resources/cert"}

source "$project_dir/test/bin/util.sh"

kube::util::ensure-gnu-sed
kube::util::test_openssl_installed
kube::util::ensure-cfssl

printf "\033[0;32m%s\n\033[0m" "## Create KinD clusters"
kubeconfig_dir=$project_dir/test/resources/kubeconfig && check_dir $kubeconfig_dir
kind create cluster --name $hosting_cluster --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig_dir/$hosting_cluster &
for i in $(seq 1 "${number}"); do
  kind create cluster --name controlplane$i-mc1 --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig_dir/controlplane$i-mc1 &
done
wait

external_host_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' ${hosting_cluster}-control-plane)
export KUBECONFIG=$kubeconfig_dir/$hosting_cluster

for i in $(seq 1 "${number}"); do

  namespace=controlplane$i && ($KUBECTL get ns $namespace || $KUBECTL create ns $namespace)
  printf "\033[0;32m%s\n\033[0m" "## Deploy standalone controlplane in namespace $namespace"
  # each controlpalne need different nodeport to expose it's server
  external_host_port="3008$i"
  deploy_dir=$project_dir/test/resources/$namespace && check_dir $deploy_dir

  if [[ "${reuse_certs}" != true ]]; then
    certs_dir=$deploy_dir/cert && check_dir $certs_dir
    generate_certs $certs_dir $external_host_ip $external_host_port
    set_service_accounts "${certs_dir}/kube-serviceaccount.key"
    cp ${certs_dir}/kube-aggregator.kubeconfig ${kubeconfig_dir}/$namespace
  fi

  cp -r $project_dir/hack/deploy/controlplane/* $deploy_dir
  sed -i "s/API_HOST/${external_host_ip}/" $deploy_dir/deployment.yaml
  sed -i 's/ClusterIP/NodePort/' $deploy_dir/service.yaml
  sed -i '/route\.yaml/d' $deploy_dir/kustomization.yaml
  sed -i "/targetPort.*/a  \ \ \ \ \ \ nodePort: $external_host_port" $deploy_dir/service.yaml

  # deploy the controplane
  cd $deploy_dir
  $KUSTOMIZE edit set namespace $namespace 
  echo "## Using the Controlplane Image: $IMAGE_NAME"
  $KUSTOMIZE edit set image quay.io/open-cluster-management/multicluster-controlplane=$IMAGE_NAME
  $KUSTOMIZE build $deploy_dir | $KUBECTL apply -f -
  kube::util::wait_for_url "https://${external_host_ip}:${external_host_port}/healthz" "apiserver: " 1 120 1 || { echo "Controlplane $namespace is not ready!" ; exit 1 ; }

  printf "\033[0;32m%s\n\033[0m" "## Join the managed cluster: ${namespace}-mc1 into controlplane: $namespace"
  # get bootstrap token of the OCM hub from controlplane api-server
  output=$(clusteradm --kubeconfig=${kubeconfig_dir}/${namespace} get token --use-bootstrap-token)
  token=$(echo $output | awk -F ' ' '{print $1}' | awk -F '=' '{print $2}')
  # join the controlplane
  clusteradm --kubeconfig=${kubeconfig_dir}/${namespace}-mc1 join --hub-token $token --hub-apiserver "https://${external_host_ip}:${external_host_port}" --cluster-name ${namespace}-mc1 --wait
  wait_appear "$KUBECTL --kubeconfig=${kubeconfig_dir}/${namespace} get csr --ignore-not-found | grep ^${namespace}-mc1 || true"
  clusteradm --kubeconfig=${kubeconfig_dir}/${namespace} accept --clusters ${namespace}-mc1
done

printf "%s\033[0;32m%s\n\033[0m" "[ Hosting ]: " "export KUBECONFIG=${kubeconfig_dir}/${hosting_cluster}"
for i in $(seq 1 "${number}"); do
  namespace=controlplane$i
  printf "%s\033[0;32m%s\n\033[0m" "[ Controlplane$i ]: " "export KUBECONFIG=${kubeconfig_dir}/${namespace}"
done