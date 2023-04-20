#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/.." ; pwd -P)"

GO_OUT=${GO_OUT:-"${REPO_DIR}/bin"}

WAIT_FOR_URL_API_SERVER=${WAIT_FOR_URL_API_SERVER:-60}
MAX_TIME_FOR_URL_API_SERVER=${MAX_TIME_FOR_URL_API_SERVER:-1}

CONFIG_DIR=${CONFIG_DIR:-"${REPO_DIR}/_output/controlplane"}
DATA_DIR=${DATA_DIR:-"${REPO_DIR}/_output/controlplane/.ocm"}

CONTROLPLANE_PORT=${CONTROLPLANE_PORT:-"9443"}
ETCD_MODE=${ETCD_MODE:-"embed"}
BOOTSTRAP_USERS=${BOOTSTRAP_USERS:-""}
FEATURE_GATES=${FEATURE_GATES:-"DefaultClusterSet=true,ManagedClusterAutoApproval=true"}

# Stop right away if the build fails
set -e

source "${REPO_DIR}/hack/lib/init.sh"

# Shut down anyway if there's an error.
set +e

LOG_DIR=${LOG_DIR:-"/tmp"}
APISERVER_LOG=${LOG_DIR}/kube-apiserver.log

mkdir -p ${DATA_DIR}

function test_apiserver_off {
    # For the common local scenario, fail fast if server is already running.
    # this can happen if you run start-multicluster-controlplane.sh twice and kill etcd in between.
    if ! curl --silent -k -g "127.0.0.1:${CONTROLPLANE_PORT}" ; then
        echo "API SERVER secure port is free, proceeding..."
    else
        echo "ERROR starting API SERVER, exiting. Some process is serving already on ${CONTROLPLANE_PORT}"
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
    "${GO_OUT}/multicluster-controlplane" \
    "server" \
    --controlplane-config-dir="${CONFIG_DIR}" \
    --cluster-auto-approval-users="${BOOTSTRAP_USERS}" \
    --feature-gates="${FEATURE_GATES}"  >"${APISERVER_LOG}" 2>&1 &

    APISERVER_PID=$!
    
    echo "Waiting for apiserver to come up"
    kube::util::wait_for_url "https://127.0.0.1:${CONTROLPLANE_PORT}/healthz" "apiserver: " 1 "${WAIT_FOR_URL_API_SERVER}" "${MAX_TIME_FOR_URL_API_SERVER}" \
    || { echo "check apiserver logs: ${APISERVER_LOG}" ; exit 1 ; }
    
    echo "use 'kubectl --kubeconfig=${DATA_DIR}/cert/kube-aggregator.kubeconfig' to access the controlplane"
    echo "$APISERVER_PID" > "${REPO_DIR}/_output/controlplane/controlpane_pid"
    chmod a+r ${DATA_DIR}/cert/kube-aggregator.kubeconfig
}

###############################################################################
if [ ! -f "${CONFIG_DIR}/ocmconfig.yaml" ] ; then
    cat > "${CONFIG_DIR}/ocmconfig.yaml" <<EOF
dataDirectory: ${DATA_DIR}
apiserver:
  port: ${CONTROLPLANE_PORT}
etcd:
  mode: ${ETCD_MODE}
EOF
fi

echo "multicluster-controlplane configurations in ${CONFIG_DIR}/ocmconfig.yaml"
cat "${CONFIG_DIR}/ocmconfig.yaml"
echo ""

if [[ "${ETCD_MODE}" == "external" ]]; then
    # validate that etcd is: not running, in path, and has minimum required version.
    echo "etcd validating..."
    kube::etcd::validate
fi

test_apiserver_off

trap cleanup EXIT

if [[ "${ETCD_MODE}" == "external" ]]; then
    start_etcd
fi

echo "Starting apiserver ..."
start_apiserver

while true; do sleep 1; healthcheck; done
