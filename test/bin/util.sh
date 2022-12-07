#!/bin/bash

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
source "$project_dir/hack/lib/init.sh"

function generate_certs {
    CERT_DIR=$1
    API_HOST_IP=$2
    API_HOST_PORT=$3

    # Ensure CERT_DIR is created for auto-generated crt/key and kubeconfig
    mkdir -p "${CERT_DIR}"
    CONTROLPLANE_SUDO=$(test -w "${CERT_DIR}" || echo "sudo -E")

    # in the flags of apiserver --service-cluster-ip-range
    FIRST_SERVICE_CLUSTER_IP=${FIRST_SERVICE_CLUSTER_IP:-10.0.0.1}

    # create ca
    kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" server '"server auth"'
    kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" client '"client auth"'
    kube::util::create_signing_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" request-header '"proxy client auth"'
    
    # serving cert for kube-apiserver
    ROOT_CA_FILE="serving-kube-apiserver.crt"
    kube::util::create_serving_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "server-ca" kube-apiserver kubernetes.default kubernetes.default.svc "localhost" "${API_HOST_IP}" "${FIRST_SERVICE_CLUSTER_IP}"
    
    # create client certs signed with client-ca, given id, given CN and a number of groups
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" 'client-ca' admin system:admin system:masters
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" 'client-ca' kube-apiserver kube-apiserver
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" 'client-ca' kube-aggregator system:kube-aggregator system:masters
    
    # create matching certificates for kube-aggregator
    kube::util::create_serving_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "server-ca" kube-aggregator api.kube-public.svc "localhost" "${API_HOST_IP}"
    kube::util::create_client_certkey "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "request-header-ca" auth-proxy system:auth-proxy
    
    # generate kubeconfig
    kube::util::write_client_kubeconfig "${CONTROLPLANE_SUDO}" "${CERT_DIR}" "${ROOT_CA_FILE}" "${API_HOST_IP}" "${API_HOST_PORT}" kube-aggregator
}

function set_service_accounts {
    output_file=$1
    # Generate ServiceAccount key if needed
    if [[ ! -f "${output_file}" ]]; then
      mkdir -p "$(dirname "${output_file}")"
      openssl genrsa -out "${output_file}" 2048 2>/dev/null     # create user private key
    fi
}

function check_dir() {
  if [ ! -d "$1" ];then
    mkdir -p "$1"
  fi
}

function wait_disappear() {
  command=$1
  timeout=${2:-180}
  second=0
  while [ -n "$(eval $command)" ]; do 
    if [ $second -gt $timeout ]; then
      echo " Timeout($second) waiting for getting empty response: $command"
      exit 1
    fi 
    echo " Waitting($second) for getting empty response: $command"
    sleep 2
    (( second = second + 2 ))
  done
  echo "* CMD get reponse: $(eval $command)"
}

function wait_appear() {
  command=$1
  timeout=${2:-180}
  second=0
  while [ -z "$(eval $command)" ]; do 
    if [ $second -gt $timeout ]; then
      echo " Timeout($second) waiting for getting response: $command"
      exit 1
    fi 
     echo " Waitting($second) for getting response: $command"
    sleep 2
    (( second = second + 2 ))
  done
  echo "* CMD get reponse: $(eval $command)"
}

function print_color {
    message=$1
    prefix=${2:+$2: } # add color only if defined
    color=${3:-1}     # default is red
    echo -n "$(tput bold)$(tput setaf "${color}")"
    echo "${prefix}${message}"
    echo -n "$(tput sgr0)"
}

function warning_log {
    print_color "$1" "W$(date "+%m%d %H:%M:%S")]" 1
}