# Copyright Contributors to the Open Cluster Management project
#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $HUB_CRD_FILES
do
    cp $f ./pkg/controllers/bootstrap/crds/
done

for f in $SPOKE_CRD_FILES
do
    cp $f ./pkg/agent/crds/
done
