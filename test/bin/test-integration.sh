#!/bin/bash

set -e

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
kubeconfig_dir=$project_dir/test/resources/integration/kubeconfig
options_file="${project_dir}/test/resources/integration/options.yaml"

cp="integration-cp"
mc="integration-mc"

printf "options:" > $options_file
printf "\n  controlplanes:" >> $options_file
printf "\n    - name: $cp" >> $options_file
printf "\n      context: multicluster-controlplane" >> $options_file
printf "\n      kubeconfig: ${kubeconfig_dir}/${cp}" >> $options_file
printf "\n      managedclusters:" >> $options_file
printf "\n        - name: $mc" >> $options_file
printf "\n          context: kind-$mc" >> $options_file
printf "\n          kubeconfig: ${kubeconfig_dir}/${mc}" >> $options_file

while getopts ":f:v:" opt; do
  case $opt in
    f) filter="$OPTARG"
    ;;
    v) verbose="$OPTARG"
    ;;
    \?) echo "Invalid option -$OPTARG" >&2
    exit 1
    ;;
  esac

  case $OPTARG in
    -*) echo "Option $opt needs a valid argument"
    exit 1
    ;;
  esac
done

echo "Build Test ..."
go test -c ${project_dir}/test/e2e -mod=vendor -o ${project_dir}/test/e2e/e2e.test

echo "Run Test ..."
cd ${project_dir}/test/e2e

if [ -z "${filter}" ]; then
  ./e2e.test --options=$options_file -v=$verbose
else
  ./e2e.test --ginkgo.label-filter=${filter} --ginkgo.v --options=$options_file -v=$verbose 
fi

rm ${project_dir}/test/e2e/e2e.test