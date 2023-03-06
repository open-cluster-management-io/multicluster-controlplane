#!/bin/bash

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
source "$project_dir/hack/lib/init.sh"

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

# creates a ocmconfig: args are dest-dir, host, port, etcd_mode
function write_ocm_config {
  local dest_dir=$1
  local config_dir=$2
  local api_host=$3
  local api_port=$4
  local etcd_mode=$5
  local etcd_prefix=$6

  cat <<EOF | tee ${dest_dir}/ocmconfig.yaml > /dev/null
configDirectory: $config_dir
apiserver:
  externalHostname: $api_host
  port: $api_port
etcd:
  mode: $etcd_mode
  prefix: $etcd_prefix
  caFile: /controlplane_config/etcd-ca.crt
  certFile: /controlplane_config/etcd-client.crt
  keyFile: /controlplane_config/etcd-client.key
EOF
}

function wait_for_kubeconfig_secret {
  local kubeconfig_dir=$1
  local namespace=$2
  local kubeconfigfile=$3

  echo "Waiting for kubeconfig..."
  for i in {1..10}; do
      kubectl --kubeconfig ${kubeconfigfile} -n ${namespace} get secret kubeconfig &>/dev/null
      if [ $? -eq 0 ]; then
          kubectl --kubeconfig ${kubeconfigfile} -n ${namespace} get secret kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > ${kubeconfig_dir}/${namespace}
          return 0
      fi
      sleep 2
  done
  echo "kubeconfig secret not appear..."
  return 1
}

function check_multicluster_controlplane {
  local kubeconfig_dir=$1
  local hub_name=$2

  echo "Checking multicluster-controlplane..."
  for i in {1..10}; do
      RESULT=$(kubectl --kubeconfig=${kubeconfig_dir}/${hub_name} api-resources | grep managedclusters)
      if [ -n "${RESULT}" ]; then
          echo "#### multicluster-controlplane ${hub_name} is ready ####"
          break
      fi
      
      if [ $i -eq 10 ]; then
          echo "!!!!!!!!!!  the multicluster-controlplane ${hub_name} is not ready within 30s"
          kubectl -n ${hub_name} get pods
          
          exit 1
      fi
      sleep 2
  done
}
