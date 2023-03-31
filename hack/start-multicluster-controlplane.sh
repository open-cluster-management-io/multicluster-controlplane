#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/.." ; pwd -P)"

GO_OUT=${GO_OUT:-"${REPO_DIR}/bin"}

WAIT_FOR_URL_API_SERVER=${WAIT_FOR_URL_API_SERVER:-60}
MAX_TIME_FOR_URL_API_SERVER=${MAX_TIME_FOR_URL_API_SERVER:-1}

FEATURE_GATES=${FEATURE_GATES:-"DefaultClusterSet=true,OpenAPIV3=false"}

# enable self managed
ENABLE_SELF_MANAGED=${ENABLE_SELF_MANAGED:-"false"}

# Stop right away if the build fails
set -e

source "${REPO_DIR}/hack/lib/init.sh"
source "${REPO_DIR}/hack/lib/yaml.sh"

# Shut down anyway if there's an error.
set +e

LOG_DIR=${LOG_DIR:-"/tmp"}

# TODO consider to have a flag to save/remove the output data
mkdir -p ${REPO_DIR}/_output/controlplane

function test_apiserver_off {
    # For the common local scenario, fail fast if server is already running.
    # this can happen if you run start-multicluster-controlplane.sh twice and kill etcd in between.
    if ! curl --silent -k -g "${apiserver_externalHostname}:${apiserver_port}" ; then
        echo "API SERVER secure port is free, proceeding..."
    else
        echo "ERROR starting API SERVER, exiting. Some process on ${apiserver_externalHostname} is serving already on ${apiserver_port}"
        exit 1
    fi
}

function cleanup() {
    echo "Cleaning up..."
    # Check if the API server is still running
    [[ -n "${APISERVER_PID-}" ]] && kube::util::read-array APISERVER_PIDS < <(pgrep -P "${APISERVER_PID}" ; ps -o pid= -p "${APISERVER_PID}")
    [[ -n "${APISERVER_PIDS-}" ]] && kill "${APISERVER_PIDS[@]}" 2>/dev/null
    exit 0
}

function healthcheck {
    if [[ -n "${APISERVER_PID-}" ]] && ! kill -0 "${APISERVER_PID}" 2>/dev/null; then
        warning_log "API server terminated unexpectedly, see ${APISERVER_LOG}"
        APISERVER_PID=
    fi
}

function print_color {
    message=$1
    prefix=${2:+$2: } # add colon only if defined
    color=${3:-1}     # default is red
    echo -n "$(tput bold)$(tput setaf "${color}")"
    echo "${prefix}${message}"
    echo -n "$(tput sgr0)"
}

function warning_log {
    print_color "$1" "W$(date "+%m%d %H:%M:%S")]" 1
}

function start_etcd {
    echo "etcd starting..."
    export ETCD_LOGFILE=${LOG_DIR}/etcd.log
    kube::etcd::start
}

function start_apiserver {
    APISERVER_LOG=${LOG_DIR}/kube-apiserver.log
    "${GO_OUT}/multicluster-controlplane" \
    "server" \
    --controlplane-config-dir="${config_dir}" \
    --feature-gates="${FEATURE_GATES}"  >"${APISERVER_LOG}" 2>&1 &
    APISERVER_PID=$!
    
    echo "Waiting for apiserver to come up"
    kube::util::wait_for_url "https://127.0.0.1:${apiserver_port}/healthz" "apiserver: " 1 "${WAIT_FOR_URL_API_SERVER}" "${MAX_TIME_FOR_URL_API_SERVER}" \
    || { echo "check apiserver logs: ${APISERVER_LOG}" ; exit 1 ; }
    
    echo "use 'kubectl --kubeconfig=${data_dir}/cert/kube-aggregator.kubeconfig' to access the controlplane"
    echo "$APISERVER_PID" > "${REPO_DIR}/_output/controlplane/controlpane_pid"
    chmod a+r ${data_dir}/cert/kube-aggregator.kubeconfig
}

###############################################################################
config_dir=${CONFIG_DIR:-"${REPO_DIR}/_output/controlplane"}
data_dir=${REPO_DIR}/_output/controlplane/.ocm

if [ ! -f "${config_dir}/ocmconfig.yaml" ] ; then
    if [[ "$(uname)" == "Darwin" ]]; then
        externalHostName=$(ifconfig en0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*')
    else
        externalHostName=$(ifconfig eth0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*')
    fi

    cat > "${config_dir}/ocmconfig.yaml" <<EOF
dataDirectory: ${data_dir}
apiserver:
  externalHostname: $externalHostName
  port: 9443
  caFile: 
  caKeyFile: 
etcd:
  mode: embed
EOF
fi

create_variables "${config_dir}/ocmconfig.yaml"

echo "multicluster-controlplane configurations in ${config_dir}/ocmconfig.yaml"
cat "${config_dir}/ocmconfig.yaml"
echo ""

if [[ ! -z "${etcd_mode:+x}" && "${etcd_mode}" == "external" ]]; then
    # validate that etcd is: not running, in path, and has minimum required version.
    echo "etcd validating..."
    kube::etcd::validate
fi

test_apiserver_off

trap cleanup EXIT

echo "Starting apiserver ..."
if [[ ! -z "${etcd_mode:+x}" && "${etcd_mode}" == "external" ]]; then
    start_etcd
fi
start_apiserver

while true; do sleep 1; healthcheck; done
