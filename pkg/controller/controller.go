package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager, fnList []func(manager.Manager) error) error {
	for _, f := range fnList {
		if err := f(m); err != nil {
			return err
		}
	}
	return nil
}
