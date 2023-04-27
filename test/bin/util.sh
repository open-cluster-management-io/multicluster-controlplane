#!/usr/bin/env bash
REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../.." ; pwd -P)"

function wait_command() {
  local command="$1"; shift
  local wait_seconds="${1:-90}"; shift # 90 seconds as default timeout

  until [[ $((wait_seconds--)) -eq 0 ]] || eval "$command &> /dev/null" ; do sleep 1; done

  ((++wait_seconds))
}

function wait_for_url() {
  local url="$1"; shift
  local times="${1:-90}"; shift # 90 seconds as default timeout

  command -v curl >/dev/null || {
    echo "curl must be installed"
    exit 1
  }

  local i
  for i in $(seq 1 "${times}"); do
    local out
    if out=$(curl -gkfs "${url}" 2>/dev/null); then
      echo "On try ${i}, ${url}: ${out}"
      return 0
    fi
    sleep 1
  done
  
  echo "Timed out waiting for ${url}; tried ${times}"
  exit 1
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
