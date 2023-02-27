#!/usr/bin/env bash

REPO_DIR="$(cd "$(dirname ${BASH_SOURCE[0]})/../../.." ; pwd -P)"

cluster="e2e-test"

sudo -E rm -rf ${REPO_DIR}/_output
kind delete clusters $cluster
