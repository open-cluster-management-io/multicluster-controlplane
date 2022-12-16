// Copyright Contributors to the Open Cluster Management project

package controllers

import aggregatorapiserver "k8s.io/kube-aggregator/pkg/apiserver"

type Controller func(stopCh <-chan struct{}, aggregatorConfig *aggregatorapiserver.Config) error
