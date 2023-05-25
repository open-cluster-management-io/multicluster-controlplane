#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/.." ; pwd -P)"

set -o nounset
set -o pipefail
set -o errexit

source ${REPO_DIR}/hack/lib/deps.sh

check_kubectl
check_kustomize
check_cfssl

SED=sed
if [ "$(uname)" = 'Darwin' ]; then
  # run `brew install gnu-${SED}` to install gsed
  SED=gsed
fi

ETCD_NS=${ETCD_NS:-"multicluster-controlplane-etcd"}
ETCD_IMAGE_NAME=${ETCD_IMAGE_NAME:-"quay.io/coreos/etcd"}
REUSE_CA=${REUSE_CA:-false}
STORAGE_CLASS_NAME=${STORAGE_CLASS_NAME:-""}

deploy_dir=${REPO_DIR}/_output/etcd/deploy
cert_dir=${deploy_dir}/cert-etcd

echo "Deploy etcd on the namespace ${ETCD_NS} in the cluster ${KUBECONFIG}"
mkdir -p ${cert_dir}
cp -r ${REPO_DIR}/hack/deploy/etcd/* $deploy_dir

kubectl delete ns ${ETCD_NS} --ignore-not-found
kubectl create ns ${ETCD_NS}

if [ "${REUSE_CA}" != true ]; then
    pushd $cert_dir
    echo '{"CN":"multicluster-controlplane","key":{"algo":"rsa","size":2048}}' | cfssl gencert -initca - | cfssljson -bare ca -
    echo '{"signing":{"default":{"expiry":"43800h","usages":["signing","key encipherment","server auth","client auth"]}}}' > ca-config.json
    echo '{"CN":"'client'","hosts":[""],"key":{"algo":"rsa","size":2048}}' | cfssl gencert -config=ca-config.json -ca=ca.pem -ca-key=ca-key.pem -hostname="" - | cfssljson -bare client
    popd
fi

if [ "$STORAGE_CLASS_NAME" != "gp2" ]; then
  ${SED} -i "s/gp2/${STORAGE_CLASS_NAME}/g" $deploy_dir/statefulset.yaml
fi

pushd $deploy_dir
kustomize edit set namespace ${ETCD_NS}
kustomize edit set image quay.io/coreos/etcd=${ETCD_IMAGE_NAME}
popd

kustomize build ${deploy_dir} | kubectl apply -f -

wait_seconds="60"; until [[ $((wait_seconds--)) -eq 0 ]] || eval "kubectl -n ${ETCD_NS} get pod etcd-0 &> /dev/null" ; do sleep 1; done
kubectl -n ${ETCD_NS} wait pod/etcd-0 --for condition=Ready --timeout=180s

wait_seconds="60"; until [[ $((wait_seconds--)) -eq 0 ]] || eval "kubectl -n ${ETCD_NS} get pod etcd-1 &> /dev/null" ; do sleep 1; done
kubectl -n ${ETCD_NS} wait pod/etcd-1 --for condition=Ready --timeout=180s

wait_seconds="60"; until [[ $((wait_seconds--)) -eq 0 ]] || eval "kubectl -n ${ETCD_NS} get pod etcd-2 &> /dev/null" ; do sleep 1; done
kubectl -n ${ETCD_NS} wait pod/etcd-2 --for condition=Ready --timeout=180s

echo "wait for etcd health (timeout=180s) ..."
wait_seconds="180"; until [[ $((wait_seconds--)) -eq 0 ]] || eval "kubectl -n ${ETCD_NS} exec etcd-0 -- etcdctl cluster-health &> /dev/null" ; do sleep 1; done

echo "etcd is health"
