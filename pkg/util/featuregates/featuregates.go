package featuregates

import (
	v1 "github.com/openshift/api/config/v1"
	"k8s.io/klog/v2"
)

const (
	// DeployMHCControllerFeatureGateName is the name of the feature gate for enabling the MHC controller
	DeployMHCControllerFeatureGateName = "MachineAPIOperatorDeployMHCController"
)

// IsDeployMHCControllerEnabled returns if the feature gate for the MHC controller deployment is enabled.
// For now this is an experimental feature gate, and we only check if it's disabled via the CustomNoUpgrade feature set.
// The purpose is to disable the MHC controller for being able to test the upcoming MHC feature of the NodeHealthCheck operator.
// Whenever NHC becomes the default MHC handler, the default return value needs to be changed to false!
func IsDeployMHCControllerEnabled(fg *v1.FeatureGate) bool {
	deployMHCControllerFeatureGate := v1.FeatureGateName(DeployMHCControllerFeatureGateName)
	if fg != nil && fg.Spec.CustomNoUpgrade != nil {
		for _, enabled := range fg.Spec.CustomNoUpgrade.Enabled {
			if enabled == deployMHCControllerFeatureGate {
				klog.V(2).Info("MHC controller enabled by feature gate")
				return true
			}
		}
		for _, disabled := range fg.Spec.CustomNoUpgrade.Disabled {
			if disabled == deployMHCControllerFeatureGate {
				klog.V(2).Info("MHC controller disabled by feature gate")
				return false
			}
		}
	}
	// switch to false once NHC is the default!
	klog.V(4).Info("MHC controller enabled (default)")
	return true
}
