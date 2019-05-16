package operator

import (
	"fmt"

	osev1 "github.com/openshift/api/config/v1"
)

const (
	// MachineAPIFeatureGateName contains a name of the machine API FeatureGate object
	MachineAPIFeatureGateName = "cluster"

	// FeatureGateMachineHealthCheck contains the name of the MachineHealthCheck feature gate
	FeatureGateMachineHealthCheck = "MachineHealthCheck"
)

// MachineAPIOperatorFeatureSets contains a map of machine-api-operator features names to Enabled/Disabled feature.
//
// NOTE: The caller needs to make sure to check for the existence of the value
// using golang's existence field. A possible scenario is an upgrade where new
// FeatureSets are added and a controller has not been upgraded with a newer
// version of this file. In this upgrade scenario the map could return nil.
//
// example:
//   if featureSet, ok := MachineAPIOperatorFeatureSets["SomeNewFeature"]; ok { }
//
// If you put an item in either of these lists, put your area and name on it so we can find owners.
var MachineAPIOperatorFeatureSets = map[osev1.FeatureSet]*osev1.FeatureGateEnabledDisabled{
	osev1.Default: {
		Disabled: []string{
			FeatureGateMachineHealthCheck, // machine-api-operator, alukiano
		},
	},
	osev1.TechPreviewNoUpgrade: {
		Enabled: []string{
			FeatureGateMachineHealthCheck, // machine-api-operator, alukiano
		},
	},
}

func generateFeatureMap(featureSet osev1.FeatureSet) (map[string]bool, error) {
	rv := map[string]bool{}
	set, ok := MachineAPIOperatorFeatureSets[featureSet]
	if !ok {
		return nil, fmt.Errorf("enabled FeatureSet %v does not have a corresponding config", featureSet)
	}
	for _, featEnabled := range set.Enabled {
		rv[featEnabled] = true
	}
	for _, featDisabled := range set.Disabled {
		rv[featDisabled] = false
	}
	return rv, nil
}
