package controller

import (
	"k8s.io/component-base/featuregate"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManager adds all Controllers to the Manager along with a feature gate accessor
func AddToManagerWithFeatureGates(m manager.Manager, opts manager.Options, featureGate featuregate.MutableFeatureGate, fnList ...func(manager.Manager, manager.Options, featuregate.MutableFeatureGate) error) error {
	for _, f := range fnList {
		if err := f(m, opts, featureGate); err != nil {
			return err
		}
	}
	return nil
}

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, opts manager.Options, fnList ...func(manager.Manager, manager.Options) error) error {
	for _, f := range fnList {
		if err := f(m, opts); err != nil {
			return err
		}
	}
	return nil
}
