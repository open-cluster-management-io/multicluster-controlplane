// Copyright Contributors to the Open Cluster Management project

package features

import (
	"k8s.io/component-base/featuregate"
)

const (
	// ManagedServiceAccount will start new controllers in the controlplane agent process to synchronize ServiceAccount to the managed clusters
	// and collecting the tokens from these local service accounts as secret resources back to the hub cluster.
	ManagedServiceAccount featuregate.Feature = "ManagedServiceAccount"

	// ManagedServiceAccountEphemeralIdentity allow user to set TTL on the ManagedServiceAccount resource via spec.ttlSecondsAfterCreation
	ManagedServiceAccountEphemeralIdentity featuregate.Feature = "ManagedServiceAccountEphemeralIdentity"
)

var DefaultControlPlaneFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ManagedServiceAccountEphemeralIdentity: {Default: false, PreRelease: featuregate.Alpha},
}

var DefaultControlPlaneAgentFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	ManagedServiceAccount: {Default: false, PreRelease: featuregate.Alpha},
}
