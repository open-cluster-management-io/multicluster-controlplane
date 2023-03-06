#!/usr/bin/env bash
set -o nounset
set -o pipefail

KUBECTL=${KUBECTL:-kubectl}

project_dir="$(cd "$(dirname ${BASH_SOURCE[0]})/../.." ; pwd -P)"
source "$project_dir/test/bin/util.sh"

kubeconfig_dir=$project_dir/test/resources/integration/kubeconfig
check_dir $kubeconfig_dir

kube::util::ensure-gnu-sed

controlplane_bin=${CONTROPLANE_BIN:-"${project_dir}/bin"}
network_interface=${NETWORK_INTERFACE:-"eth0"}
api_host_ip=$(ifconfig $network_interface | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*')
if [ ! $api_host_ip ] ; then
    echo "api_host_ip should be set"
    exit 1
fi
api_secure_port=${API_SECURE_PORT:-"9443"}

config_dir="${project_dir}/test/resources/integration"
cert_dir=${CERT_DIR:-"${config_dir}/cert"}
check_dir $cert_dir

CONTROLPLANE_SUDO=$(test -w "${cert_dir}" || echo "sudo -E")
function start_apiserver {
    apiserver_log=${project_dir}/test/resources/integration/kube-apiserver.log

    ${CONTROLPLANE_SUDO} "${controlplane_bin}/multicluster-controlplane" \
    "server" \
    --config-file=${config_dir}/ocmconfig.yaml \
    --feature-gates="DefaultClusterSet=true,OpenAPIV3=false"  >"$apiserver_log" 2>&1 &
    apiserver_pid=$!
    echo "$apiserver_pid" > ${project_dir}/test/resources/integration/controlpane_pid

    echo "Waiting for apiserver to come up"
    kube::util::wait_for_url "https://${api_host_ip}:${api_secure_port}/healthz" "apiserver: " 1 60 1 \
    || { echo "check apiserver logs: $apiserver_log" ; exit 1 ; }
    
    cp ${cert_dir}/kube-aggregator.kubeconfig ${kubeconfig_dir}/integration-cp
    echo "use 'kubectl --kubeconfig=${kubeconfig_dir}/integration-cp' to use the aggregated API server" 
}

if ! curl --silent -k -g "${api_host_ip}:${api_secure_port}" ; then
    echo "API SERVER secure port is free, proceeding..."
    write_ocm_config ${config_dir} $config_dir $api_host_ip $api_secure_port "embed" "/registry"
    start_apiserver
else
    echo "Some(API SERVER) process on ${api_host_ip} is serving already on ${api_secure_port}"
fi

# create a kind managed cluster 
mc="integration-mc"
kind get clusters | grep $mc 2>/dev/null || kind create cluster --name $mc --image "kindest/node:v1.24.7" --kubeconfig $kubeconfig_dir/$mc

# join the managed cluster to controlplane
if [[ -z $($KUBECTL --kubeconfig=${kubeconfig_dir}/integration-cp get mcl $mc --ignore-not-found) ]]; then 
  echo "Joining the managed cluster: $mc to integration-cp"
  output=$(clusteradm --kubeconfig=${kubeconfig_dir}/integration-cp get token --use-bootstrap-token)
  token=$(echo $output | awk -F ' ' '{print $1}' | awk -F '=' '{print $2}')
  clusteradm --kubeconfig=${kubeconfig_dir}/${mc} join --hub-token $token --hub-apiserver "https://${api_host_ip}:${api_secure_port}" --cluster-name $mc --wait
  clusteradm --kubeconfig=${kubeconfig_dir}/integration-cp accept --clusters $mc
fi 

printf "%s\033[0;32m%s\n\033[0m" "[ Integration Controlplane ]: " "export KUBECONFIG=${kubeconfig_dir}/integration-cp"
