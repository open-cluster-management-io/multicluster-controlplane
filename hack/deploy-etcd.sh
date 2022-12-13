#!/bin/bash

KUBE_ROOT=$(pwd)
KUBECTL=${KUBECTL:-"oc"}
KUSTOMIZE=${KUSTOMIZE:-"kustomize"}
if [ ! $KUBECTL >& /dev/null ] ; then
      echo "Failed to run $KUBECTL. Please ensure $KUBECTL is installed"
  exit 1
fi
if [ ! $KUSTOMIZE >& /dev/null ] ; then
      echo "Failed to run $KUSTOMIZE. Please ensure $KUSTOMIZE is installed"
  exit 1
fi

export ETCD_NS=${ETCD_NS:-"multicluster-controlplane-etcd"}

IMAGE_NAME=${IMAGE_NAME:-"quay.io/coreos/etcd"}
REUSE_CA=${REUSE_CA:-false}

if [[ "${REUSE_CA}" != true ]]; then
    if [ ! go >& /dev/null ] ; then
        echo "go found"
        exit 1
    fi
    if [ ! cfssl >& /dev/null ] ; then
        echo "cfssl not found, installing..."
        go install github.com/cloudflare/cfssl/cmd/cfssl@latest
    fi
    if [ ! cfssljson >& /dev/null ] ; then
        echo "cfssljson not found, installing..."
        go install github.com/cloudflare/cfssl/cmd/cfssljson@latest
    fi

    CFSSL_DIR=${KUBE_ROOT}/multicluster_ca
    mkdir -p ${CFSSL_DIR}
    cd ${CFSSL_DIR}

    echo '{"CN":"multicluster-controlplane","key":{"algo":"rsa","size":2048}}' | cfssl gencert -initca - | cfssljson -bare ca -
    echo '{"signing":{"default":{"expiry":"43800h","usages":["signing","key encipherment","server auth","client auth"]}}}' > ca-config.json

    export ADDRESS=
    export NAME=client
    echo '{"CN":"'$NAME'","hosts":[""],"key":{"algo":"rsa","size":2048}}' | cfssl gencert -config=ca-config.json -ca=ca.pem -ca-key=ca-key.pem -hostname="$ADDRESS" - | cfssljson -bare $NAME

    cd ${KUBE_ROOT}
    # copy ca to etcd dir
    mkdir -p ${KUBE_ROOT}/hack/deploy/etcd/cert-etcd
    cp -f ${CFSSL_DIR}/ca.pem ${KUBE_ROOT}/hack/deploy/etcd/cert-etcd/ca.pem

    # copy cert to controlplane dir
    CONTROLPLANE_ETCD_CERT=${KUBE_ROOT}/hack/deploy/controlplane/cert-etcd
    mkdir -p ${CONTROLPLANE_ETCD_CERT}
    cp -f ${CFSSL_DIR}/ca.pem ${CONTROLPLANE_ETCD_CERT}/ca.pem
    cp -f ${CFSSL_DIR}/client.pem ${CONTROLPLANE_ETCD_CERT}/client.pem
    cp -f ${CFSSL_DIR}/client-key.pem ${CONTROLPLANE_ETCD_CERT}/client-key.pem
fi

cp hack/deploy/etcd/kustomization.yaml  hack/deploy/etcd/kustomization.yaml.tmp
cd hack/deploy/etcd && ${KUSTOMIZE} edit set namespace ${ETCD_NS} && ${KUSTOMIZE} edit set image quay.io/coreos/etcd=${IMAGE_NAME}
cd ../../../
${KUSTOMIZE} build ${KUBE_ROOT}/hack/deploy/etcd | ${KUBECTL} apply -f -
mv hack/deploy/etcd/kustomization.yaml.tmp hack/deploy/etcd/kustomization.yaml

echo "#### etcd deployed ####" 
echo ""
