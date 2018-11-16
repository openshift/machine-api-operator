package main

import (
	"time"

	"context"
	"errors"
	"github.com/golang/glog"
	kappsapi "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func ExpectOneClusterObject() error {
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	clusterList := capiv1alpha1.ClusterList{}

	err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		if err := F.Client.List(context.TODO(), &listOptions, &clusterList); err != nil {
			glog.Errorf("error querying api for clusterList object: %v, retrying...", err)
			return false, nil
		}
		if len(clusterList.Items) != 1 {
			return false, errors.New("more than one cluster object found")
		}
		return true, nil
	})
	return err
}
