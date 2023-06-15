# Copyright Contributors to the Open Cluster Management project
FROM golang:1.20 AS builder

ARG OS=linux
ARG ARCH=amd64
ENV DIRPATH /go/src/open-cluster-management.io/multicluster-controlplane
WORKDIR ${DIRPATH}

COPY . .

RUN apt-get update && apt-get install net-tools
RUN make vendor && make build


FROM registry.access.redhat.com/ubi8/ubi-minimal:latest
ENV USER_UID=10001

COPY --from=builder /go/src/open-cluster-management.io/multicluster-controlplane/bin/multicluster-controlplane /

USER ${USER_UID}
