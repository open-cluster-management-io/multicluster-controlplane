#!/bin/bash

set -o nounset
set -o pipefail

project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." ; pwd -P)"
kubeconfig_dir=$project_dir/test/resources/kubeconfig
options_file="${project_dir}/test/resources/kubeconfig/options.yaml"

hosting=${HOST_CLUSTER_NAME:-"hosting"}  # hosting cluster
cp1=controlplane1
cp2=controlplane2
cp1_mc1=$cp1-mc1
cp2_mc1=$cp2-mc1

printf "options:" > $options_file
printf "\n  hosting:" >> $options_file
printf "\n    name: $hosting" >> $options_file
printf "\n    kubeconfig: ${kubeconfig_dir}/${hosting}" >> $options_file
printf "\n  controlplanes:" >> $options_file
printf "\n    - name: $cp1" >> $options_file
printf "\n      kubeconfig: ${kubeconfig_dir}/${cp1}" >> $options_file
printf "\n      managedclusters:" >> $options_file
printf "\n        - name: $cp1_mc1" >> $options_file
printf "\n          kubeconfig: ${kubeconfig_dir}/${cp1_mc1}" >> $options_file
printf "\n    - name: $cp2" >> $options_file
printf "\n      kubeconfig: ${kubeconfig_dir}/${cp2}" >> $options_file
printf "\n      managedclusters:" >> $options_file
printf "\n        - name: $cp2_mc1" >> $options_file
printf "\n          kubeconfig: ${kubeconfig_dir}/${cp2_mc1}" >> $options_file
