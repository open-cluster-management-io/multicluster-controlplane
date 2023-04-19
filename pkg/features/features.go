// Copyright Contributors to the Open Cluster Management project

package features

import (
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/component-base/featuregate"

	ocmfeature "open-cluster-management.io/api/feature"
)

var (
	// DefaultControlplaneMutableFeatureGate is made up of multiple mutable feature-gates for controlplane.
	DefaultControlplaneMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()

	// DefaultAgentMutableFeatureGate made up of multiple mutable feature-gates for controlplane agent.
	DefaultAgentMutableFeatureGate featuregate.MutableFeatureGate = featuregate.NewFeatureGate()
)

func init() {
	utilruntime.Must(DefaultControlplaneMutableFeatureGate.Add(ocmfeature.DefaultHubRegistrationFeatureGates))

	utilruntime.Must(DefaultAgentMutableFeatureGate.Add(ocmfeature.DefaultSpokeRegistrationFeatureGates))
	utilruntime.Must(DefaultAgentMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates))
}
