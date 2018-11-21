package main

import (
	"time"

	"context"
	"errors"
	"fmt"
	"github.com/golang/glog"
	osconfigv1 "github.com/openshift/api/config/v1"
	cvoresourcemerge "github.com/openshift/cluster-version-operator/lib/resourcemerge"
	kappsapi "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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
	d := &kappsapi.Deployment{}

	err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		if err := F.Client.Get(context.TODO(), key, d); err != nil {
			glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
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

func ExpectClusterOperatorStatusAvailable() error {
	name := "machine-api-operator"
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	clusterOperator := &osconfigv1.ClusterOperator{}

	err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		if err := F.Client.Get(context.TODO(), key, clusterOperator); err != nil {
			glog.Errorf("error querying api for OperatorStatus object: %v, retrying...", err)
			return false, nil
		}
		if available := cvoresourcemerge.FindOperatorStatusCondition(clusterOperator.Status.Conditions, osconfigv1.OperatorAvailable); available != nil {
			if available.Status == osconfigv1.ConditionTrue {
				return true, nil
			}
		}
		return false, nil
	})
	return err
}

func ExpectAllMachinesLinkedToANode() error {
	machineAnnotationKey := "machine"
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	machineList := capiv1alpha1.MachineList{}
	nodeList := corev1.NodeList{}

	err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		if err := F.Client.List(context.TODO(), &listOptions, &machineList); err != nil {
			glog.Errorf("error querying api for machineList object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	err = wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		if err := F.Client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	nodeNameToMachineAnnotation := make(map[string]string)
	for _, node := range nodeList.Items {
		nodeNameToMachineAnnotation[node.Name] = node.Annotations[machineAnnotationKey]
	}
	for _, machine := range machineList.Items {
		nodeName := machine.Status.NodeRef.Name
		if nodeNameToMachineAnnotation[nodeName] != fmt.Sprintf("%s/%s", namespace, machine.Name) {
			glog.Errorf("node name %s does not match expected machine name %s", nodeName, machine.Name)
			return errors.New("not all nodes are linked to a machine")
		}
	}
	return nil
}
