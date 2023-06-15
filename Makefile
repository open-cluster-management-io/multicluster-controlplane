# Copyright Contributors to the Open Cluster Management project
BINARYDIR := bin

KUBECTL?=kubectl
KUSTOMIZE?=kustomize

HUB_NAME?=multicluster-controlplane

IMAGE_REGISTRY?=quay.io/open-cluster-management
IMAGE_TAG?=latest
export IMAGE_NAME?=$(IMAGE_REGISTRY)/multicluster-controlplane:$(IMAGE_TAG)

CONTROLPLANE_KUBECONFIG?=hack/deploy/cert-$(HUB_NAME)/kubeconfig

# build

check-copyright: 
	@hack/check/check-copyright.sh
.PHONY: check-copyright

check: check-copyright
.PHONY: check

verify-gocilint:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.53.2
	go vet ./...
	golangci-lint run --timeout=3m ./...
.PHONY: verify-gocilint

verify: verify-gocilint
.PHONY: verify

all: clean vendor build run
.PHONY: all

run: vendor build
	hack/start-multicluster-controlplane.sh
.PHONY: run

build-bin-release:
	$(rm -rf bin)
	$(mkdir -p bin)
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane ./cmd/server/main.go && tar -czf bin/multicluster_controlplane_darwin_amd64.tar.gz LICENSE -C bin/ multicluster-controlplane
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane ./cmd/server/main.go && tar -czf bin/multicluster_controlplane_darwin_arm64.tar.gz LICENSE -C bin/ multicluster-controlplane
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane ./cmd/server/main.go && tar -czf bin/multicluster_controlplane_linux_amd64.tar.gz LICENSE -C bin/ multicluster-controlplane
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane ./cmd/server/main.go && tar -czf bin/multicluster_controlplane_linux_arm64.tar.gz LICENSE -C bin/ multicluster-controlplane
	GOOS=linux GOARCH=ppc64le go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane ./cmd/server/main.go && tar -czf bin/multicluster_controlplane_linux_ppc64le.tar.gz LICENSE -C bin/ multicluster-controlplane
	GOOS=linux GOARCH=s390x go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane ./cmd/server/main.go && tar -czf bin/multicluster_controlplane_linux_s390x.tar.gz LICENSE -C bin/ multicluster-controlplane
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -gcflags=-trimpath=x/y -o bin/multicluster-controlplane.exe ./cmd/server/main.go && zip -q bin/multicluster_controlplane_windows_amd64.zip LICENSE -j bin/multicluster-controlplane.exe

build: vendor
	$(shell if [ ! -e $(BINARYDIR) ];then mkdir -p $(BINARYDIR); fi)
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/multicluster-controlplane cmd/server/main.go 
.PHONY: build

image:
	docker build -f Dockerfile -t $(IMAGE_NAME) .
.PHONY: image

push:
	docker push $(IMAGE_NAME)
.PHONY: push

clean:
	rm -rf bin .ocmconfig vendor
.PHONY: clean

vendor: 
	go mod tidy 
	go mod vendor
.PHONY: vendor

update:
	bash -x hack/crd-update/copy-crds.sh
.PHONY: update

# deploy

deploy-etcd:
	hack/deploy-etcd.sh
.PHONY: deploy-etcd

deploy:
	HUB_NAME=$(HUB_NAME) hack/deploy-multicluster-controlplane.sh
.PHONY: deploy

destroy:
	HUB_NAME=$(HUB_NAME) hack/deploy-multicluster-controlplane.sh uninstall
.PHONY: destroy

deploy-work-manager-addon:
	$(KUBECTL) apply -k hack/deploy/addon/work-manager/hub --kubeconfig=$(CONTROLPLANE_KUBECONFIG)
	cp hack/deploy/addon/work-manager/manager/kustomization.yaml hack/deploy/addon/work-manager/manager/kustomization.yaml.tmp
	cd hack/deploy/addon/work-manager/manager && $(KUSTOMIZE) edit set namespace $(HUB_NAME)
	$(KUSTOMIZE) build hack/deploy/addon/work-manager/manager | $(KUBECTL) apply -f -
	mv hack/deploy/addon/work-manager/manager/kustomization.yaml.tmp hack/deploy/addon/work-manager/manager/kustomization.yaml
.PHONY: deploy-work-manager-addon

deploy-managed-serviceaccount-addon:
	$(KUBECTL) apply -k hack/deploy/addon/managed-serviceaccount/hub --kubeconfig=$(CONTROLPLANE_KUBECONFIG)
	cp hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml.tmp
	cd hack/deploy/addon/managed-serviceaccount/manager && $(KUSTOMIZE) edit set namespace $(HUB_NAME)
	$(KUSTOMIZE) build hack/deploy/addon/managed-serviceaccount/manager | $(KUBECTL) apply -f -
	mv hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml.tmp hack/deploy/addon/managed-serviceaccount/manager/kustomization.yaml
.PHONY: deploy-managed-serviceaccount-addon

deploy-policy-addon:
	$(KUBECTL) apply -k hack/deploy/addon/policy/hub --kubeconfig=$(CONTROLPLANE_KUBECONFIG)
	cp hack/deploy/addon/policy/manager/kustomization.yaml hack/deploy/addon/policy/manager/kustomization.yaml.tmp
	cd hack/deploy/addon/policy/manager && $(KUSTOMIZE) edit set namespace $(HUB_NAME)
	$(KUSTOMIZE) build hack/deploy/addon/policy/manager | $(KUBECTL) apply -f -
	mv hack/deploy/addon/policy/manager/kustomization.yaml.tmp hack/deploy/addon/policy/manager/kustomization.yaml
.PHONY: deploy-policy-addon

deploy-all: deploy deploy-work-manager-addon deploy-managed-serviceaccount-addon deploy-policy-addon
.PHONY: deploy-all

# test

cleanup-e2e:
	./test/e2e/hack/cleanup.sh
.PHONY: cleanup-e2e

build-e2e-test:
	go test -c ./test/e2e -o _output/e2e.test
.PHONY: build-e2e-test

test-e2e: cleanup-e2e build-e2e-test
	./test/e2e/hack/e2e.sh
.PHONY: test-e2e

cleanup-integration:
	./test/integration/hack/cleanup.sh
.PHONY: cleanup-integration

test-integration: cleanup-integration build
	./test/integration/hack/integration.sh
.PHONY: test-integration

build-performance-test:
	go build -o bin/perftool test/performance/perftool.go
.PHONY: build-performance-test

export PERF_TEST_OUTPUT_SUFFIX ?= $(shell date '+%Y%m%dT%H%M%S')

test-performance: build-performance-test
	mkdir -p _output/performance
	./test/performance/hack/performance.sh >_output/performance/perf.$(PERF_TEST_OUTPUT_SUFFIX).output 2>&1 &
.PHONY: test-performance
