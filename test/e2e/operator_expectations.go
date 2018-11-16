package main

import (
	"time"

	"context"
	kappsapi "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

func ExpectOperatorAvailable() error {
	name := "machine-api-operator"
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		d := &kappsapi.Deployment{}
		if err := F.Client.Get(context.TODO(), key, d); err != nil {
			return false, nil
		}
		if d.Status.ReadyReplicas < 1 {
			return false, nil
		}
		return true, nil
	})
	return err
}
