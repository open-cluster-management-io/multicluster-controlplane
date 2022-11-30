export CONTROLPLANE_NUMBER ?= 2
export CONTROLPLANE_IMAGE ?= quay.io/open-cluster-management/multicluster-controlplane

setup-controlplane:
	./bin/setup.sh
.PHONY: setup-controlplane

cleanup-controlplane:
	./bin/cleanup.sh
.PHONY: cleanup_controlplane

test-e2e:
	./bin/test-e2e.sh
.PHONY: test-e2e
