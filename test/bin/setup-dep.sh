#!/usr/bin/env bash

bin_dir="$(go env GOPATH)/bin"

function check_kubectl() {
  if ! command -v kubectl >/dev/null 2>&1; then 
    echo "This script will install kubectl (https://kubernetes.io/docs/tasks/tools/install-kubectl/) on your machine"
    if [[ "$(uname)" == "Linux" ]]; then
        curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.18.0/bin/linux/amd64/kubectl
    elif [[ "$(uname)" == "Darwin" ]]; then
        curl -LO https://storage.googleapis.com/kubernetes-release/release/v1.18.0/bin/darwin/amd64/kubectl
    fi
    chmod +x ./kubectl
    sudo mv ./kubectl ${bin_dir}/kubectl
  fi
  echo "kubectl version: $(kubectl version --client --short)"
}

function check_kustomize() {
  if ! command -v kustomize >/dev/null 2>&1; then 
    echo "This script will install kustomize on your machine"
    curl -LO "https://raw.githubusercontent.com/kubernetes-sigs/kustomize/master/hack/install_kustomize.sh"
    chmod +x ./install_kustomize.sh
    source ./install_kustomize.sh 3.8.2 ${bin_dir}
  fi
  echo "kustomize version: $(kustomize version)"
}

function check_clusteradm() {
  if ! command -v clusteradm >/dev/null 2>&1; then 
    curl -LO https://raw.githubusercontent.com/open-cluster-management-io/clusteradm/main/install.sh
    chmod +x ./install.sh
    INSTALL_DIR=$bin_dir
    source ./install.sh 0.4.1
    rm ./install.sh
  fi
  echo "clusteradm path: $(which clusteradm)"
}

function check_ginkgo() {
  if ! command -v ginkgo >/dev/null 2>&1; then 
    go install github.com/onsi/ginkgo/v2/ginkgo@v2.5.0
    sudo mv $(go env GOPATH)/bin/ginkgo ${bin_dir}/ginkgo
  fi 
  echo "ginkgo version: $(ginkgo version)"
}

function check_cfssl() {
  if ! command -v cfssl >/dev/null 2>&1; then 
    curl --retry 10 -L -o cfssl https://github.com/cloudflare/cfssl/releases/download/v1.5.0/cfssl_1.5.0_linux_amd64
    chmod +x cfssl || true
    sudo mv cfssl ${bin_dir}/cfssl
  fi 
  if ! command -v cfssljson >/dev/null 2>&1; then 
    curl --retry 10 -L -o cfssljson https://github.com/cloudflare/cfssl/releases/download/v1.5.0/cfssljson_1.5.0_linux_amd64
    chmod +x cfssljson || true
    sudo mv cfssljson ${bin_dir}/cfssljson
  fi 
}

check_kubectl
check_kustomize
check_clusteradm
check_ginkgo
check_cfssl
