# Copyright Contributors to the Open Cluster Management project
FROM golang:1.19 AS builder

ARG OS=linux
ARG ARCH=amd64
ENV DIRPATH /go/src/open-cluster-management.io/multicluster-controlplane
WORKDIR ${DIRPATH}

COPY . .

RUN apt-get update && apt-get install net-tools && make vendor 
RUN GOOS=${OS} \
    GOARCH=${ARCH} \
    make build


FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

RUN mkdir /var/run/kubernetes && chmod -R 777 /var/run/kubernetes

ENV USER_UID=10001

COPY --from=builder /go/src/open-cluster-management.io/multicluster-controlplane/bin/multicluster-controlplane /

USER ${USER_UID}
