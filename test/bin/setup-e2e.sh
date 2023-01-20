#!/bin/bash

set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}
KUSTOMIZE=kustomize
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
hosting_cluster=${HOST_CLUSTER_NAME:-"hosting"}
number=${CONTROLPLANE_NUMBER:-1}

etcdns=${ETCD_NS:-"multicluster-controlplane-etcd"}

set -e
source "$project_dir/test/bin/util.sh"
kube::util::ensure-gnu-sed
set +e

printf "\033[0;32m%s\n\033[0m" "## Create KinD clusters"
kubeconfig_dir=$project_dir/test/resources/kubeconfig && check_dir $kubeconfig_dir
kind create cluster --name $hosting_cluster --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig_dir/$hosting_cluster &
for i in $(seq 1 "${number}"); do
  kind create cluster --name controlplane$i-mc1 --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig_dir/controlplane$i-mc1 &
done
wait

external_host_ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' ${hosting_cluster}-control-plane)
export KUBECONFIG=$kubeconfig_dir/$hosting_cluster

# load controlplane image to the hosting cluster
kind load docker-image $IMAGE_NAME --name $hosting_cluster

# external etcd in kind
cp $project_dir/hack/deploy/etcd/statefulset.yaml $project_dir/hack/deploy/etcd/statefulset.yaml.tmp
sed -i "s/gp2/standard/g" $project_dir/hack/deploy/etcd/statefulset.yaml
cd $project_dir && make deploy-etcd
mv $project_dir/hack/deploy/etcd/statefulset.yaml.tmp $project_dir/hack/deploy/etcd/statefulset.yaml

for i in $(seq 1 "${number}"); do

  namespace=controlplane$i && ($KUBECTL get ns $namespace || $KUBECTL create ns $namespace)
  printf "\033[0;32m%s\n\033[0m" "## Deploy standalone controlplane in namespace $namespace"
  # each controlpalne need different nodeport to expose it's server
  external_host_port="3008$i"
  deploy_dir=$project_dir/test/resources/$namespace && check_dir $deploy_dir

  cp -r $project_dir/hack/deploy/controlplane/* $deploy_dir

  # expose apiserver
  sed -i 's/ClusterIP/NodePort/' $deploy_dir/service.yaml
  sed -i '/route\.yaml/d' $deploy_dir/kustomization.yaml
  sed -i "/targetPort.*/a  \ \ \ \ \ \ nodePort: $external_host_port" $deploy_dir/service.yaml

  # setup external etcd 
  CERTS_DIR=$deploy_dir/certs
  mkdir -p ${CERTS_DIR}
  cp -f ${project_dir}/multicluster_ca/ca.pem ${CERTS_DIR}/etcd-ca.crt
  cp -f ${project_dir}/multicluster_ca/client.pem ${CERTS_DIR}/etcd-client.crt
  cp -f ${project_dir}/multicluster_ca/client-key.pem ${CERTS_DIR}/etcd-client.key

  sed -i "$(sed -n  '/  - ocmconfig.yaml/=' $deploy_dir/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-client.key" $deploy_dir/kustomization.yaml
  sed -i "$(sed -n  '/  - ocmconfig.yaml/=' $deploy_dir/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-client.crt" $deploy_dir/kustomization.yaml
  sed -i "$(sed -n  '/  - ocmconfig.yaml/=' $deploy_dir/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-ca.crt" $deploy_dir/kustomization.yaml

  write_ocm_config $deploy_dir /.ocmconfig $external_host_ip 9443 external "${namespace}"
  sed -i "$(sed -n  '/etcd:/=' ${deploy_dir}/ocmconfig.yaml) a \  servers: " ${deploy_dir}/ocmconfig.yaml
  etcd_size=$(${KUBECTL} -n ${etcdns} get statefulset.apps/etcd -o jsonpath='{.spec.replicas}')
  for((i=0;i<$etcd_size;i++))
  do
    etcd_service="http://etcd-"$i".etcd.${etcdns}:2379"
    sed -i "$(sed -n '/servers:/=' ${deploy_dir}/ocmconfig.yaml) a \  - ${etcd_service}" ${deploy_dir}/ocmconfig.yaml
  done 
  sed -i "$(sed -n '/configDirectory:/=' ${deploy_dir}/ocmconfig.yaml) a \deployToOCP: true" ${deploy_dir}/ocmconfig.yaml
  sed -i "s@ocmconfig.yaml@${deploy_dir}/ocmconfig.yaml@g" $deploy_dir/kustomization.yaml

  # deploy the controplane
  cd $deploy_dir
  $KUSTOMIZE edit set namespace $namespace 
  echo "## Using the Controlplane Image: $IMAGE_NAME"
  $KUSTOMIZE edit set image quay.io/open-cluster-management/multicluster-controlplane=$IMAGE_NAME
  $KUSTOMIZE build $deploy_dir | $KUBECTL apply -f -
  
  wait_for_kubeconfig_secret "${kubeconfig_dir}" "${namespace}" "${KUBECONFIG}"
  if [ $? -ne 0 ]; then 
    exit 1
  fi 
  sed -i "s/443/${external_host_port}/g" ${kubeconfig_dir}/${namespace}
  check_multicluster_controlplane "${kubeconfig_dir}" "${namespace}"

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