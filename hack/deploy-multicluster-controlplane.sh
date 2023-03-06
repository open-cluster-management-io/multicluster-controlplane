#!/usr/bin/env bash
# Copyright Contributors to the Open Cluster Management project

KUBECTL=oc
KUSTOMIZE=kustomize
if [ ! $KUBECTL >& /dev/null ] ; then
    echo "Failed to run $KUBECTL. Please ensure $KUBECTL is installed"
    exit 1
fi
if [ ! $KUSTOMIZE >& /dev/null ] ; then
    echo "Failed to run $KUSTOMIZE. Please ensure $KUSTOMIZE is installed"
    exit 1
fi

KUBE_ROOT=$(pwd)

configfile=${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
if [ ! -f "$configfile" ] ; then 
    echo "config file $configfile do not exist, create it first!" 
    exit 1
fi 

HUB_NAME=${HUB_NAME:-"multicluster-controlplane"}
IMAGE_NAME=${IMAGE_NAME:-"quay.io/open-cluster-management/multicluster-controlplane"}

# this is needed for the controlplane deploy
echo "* Testing connection"
HOST_URL=$(${KUBECTL} -n openshift-console get routes console -o jsonpath='{.status.ingress[0].routerCanonicalHostname}')
if [ $? -ne 0 ]; then
    echo "ERROR: Make sure you are logged into an OpenShift Container Platform before running this script"
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

source "${KUBE_ROOT}/hack/lib/init.sh"
kube::util::ensure-gnu-sed

source "${KUBE_ROOT}/hack/lib/yaml.sh"

# Shut down anyway if there's an error.
set +e

CERTS_DIR="${KUBE_ROOT}/hack/deploy/controlplane/certs"
IN_POD_CERTS_DIR="/controlplane_config"

function start_apiserver {
    cp ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml  ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml.tmp 

    if [[ -z "${apiserver_externalHostname:+x}" ]]; then 
        sed -i '/externalHostname/d' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
        sed -i "$(sed -n  '/apiserver/=' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml) a \  externalHostname: ${API_HOST}" ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
    fi

    cp ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml  ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml.tmp
    cp ${KUBE_ROOT}/hack/deploy/controlplane/deployment.yaml ${KUBE_ROOT}/hack/deploy/controlplane/deployment.yaml.tmp

    # copy root-ca to ca directory
    if [[ ! -z "${apiserver_caFile}" && ! -z "${apiserver_caKeyFile}" ]]; then 
        mkdir -p ${CERTS_DIR}
        cp -f ${KUBE_ROOT}/${apiserver_caFile} ${CERTS_DIR}/root-ca.crt
        cp -f ${KUBE_ROOT}/${apiserver_caKeyFile} ${CERTS_DIR}/root-ca.key
        # add to kustomize
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/root-ca.crt" ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/root-ca.key" ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml
        # modify config file
        sed -i "s,${apiserver_caFile},${IN_POD_CERTS_DIR}/root-ca.crt," ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
        sed -i "s,${apiserver_caKeyFile},${IN_POD_CERTS_DIR}/root-ca.key," ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
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
            sed -i '/servers/d' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
            sed -i '/  - /d' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
            # set etcd servers
            sed -i "$(sed -n  '/etcd:/=' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml) a \  servers: " ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
            CLUSTER_SIZE=$(${KUBECTL} -n ${ETCD_NS} get statefulset.apps/etcd -o jsonpath='{.spec.replicas}')
            for((i=0;i<$CLUSTER_SIZE;i++))
            do
                ETCD_SERVER="http://etcd-"$i".etcd.${ETCD_NS}:2379"
                sed -i "$(sed -n  '/servers:/=' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml) a \  - ${ETCD_SERVER}" ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
            done
        fi

        if [[ -z "${etcd_prefix:+x}" ]] ; then 
            sed -i '/prefix/d' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
            # set etcd prefix
            sed -i "$(sed -n  '/etcd:/=' ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml) a \  prefix: \"${HUB_NAME}\"" ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
        fi

        mkdir -p ${CERTS_DIR}
        cp -f ${KUBE_ROOT}/${etcd_caFile} ${CERTS_DIR}/etcd-ca.crt
        cp -f ${KUBE_ROOT}/${etcd_certFile} ${CERTS_DIR}/etcd-client.crt
        cp -f ${KUBE_ROOT}/${etcd_keyFile} ${CERTS_DIR}/etcd-client.key
        # add to kustomize
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-client.key" ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-client.crt" ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml
        sed -i "$(sed -n  '/  - ocmconfig.yaml/=' ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml) a \  - ${CERTS_DIR}/etcd-ca.crt" ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml
        # modify config file
        sed -i "s,${etcd_caFile},${IN_POD_CERTS_DIR}/etcd-ca.crt," ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml 
        sed -i "s,${etcd_certFile},${IN_POD_CERTS_DIR}/etcd-client.crt," ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
        sed -i "s,${etcd_keyFile},${IN_POD_CERTS_DIR}/etcd-client.key," ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
    else 
        echo "invalid value etcd mode"
        exit 1
    fi 

    cd ${KUBE_ROOT}/hack/deploy/controlplane && ${KUSTOMIZE} edit set namespace ${HUB_NAME} && ${KUSTOMIZE} edit set image quay.io/open-cluster-management/multicluster-controlplane=${IMAGE_NAME}
    cd ${KUBE_ROOT}
    echo "$(cat hack/deploy/controlplane/ocmconfig.yaml)"
    ${KUSTOMIZE} build ${KUBE_ROOT}/hack/deploy/controlplane | ${KUBECTL} apply -f -
    mv ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml.tmp ${KUBE_ROOT}/hack/deploy/controlplane/kustomization.yaml
    mv ${KUBE_ROOT}/hack/deploy/controlplane/deployment.yaml.tmp ${KUBE_ROOT}/hack/deploy/controlplane/deployment.yaml
    mv ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml.tmp  ${KUBE_ROOT}/hack/deploy/controlplane/ocmconfig.yaml
}

function wait_for_kubeconfig_secret {
    echo "Waiting for kubeconfig..."
    while true; do
        oc -n ${HUB_NAME} get secret kubeconfig &>/dev/null
        if [ $? -ne 0 ]; then
            continue
        else
            break
        fi
    done
    oc -n ${HUB_NAME} get secret kubeconfig -o jsonpath='{.data.kubeconfig}' | base64 -d > ${HUB_NAME}.kubeconfig 
}

function check_multicluster-controlplane {
    for i in {1..10}; do
        echo "Checking multicluster-controlplane..."
            RESULT=$(${KUBECTL} --kubeconfig=${HUB_NAME}.kubeconfig api-resources | grep managedclusters)
        if [ -n "${RESULT}" ]; then
            echo "#### multicluster-controlplane ${HUB_NAME} is ready ####"
            break
        fi
        
        if [ $i -eq 10 ]; then
            echo "!!!!!!!!!!  the multicluster-controlplane ${HUB_NAME} is not ready within 30s"
            ${KUBECTL} -n ${HUB_NAME} get pods
            
            exit 1
        fi
        sleep 2
    done
}

###############################################################################
create_variables $configfile

if [[ -z "${deployToOCP:+x}" || "${deployToOCP}" != "true" ]]; then
    echo "deployToOCP should be set to true"
    exit 1
fi

if [[ -z "${apiserver_externalHostname:+x}" ]]; then
    echo "externalHostname not set, using default OCP format..."
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
echo "#### Use '${KUBECTL} --kubeconfig=${HUB_NAME}.kubeconfig' to use the aggregated API server. ####"
echo ""
