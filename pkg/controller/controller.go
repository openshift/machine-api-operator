package controller

import (
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, opts manager.Options, fnList ...func(manager.Manager, manager.Options) error) error {
	for _, f := range fnList {
		if err := f(m, opts); err != nil {
			return err
		}
	}
	return nil
}

// AddToManagerWithFeatureGate adds all Controllers to the Manager along with a feature gate accessor
func AddToManagerWithFeatureGate(m manager.Manager, opts manager.Options, featureGateAccessor featuregates.FeatureGateAccess, fnList ...func(manager.Manager, manager.Options, featuregates.FeatureGateAccess) error) error {
	for _, f := range fnList {
		if err := f(m, opts, featureGateAccessor); err != nil {
			return err
		}
	}
	return nil
}
