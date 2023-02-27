#!/bin/bash

KUBE_ROOT=$(pwd)
KUBECTL=${KUBECTL:-"kubectl"}
KUSTOMIZE=${KUSTOMIZE:-"kustomize"}
CFSSL=${CFSSL:-"cfssl"}
CFSSLJSON=${CFSSLJSON:-"cfssljson"}

if ! command -v $KUBECTL >/dev/null 2>&1; then
  echo "Command $KUBECTL is not found"
  exit 1
fi

if ! command -v $KUSTOMIZE >/dev/null 2>&1; then
  echo "Command $KUSTOMIZE is not found"
  exit 1
fi

export ETCD_NS=${ETCD_NS:-"multicluster-controlplane-etcd"}

ETCD_IMAGE_NAME=${ETCD_IMAGE_NAME:-"quay.io/coreos/etcd"}
REUSE_CA=${REUSE_CA:-false}

if [[ "${REUSE_CA}" != true ]]; then
    if ! command -v go >/dev/null 2>&1; then
        echo "Command go is not found"
        exit 1
    fi

    if ! command -v $CFSSL >/dev/null 2>&1; then
        echo "Command $CFSSL is not found, installing..."
        go install github.com/cloudflare/cfssl/cmd/cfssl@latest
        CFSSL="$(go env GOPATH)/bin/cfssl"
    fi

    if ! command -v $CFSSLJSON >/dev/null 2>&1; then
        echo "Command $CFSSLJSON is not found, installing..."
        go install github.com/cloudflare/cfssl/cmd/cfssljson@latest
        CFSSLJSON="$(go env GOPATH)/bin/cfssljson"
    fi

    CFSSL_DIR=${KUBE_ROOT}/multicluster_ca
    mkdir -p ${CFSSL_DIR}
    cd ${CFSSL_DIR}

    echo '{"CN":"multicluster-controlplane","key":{"algo":"rsa","size":2048}}' | $CFSSL gencert -initca - | $CFSSLJSON -bare ca -
    echo '{"signing":{"default":{"expiry":"43800h","usages":["signing","key encipherment","server auth","client auth"]}}}' > ca-config.json

    export ADDRESS=
    export NAME=client
    echo '{"CN":"'$NAME'","hosts":[""],"key":{"algo":"rsa","size":2048}}' | $CFSSL gencert -config=ca-config.json -ca=ca.pem -ca-key=ca-key.pem -hostname="$ADDRESS" - | $CFSSLJSON -bare $NAME

    cd ${KUBE_ROOT}
    # copy ca to etcd dir
    mkdir -p ${KUBE_ROOT}/hack/deploy/etcd/cert-etcd
    cp -f ${CFSSL_DIR}/ca.pem ${KUBE_ROOT}/hack/deploy/etcd/cert-etcd/ca.pem
fi

cp ${KUBE_ROOT}/hack/deploy/etcd/kustomization.yaml  ${KUBE_ROOT}/hack/deploy/etcd/kustomization.yaml.tmp
cd ${KUBE_ROOT}/hack/deploy/etcd && ${KUSTOMIZE} edit set namespace ${ETCD_NS} && ${KUSTOMIZE} edit set image quay.io/coreos/etcd=${ETCD_IMAGE_NAME}
cd ../../../
${KUSTOMIZE} build ${KUBE_ROOT}/hack/deploy/etcd | ${KUBECTL} apply -f -
mv ${KUBE_ROOT}/hack/deploy/etcd/kustomization.yaml.tmp ${KUBE_ROOT}/hack/deploy/etcd/kustomization.yaml

function check_multicluster-etcd {
    for i in {1..50}; do
        echo "Checking multicluster-etcd..."
        RESULT=$(${KUBECTL} -n ${ETCD_NS} exec etcd-0 -- etcdctl cluster-health | tail -n1)
        if [[ "${RESULT}" = "cluster is healthy" ]]; then
            echo "#### multicluster-etcd ${ETCD_NS} is ready ####"
            break
        fi
        
        if [ $i -eq 90 ]; then
            echo "!!!!!!!!!!  the multicluster-etcd ${ETCD_NS} is not ready within 180s"
            ${KUBECTL} -n ${ETCD_NS} get pods
            
            exit 1
        fi
        sleep 2
    done
}
check_multicluster-etcd

echo "#### etcd deployed ####" 
echo ""
