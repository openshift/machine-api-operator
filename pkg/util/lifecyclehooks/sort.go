package lifecyclehooks

import (
	"reflect"

	machinev1 "github.com/openshift/api/machine/v1beta1"
)

func GetChangedLifecycleHooks(old, new []machinev1.LifecycleHook) []machinev1.LifecycleHook {
	oldSet := make(map[string]machinev1.LifecycleHook)
	for _, hook := range old {
		oldSet[hook.Name] = hook
	}

	changedHooks := []machinev1.LifecycleHook{}
	for _, hook := range new {
		if oldHook, ok := oldSet[hook.Name]; !ok || !reflect.DeepEqual(oldHook, hook) {
			changedHooks = append(changedHooks, hook)
		}
	}
	return changedHooks
}
