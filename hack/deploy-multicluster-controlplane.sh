#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/.." ; pwd -P)"

KUBECTL=${KUBECTL:-"kubectl"}
KUSTOMIZE=${KUSTOMIZE:-"kustomize"}

if ! command -v $KUBECTL >/dev/null 2>&1; then
  echo "ERROR: command $KUBECTL is not found"
  exit 1
fi

if ! command -v $KUSTOMIZE >/dev/null 2>&1; then
  echo "ERROR: command $KUSTOMIZE is not found"
  exit 1
fi

HUB_NAME=${HUB_NAME:-"multicluster-controlplane"}
IMAGE_NAME=${IMAGE_NAME:-"quay.io/open-cluster-management/multicluster-controlplane"}

# this is needed for the controlplane deploy
echo "* Testing connection"
HOST_URL=$(${KUBECTL} -n openshift-console get routes console -o jsonpath='{.status.ingress[0].routerCanonicalHostname}')
if [ $? -ne 0 ]; then
    echo "ERROR: make sure you are logged into an OpenShift Container Platform before running this script"
    exit 1
fi

# shorten to the basedomain
DEFAULT_HOST_POSTFIX=${HOST_URL/#router-default./}
API_HOST_POSTFIX=${API_HOST_POSTFIX:-$DEFAULT_HOST_POSTFIX}
if [ ! $API_HOST_POSTFIX ] ; then
    echo "API_HOST_POSTFIX should be set"
    exit 1
fi
API_HOST="multicluster-controlplane-${HUB_NAME}.${API_HOST_POSTFIX}"

# Stop right away if the build fails
set -e
source "${REPO_DIR}/hack/lib/init.sh"
source "${REPO_DIR}/hack/lib/yaml.sh"

# Shut down anyway if there's an error.
set +e

CERTS_DIR="${REPO_DIR}/hack/deploy/controlplane/certs"
IN_POD_CERTS_DIR="/controlplane_config"

function start_apiserver {
    cp ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml  ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml.tmp
    cp ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml  ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml.tmp
    cp ${REPO_DIR}/hack/deploy/controlplane/deployment.yaml ${REPO_DIR}/hack/deploy/controlplane/deployment.yaml.tmp

    # copy root-ca to ca directory
    if [[ ! -z "${apiserver_caFile}" && ! -z "${apiserver_caKeyFile}" ]]; then 
        mkdir -p ${CERTS_DIR}
        cp -f ${REPO_DIR}/${apiserver_caFile} ${CERTS_DIR}/root-ca.crt
        cp -f ${REPO_DIR}/${apiserver_caKeyFile} ${CERTS_DIR}/root-ca.key
        # add to kustomize
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/root-ca.crt" ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/root-ca.key" ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml
        # modify config file
        sed -i "s,${apiserver_caFile},${IN_POD_CERTS_DIR}/root-ca.crt," ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
        sed -i "s,${apiserver_caKeyFile},${IN_POD_CERTS_DIR}/root-ca.key," ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
    fi

    # set etcd mode
    if [[ "${etcd_mode}" == "embed" ]]; then 
        echo "using embed etcd..."
    elif [[ "${etcd_mode}" == "external" ]]; then
        echo "using external etcd..."
        if [[ -z ${ETCD_NS+x} ]]; then
            echo "environment variable ETCD_NS should be set"
            exit 1
        fi

        if [[ -z "${etcd_servers:+x}" ]]; then
            # remove previous etcd server values
            sed -i '/servers/d' ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
            sed -i '/  - /d' ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
            # set etcd servers
            sed -i "$(sed -n  '/etcd:/=' ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml) a \  servers: " ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
            CLUSTER_SIZE=$(${KUBECTL} -n ${ETCD_NS} get statefulset.apps/etcd -o jsonpath='{.spec.replicas}')
            for((i=0;i<$CLUSTER_SIZE;i++))
            do
                ETCD_SERVER="http://etcd-"$i".etcd.${ETCD_NS}:2379"
                sed -i "$(sed -n  '/servers:/=' ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml) a \  - ${ETCD_SERVER}" ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
            done
        fi

        if [[ -z "${etcd_prefix:+x}" ]] ; then 
            sed -i '/prefix/d' ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
            # set etcd prefix
            sed -i "$(sed -n  '/etcd:/=' ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml) a \  prefix: \"${HUB_NAME}\"" ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
        fi

        mkdir -p ${CERTS_DIR}
        cp -f ${REPO_DIR}/${etcd_caFile} ${CERTS_DIR}/etcd-ca.crt
        cp -f ${REPO_DIR}/${etcd_certFile} ${CERTS_DIR}/etcd-client.crt
        cp -f ${REPO_DIR}/${etcd_keyFile} ${CERTS_DIR}/etcd-client.key
        # add to kustomize
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-client.key" ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-client.crt" ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-ca.crt" ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml
        # modify config file
        sed -i "s,${etcd_caFile},${IN_POD_CERTS_DIR}/etcd-ca.crt," ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml 
        sed -i "s,${etcd_certFile},${IN_POD_CERTS_DIR}/etcd-client.crt," ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
        sed -i "s,${etcd_keyFile},${IN_POD_CERTS_DIR}/etcd-client.key," ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
    else 
        echo "invalid value etcd mode"
        exit 1
    fi 

    cd ${REPO_DIR}/hack/deploy/controlplane && ${KUSTOMIZE} edit set namespace ${HUB_NAME}
    cd ${REPO_DIR}/hack/deploy/controlplane && ${KUSTOMIZE} edit set image quay.io/open-cluster-management/multicluster-controlplane=${IMAGE_NAME}
    
    cd ${REPO_DIR}
    echo "$(cat hack/deploy/controlplane/ocmconfig.yaml)"
    ${KUSTOMIZE} build ${REPO_DIR}/hack/deploy/controlplane | ${KUBECTL} apply -f -

    mv ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml.tmp ${REPO_DIR}/hack/deploy/controlplane/kustomization.yaml
    mv ${REPO_DIR}/hack/deploy/controlplane/deployment.yaml.tmp ${REPO_DIR}/hack/deploy/controlplane/deployment.yaml
    mv ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml.tmp  ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
}

function wait_for_kubeconfig_secret {
    echo "Waiting for kubeconfig..."
    while true; do
        ${KUBECTL} -n ${HUB_NAME} get secret multicluster-controlplane-kubeconfig &>/dev/null
        if [ $? -ne 0 ]; then
            continue
        else
            break
        fi
    done
    ${KUBECTL} -n ${HUB_NAME} get secret multicluster-controlplane-kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > ${REPO_DIR}/${HUB_NAME}.kubeconfig 
}

function check_multicluster-controlplane {
    for i in {1..30}; do
        echo "Checking multicluster-controlplane with ${REPO_DIR}/${HUB_NAME}.kubeconfig ..."
        result=$(${KUBECTL} --kubeconfig=${REPO_DIR}/${HUB_NAME}.kubeconfig api-resources | grep managedclusters)
        if [ -n "${result}" ]; then
            echo "#### multicluster-controlplane ${HUB_NAME} is ready ####"
            break
        fi
        
        if [ $i -eq 30 ]; then
            echo "The multicluster-controlplane ${HUB_NAME} is not ready within 300s"
            ${KUBECTL} -n ${HUB_NAME} get pods
            exit 1
        fi
        sleep 10
    done
}

###############################################################################
configfile=${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml
if [ ! -f "$configfile" ] ; then 
    echo "config file $configfile is not found, use defaul configurations" 
    cat > ${REPO_DIR}/hack/deploy/controlplane/ocmconfig.yaml <<EOF
dataDirectory: /.ocm
apiserver:
  externalHostname: ${API_HOST}
  port: 9443
etcd:
  mode: embed
  prefix: $HUB_NAME
EOF
fi

create_variables $configfile

if [[ -z "${apiserver_externalHostname:+x}" ]]; then
    echo "externalHostname is required"
    exit 1
fi

if [[ -z "${apiserver_caFile:+x}" || -z "${apiserver_caKeyFile:+x}" ]]; then 
    echo "caFile, caKeyFile not set, using self-generated root-ca..."
    apiserver_caFile=""
    apiserver_caKeyFile="" 
fi

if [[ -z "${etcd_mode:+x}" || "${etcd_mode}" == "external" ]]; then
    if [[ -z "${etcd_caFile:+x}" || -z "${etcd_certFile:+x}" || -z "${etcd_keyFile:+x}" ]]; then 
        echo "etcd_caFile, etcd_certFile, etcd_keyFile should not be set to empty while using external etcd"
        exit 1
    fi
fi

start_apiserver
wait_for_kubeconfig_secret
check_multicluster-controlplane
echo "#### Use '${KUBECTL} --kubeconfig=${REPO_DIR}/${HUB_NAME}.kubeconfig' to access the aggregated API server. ####"
echo ""
