#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"

managed_cluster="integration-test"

rm -rf ${REPO_DIR}/_output
kind delete clusters $managed_cluster
