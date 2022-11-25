# Copyright Contributors to the Open Cluster Management project
#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/init.sh"

for f in $HUB_CRD_FILES
do
    cp $f ./config/crds/
done

