#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

KUBE_ROOT=$(pwd)

configfile=${KUBE_ROOT}/ocmconfig.yaml
if [ ! -f "$configfile" ] ; then 
    echo "config file $configfile do not exist, create it first!" 
    exit 1
fi 

GO_OUT=${GO_OUT:-"${KUBE_ROOT}/bin"}

WAIT_FOR_URL_API_SERVER=${WAIT_FOR_URL_API_SERVER:-60}
MAX_TIME_FOR_URL_API_SERVER=${MAX_TIME_FOR_URL_API_SERVER:-1}

FEATURE_GATES=${FEATURE_GATES:-"DefaultClusterSet=true,OpenAPIV3=false"}

# enable self managed
ENABLE_SELF_MANAGED=${ENABLE_SELF_MANAGED:-"true"}

# Stop right away if the build fails
set -e

source "${KUBE_ROOT}/hack/lib/init.sh"
kube::util::ensure-gnu-sed

source "${KUBE_ROOT}/hack/lib/yaml.sh"
# Shut down anyway if there's an error.
set +e

LOG_DIR=${LOG_DIR:-"/tmp"}
CONTROLPLANE_SUDO=$(echo "sudo -E")

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

cleanup()
{
    echo "Cleaning up..."
    # Check if the API server is still running
    [[ -n "${APISERVER_PID-}" ]] && kube::util::read-array APISERVER_PIDS < <(pgrep -P "${APISERVER_PID}" ; ps -o pid= -p "${APISERVER_PID}")
    [[ -n "${APISERVER_PIDS-}" ]] && sudo kill "${APISERVER_PIDS[@]}" 2>/dev/null
    exit 0
}

function healthcheck {
    if [[ -n "${APISERVER_PID-}" ]] && ! sudo kill -0 "${APISERVER_PID}" 2>/dev/null; then
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
    self_managed_arg=""
    controlplane_cert_dir_arg=""
    if [[ "${ENABLE_SELF_MANAGED}" == true ]]; then
        self_managed_arg="--self-management"
        controlplane_cert_dir_arg="--controlplane-cert-dir=${configDirectory}/cert"
    fi

    APISERVER_LOG=${LOG_DIR}/kube-apiserver.log
    ${CONTROLPLANE_SUDO} "${GO_OUT}/multicluster-controlplane" \
    "server" \
    "${self_managed_arg}" \
    "${controlplane_cert_dir_arg}" \
    --config-file="${configfile}" \
    --feature-gates="${FEATURE_GATES}"  >"${APISERVER_LOG}" 2>&1 &
    APISERVER_PID=$!
    
    echo "Waiting for apiserver to come up"
    kube::util::wait_for_url "https://127.0.0.1:${apiserver_port}/healthz" "apiserver: " 1 "${WAIT_FOR_URL_API_SERVER}" "${MAX_TIME_FOR_URL_API_SERVER}" \
    || { echo "check apiserver logs: ${APISERVER_LOG}" ; exit 1 ; }
    
    echo "use 'kubectl --kubeconfig=${configDirectory}/cert/kube-aggregator.kubeconfig' to use the aggregated API server"
}


###############################################################################
create_variables $configfile

echo "environment checking..."
if [[ ! -z "${deployToOCP:+x}" ]];then 
    if [[ "${deployToOCP}" == "true" ]]; then
    echo "deployToOCP should be set to false while running multicluster-controplane locally!"
    exit 1
    fi
fi

if [[ -z "${apiserver_externalHostname:+x}" ]]; then 
    echo "apiserver_externalHostname not set, using local IP address..."
    if [[ "$(uname)" == "Darwin" ]]; then
        apiserver_externalHostname=$(ifconfig en0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*')
    else
        apiserver_externalHostname=$(ifconfig eth0 | grep -Eo 'inet (addr:)?([0-9]*\.){3}[0-9]*' | grep -Eo '([0-9]*\.){3}[0-9]*')
    fi
fi
if [ ! $apiserver_externalHostname ] ; then
    echo "cannot get local IP address, apiserver_externalHostname should be set"
    exit 1
else 
    sed -i '/externalHostname/d' ${KUBE_ROOT}/ocmconfig.yaml
    sed -i "$(sed -n  '/apiserver/=' ${KUBE_ROOT}/ocmconfig.yaml) a \  externalHostname: ${apiserver_externalHostname}" ${KUBE_ROOT}/ocmconfig.yaml
fi

if [[ -z "${apiserver_port:+x}" ]]; then 
    echo "apiserver_port should be set"
    exit 1
fi

if [[ ! -z "${etcd_mode:+x}" && "${etcd_mode}" == "external" ]]; then
    # validate that etcd is: not running, in path, and has minimum required version.
    echo "etcd validating..."
    kube::etcd::validate
fi

test_apiserver_off

trap cleanup EXIT

echo "Starting service now!"
if [[ ! -z "${etcd_mode:+x}" && "${etcd_mode}" == "external" ]]; then
    start_etcd
fi
start_apiserver

while true; do sleep 1; healthcheck; done
