#!/usr/bin/env bash
REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../.." ; pwd -P)"

function wait_command() {
  local command="$1"; shift
  local wait_seconds="${1:-90}"; shift # 90 seconds as default timeout

  until [[ $((wait_seconds--)) -eq 0 ]] || eval "$command &> /dev/null" ; do sleep 1; done

  ((++wait_seconds))
}

function ensure_clusteradm() {
  bin_dir="${REPO_DIR}/_output/bin"
  mkdir -p ${bin_dir}
  pushd ${bin_dir}
  curl -LO https://raw.githubusercontent.com/open-cluster-management-io/clusteradm/main/install.sh
  chmod +x ./install.sh
  export INSTALL_DIR=$bin_dir
  ./install.sh 0.5.1
  unset INSTALL_DIR
  popd
}
