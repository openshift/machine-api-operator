package main

import (
	"strings"
	"time"

	"context"
	"errors"
	"fmt"

	"github.com/golang/glog"
	osconfigv1 "github.com/openshift/api/config/v1"
	caov1alpha1 "github.com/openshift/cluster-autoscaler-operator/pkg/apis/autoscaling/v1alpha1"
	cvoresourcemerge "github.com/openshift/cluster-version-operator/lib/resourcemerge"
	kappsapi "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/wait"
	awsprovider "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsproviderconfig/v1alpha1"
	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/controller/noderefutil"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	waitShort  = 1 * time.Minute
	waitMedium = 3 * time.Minute
	waitLong   = 10 * time.Minute
)

func (tc *testConfig) ExpectOperatorAvailable() error {
	name := "machine-api-operator"
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	d := &kappsapi.Deployment{}

	err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Get(context.TODO(), key, d); err != nil {
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

func (tc *testConfig) ExpectOneClusterObject() error {
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	clusterList := capiv1alpha1.ClusterList{}

	err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &clusterList); err != nil {
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

func (tc *testConfig) ExpectClusterOperatorStatusAvailable() error {
	name := "machine-api-operator"
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}
	clusterOperator := &osconfigv1.ClusterOperator{}

	err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Get(context.TODO(), key, clusterOperator); err != nil {
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

func (tc *testConfig) ExpectAllMachinesLinkedToANode() error {
	machineAnnotationKey := "cluster.k8s.io/machine"
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	machineList := capiv1alpha1.MachineList{}
	nodeList := corev1.NodeList{}

	err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &machineList); err != nil {
			glog.Errorf("error querying api for machineList object: %v, retrying...", err)
			return false, nil
		}
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		glog.Infof("Waiting for %d machines to become nodes", len(machineList.Items))
		return len(machineList.Items) == len(nodeList.Items), nil
	})
	if err != nil {
		return err
	}

	return wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		nodeNameToMachineAnnotation := make(map[string]string)
		for _, node := range nodeList.Items {
			nodeNameToMachineAnnotation[node.Name] = node.Annotations[machineAnnotationKey]
		}
		for _, machine := range machineList.Items {
			if machine.Status.NodeRef == nil {
				glog.Errorf("machine %s has no NodeRef, retrying...", machine.Name)
				return false, nil
			}
			nodeName := machine.Status.NodeRef.Name
			if nodeNameToMachineAnnotation[nodeName] != fmt.Sprintf("%s/%s", namespace, machine.Name) {
				glog.Errorf("node name %s does not match expected machine name %s, retrying...", nodeName, machine.Name)
				return false, nil
			}
		}
		return true, nil
	})
}

func (tc *testConfig) ExpectReconcileControllersDeployment() error {
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      "clusterapi-manager-controllers",
	}
	d := &kappsapi.Deployment{}

	glog.Info("Get deployment")
	err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Get(context.TODO(), key, d); err != nil {
			glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Delete deployment")
	err = wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Delete(context.TODO(), d); err != nil {
			glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Verify deployment is recreated")
	err = wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		if err := tc.client.Get(context.TODO(), key, d); err != nil {
			glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
			return false, nil
		}
		if d.Status.ReadyReplicas < 1 || !d.DeletionTimestamp.IsZero() {
			return false, nil
		}
		return true, nil
	})
	return err
}

func (tc *testConfig) ExpectAdditiveReconcileMachineTaints() error {
	glog.Info("Verify machine taints are getting applied to node")
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	machineList := capiv1alpha1.MachineList{}

	if err := tc.client.List(context.TODO(), &listOptions, &machineList); err != nil {
		return fmt.Errorf("error querying api for machineList object: %v", err)

	}
	glog.Info("Got the machine list")
	machine := machineList.Items[0]
	if machine.Status.NodeRef == nil {
		return fmt.Errorf("machine %s has no NodeRef", machine.Name)
	}
	glog.Infof("Got the machine, %s", machine.Name)
	nodeName := machine.Status.NodeRef.Name
	nodeKey := types.NamespacedName{
		Namespace: namespace,
		Name:      nodeName,
	}
	node := &corev1.Node{}

	if err := tc.client.Get(context.TODO(), nodeKey, node); err != nil {
		return fmt.Errorf("error querying api for node object: %v", err)
	}
	glog.Infof("Got the node, %s, from machine, %s", node.Name, machine.Name)
	nodeTaint := corev1.Taint{
		Key:    "not-from-machine",
		Value:  "true",
		Effect: corev1.TaintEffectNoSchedule,
	}
	node.Spec.Taints = []corev1.Taint{nodeTaint}
	if err := tc.client.Update(context.TODO(), node); err != nil {
		return fmt.Errorf("error updating node object with non-machine taint: %v", err)
	}
	glog.Info("Updated node object with taint")
	machineTaint := corev1.Taint{
		Key:    "from-machine",
		Value:  "true",
		Effect: corev1.TaintEffectNoSchedule,
	}
	machine.Spec.Taints = []corev1.Taint{machineTaint}
	if err := tc.client.Update(context.TODO(), &machine); err != nil {
		return fmt.Errorf("error updating machine object with taint: %v", err)
	}
	glog.Info("Updated machine object with taint")
	var expectedTaints = sets.NewString("not-from-machine", "from-machine")
	err := wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		if err := tc.client.Get(context.TODO(), nodeKey, node); err != nil {
			glog.Errorf("error querying api for node object: %v", err)
			return false, nil
		}
		glog.Info("Got the node again for verification of taints")
		var observedTaints = sets.NewString()
		for _, taint := range node.Spec.Taints {
			observedTaints.Insert(taint.Key)
		}
		if expectedTaints.Difference(observedTaints).HasAny("not-from-machine", "from-machine") == false {
			glog.Infof("expected : %v, observed %v , difference %v, ", expectedTaints, observedTaints, expectedTaints.Difference(observedTaints))
			return true, nil
		}
		glog.Infof("All expected taints not found on node. Missing: %v", expectedTaints.Difference(observedTaints))
		return false, nil
	})
	return err
}

func (tc *testConfig) ExpectNewNodeWhenDeletingMachine() error {
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	machineList := capiv1alpha1.MachineList{}
	nodeList := corev1.NodeList{}

	glog.Info("Get machineList")
	err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &machineList); err != nil {
			glog.Errorf("error querying api for machineList object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Get nodeList")
	err = wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	clusterInitialTotalNodes := len(nodeList.Items)
	clusterInitialTotalMachines := len(machineList.Items)
	var triagedWorkerMachine capiv1alpha1.Machine
	var triagedWorkerNode corev1.Node
MachineLoop:
	for _, m := range machineList.Items {
		if m.Labels["sigs.k8s.io/cluster-api-machine-role"] == "worker" {
			for _, n := range nodeList.Items {
				if m.Status.NodeRef == nil {
					glog.Errorf("no NodeRef found in machine %v", m.Name)
					return errors.New("no NodeRef found in machine")
				}
				if n.Name == m.Status.NodeRef.Name {
					triagedWorkerMachine = m
					triagedWorkerNode = n
					break MachineLoop
				}
			}
		}
	}

	glog.Info("Delete machine")
	err = wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Delete(context.TODO(), &triagedWorkerMachine); err != nil {
			glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	err = wait.PollImmediate(1*time.Second, waitMedium, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &machineList); err != nil {
			glog.Errorf("error querying api for machineList object: %v, retrying...", err)
			return false, nil
		}
		glog.Info("Expect new machine to come up")
		return len(machineList.Items) == clusterInitialTotalMachines, nil
	})
	if err != nil {
		return err
	}

	err = wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		glog.Info("Expect deleted machine node to go away")
		for _, n := range nodeList.Items {
			if n.Name == triagedWorkerNode.Name {
				return false, nil
			}
		}
		glog.Info("Expect new node to come up")
		return len(nodeList.Items) == clusterInitialTotalNodes, nil
	})
	if err != nil {
		return err
	}
	return nil
}

// ExpectAutoscalerScalesOut is an smoke test for the autoscaling feature
// Create a clusterAutoscaler object
// Create a machineAutoscaler object
// Create a workLoad to force autoscaling
// Validate the targeted machineSet scales out the field for the expected number of replicas
// Validate the number of nodes in the cluster is growing
// Delete the workLoad
// Delete the autoscaler object
// Ensure initial number of replicas and nodes
func (tc *testConfig) ExpectAutoscalerScalesOut() error {
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	glog.Info("Get one machineSet")
	machineSetList := capiv1alpha1.MachineSetList{}
	err := wait.PollImmediate(1*time.Second, waitMedium, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &machineSetList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		return len(machineSetList.Items) > 0, nil
	})
	if err != nil {
		return err
	}

	// When we add support for machineDeployments on the installer, cluster-autoscaler and cluster-autoscaler-operator
	// we need to test against deployments instead so we skip this test.
	targetMachineSet := machineSetList.Items[0]
	if ownerReferences := targetMachineSet.GetOwnerReferences(); len(ownerReferences) > 0 {
		glog.Infof("MachineSet %s is owned by a machineDeployment. Please run tests agains machineDeployment instead", targetMachineSet.Name)
		return nil
	}

	glog.Infof("Create ClusterAutoscaler and MachineAutoscaler objects. Targeting machineSet %s", targetMachineSet.Name)
	initialNumberOfReplicas := targetMachineSet.Spec.Replicas
	clusterAutoscaler := caov1alpha1.ClusterAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "default",
			Namespace: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterAutoscaler",
			APIVersion: "autoscaling.openshift.io/v1alpha1",
		},
	}
	machineAutoscaler := caov1alpha1.MachineAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("autoscale-%s", targetMachineSet.Name),
			Namespace:    namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineAutoscaler",
			APIVersion: "autoscaling.openshift.io/v1alpha1",
		},
		Spec: caov1alpha1.MachineAutoscalerSpec{
			MaxReplicas: 12,
			MinReplicas: 1,
			ScaleTargetRef: caov1alpha1.CrossVersionObjectReference{
				Name:       targetMachineSet.Name,
				Kind:       "MachineSet",
				APIVersion: "cluster.k8s.io/v1alpha1",
			},
		},
	}
	err = wait.PollImmediate(1*time.Second, waitMedium, func() (bool, error) {
		if err := tc.client.Create(context.TODO(), &clusterAutoscaler); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				glog.Errorf("error querying api for clusterAutoscaler object: %v, retrying...", err)
				return false, nil
			}
		}
		if err := tc.client.Create(context.TODO(), &machineAutoscaler); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				glog.Errorf("error querying api for machineAutoscaler object: %v, retrying...", err)
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Get nodeList")
	nodeList := corev1.NodeList{}
	err = wait.PollImmediate(1*time.Second, waitMedium, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}
	clusterInitialTotalNodes := len(nodeList.Items)
	glog.Infof("Cluster initial number of nodes is %d", clusterInitialTotalNodes)

	glog.Info("Create workload")
	mem, err := resource.ParseQuantity("500Mi")
	if err != nil {
		glog.Fatal("failed to ParseQuantity %v", err)
	}
	cpu, err := resource.ParseQuantity("500m")
	if err != nil {
		glog.Fatal("failed to ParseQuantity %v", err)
	}
	backoffLimit := int32(4)
	completions := int32(50)
	parallelism := int32(50)
	activeDeadlineSeconds := int64(100)
	workLoad := batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workload",
			Namespace: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "workload",
							Image: "busybox",
							Command: []string{
								"sleep",
								"300",
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									"memory": mem,
									"cpu":    cpu,
								},
							},
						},
					},
					RestartPolicy: corev1.RestartPolicy("Never"),
				},
			},
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			BackoffLimit:          &backoffLimit,
			Completions:           &completions,
			Parallelism:           &parallelism,
		},
	}
	err = wait.PollImmediate(1*time.Second, waitMedium, func() (bool, error) {
		if err := tc.client.Create(context.TODO(), &workLoad); err != nil {
			glog.Errorf("error querying api for workLoad object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Wait for cluster to scale out number of replicas")
	err = wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		msKey := types.NamespacedName{
			Namespace: namespace,
			Name:      targetMachineSet.Name,
		}
		ms := &capiv1alpha1.MachineSet{}
		if err := tc.client.Get(context.TODO(), msKey, ms); err != nil {
			glog.Errorf("error querying api for clusterAutoscaler object: %v, retrying...", err)
			return false, nil
		}
		glog.Infof("MachineSet %s. Initial number of replicas: %d. New number of replicas: %d", targetMachineSet.Name, *initialNumberOfReplicas, *ms.Spec.Replicas)
		return *ms.Spec.Replicas > *initialNumberOfReplicas, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Wait for cluster to scale out nodes")
	err = wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		nodeList := corev1.NodeList{}
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		glog.Info("Expect at least a new node to come up")
		glog.Infof("Initial number of nodes: %d. New number of nodes: %d", clusterInitialTotalNodes, len(nodeList.Items))
		return len(nodeList.Items) > clusterInitialTotalNodes, nil
	})

	glog.Info("Delete workload")
	err = wait.PollImmediate(1*time.Second, waitMedium, func() (bool, error) {
		if err := tc.client.Delete(context.TODO(), &workLoad); err != nil {
			glog.Errorf("error querying api for workLoad object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	// We delete the clusterAutoscaler and ensure the initial number of replicas to get the cluster to the initial number of nodes
	// TODO: validate the autoscaler to scale down
	glog.Info("Delete clusterAutoscaler object")
	err = wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Delete(context.TODO(), &clusterAutoscaler); err != nil {
			glog.Errorf("error querying api for clusterAutoscaler object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Delete machineAutoscaler object")
	err = wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Delete(context.TODO(), &machineAutoscaler); err != nil {
			glog.Errorf("error querying api for machineAutoscaler object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Infof("Ensure initial number of replicas: %d", initialNumberOfReplicas)
	err = wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		msKey := types.NamespacedName{
			Namespace: namespace,
			Name:      targetMachineSet.Name,
		}
		ms := &capiv1alpha1.MachineSet{}
		if err := tc.client.Get(context.TODO(), msKey, ms); err != nil {
			glog.Errorf("error querying api for machineSet object: %v, retrying...", err)
			return false, nil
		}
		ms.Spec.Replicas = initialNumberOfReplicas
		if err := tc.client.Update(context.TODO(), ms); err != nil {
			glog.Errorf("error querying api for machineSet object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		return err
	}

	glog.Info("Wait for cluster to match initial number of nodes")
	return wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		nodeList := corev1.NodeList{}
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		glog.Infof("Initial number of nodes: %d. Current number of nodes: %d", clusterInitialTotalNodes, len(nodeList.Items))
		return len(nodeList.Items) == clusterInitialTotalNodes, nil
	})
}

// Scale to 3 replicas
// Set maxUnavailable = 0 maxSurge = 1
// Trigger a rolling update by updating the machineType of a deployment
// Verify machines are rolled our one by one and each associated node goes available
// Verify expected final number of nodes
func (tc *testConfig) ExpectRollingDeploymentUpdate() error {
	listOptions := client.ListOptions{
		Namespace: namespace,
	}
	glog.Info("Get machineDeploymentList")
	machineDeploymentList := capiv1alpha1.MachineDeploymentList{}
	if err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &machineDeploymentList); err != nil {
			glog.Errorf("error querying api for machineList object: %v, retrying...", err)
			return false, nil
		}
		return len(machineDeploymentList.Items) > 0, nil
	}); err != nil {
		return err
	}

	glog.Info("Get initial nodeList")
	nodeList := corev1.NodeList{}
	var clusterInitialTotalNodes int
	if err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}

		readyNodes := 0
		clusterInitialTotalNodes = len(nodeList.Items)
		for _, node := range nodeList.Items {
			if noderefutil.IsNodeReady(&node) {
				readyNodes++
			}
		}
		glog.Infof("Initial number of nodes: %d. Ready nodes: %d", clusterInitialTotalNodes, readyNodes)
		return readyNodes == clusterInitialTotalNodes, nil
	}); err != nil {
		return err
	}

	replicas := int32(3)
	dep := machineDeploymentList.Items[0]
	glog.Infof("Scale deployment %s to %d replicas", dep.Name, replicas)
	initialNumberOfReplicas := int(*dep.Spec.Replicas)
	dep.Spec.Replicas = &replicas
	expectedNumberOfNodes := clusterInitialTotalNodes + int(replicas) - initialNumberOfReplicas
	expectedNumberOfMachines := expectedNumberOfNodes
	if err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Update(context.TODO(), &dep); err != nil {
			glog.Errorf("error querying api for deployment object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}

	glog.Infof("Wait for nodes to scale out")
	if err := wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		readyNodes := 0
		for _, node := range nodeList.Items {
			if noderefutil.IsNodeReady(&node) {
				readyNodes++
			}
		}
		glog.Infof("Expected number of nodes: %d. Ready nodes: %d", expectedNumberOfNodes, readyNodes)
		return readyNodes == expectedNumberOfNodes, nil

	}); err != nil {
		return err
	}

	glog.Info("Set new machine type to trigger rolling update")
	// Validations here assume deployment values for rolling machines one by one:
	maxSurge := intstr.FromInt(1)
	maxUnavailable := intstr.FromInt(0)
	dep.Spec.Strategy.RollingUpdate.MaxUnavailable = &maxUnavailable
	dep.Spec.Strategy.RollingUpdate.MaxSurge = &maxSurge

	codec, err := awsprovider.NewCodec()
	if err != nil {
		glog.Fatal(err)
	}
	var config awsprovider.AWSMachineProviderConfig
	if err := codec.DecodeProviderSpec(&capiv1alpha1.ProviderSpec{Value: dep.Spec.Template.Spec.ProviderSpec.Value}, &config); err != nil {
		return err
	}
	var newInstanceType string
	if config.InstanceType == "m4.xlarge" {
		newInstanceType = "m4.large"
	} else {
		newInstanceType = "m4.xlarge"
	}
	config.InstanceType = newInstanceType
	dep.Spec.Template.Spec.ProviderSpec = capiv1alpha1.ProviderSpec{
		Value: &runtime.RawExtension{Object: &config},
	}
	if err := wait.PollImmediate(1*time.Second, waitShort, func() (bool, error) {
		if err := tc.client.Update(context.TODO(), &dep); err != nil {
			glog.Errorf("error querying api for deployment object: %v, retrying...", err)
			return false, nil
		}
		return true, nil
	}); err != nil {
		return err
	}
	glog.Infof("New InstanceType for deployment %s is %s", dep.Name, config.InstanceType)

	// Verify a new node per replica is rolled out
	for i := 0; i < int(replicas); i++ {
		glog.Infof("%d nodes remaining to finish rolling update", int(replicas)-i)
		glog.Info("Verify a new node is rolled out")
		glog.Info("Get new machine")
		var newMachine capiv1alpha1.Machine
		if err := wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
			glog.Infof("Wait for a new machine to be created")
			machineList := capiv1alpha1.MachineList{}
			if err := tc.client.List(context.TODO(), &listOptions, &machineList); err != nil {
				glog.Errorf("error querying api for machineList object: %v, retrying...", err)
				return false, nil
			}
			glog.Infof("Initial number of machines: %d. current number of machines: %d", expectedNumberOfMachines, len(machineList.Items))
			if expectedNumberOfMachines+1 != len(machineList.Items) {
				return false, nil
			}
			sortedMachineList := machineList.Items
			sortByNewestCreationTimestamp(sortedMachineList)
			newMachine = sortedMachineList[0]

			var config awsprovider.AWSMachineProviderConfig
			if err := codec.DecodeProviderSpec(&capiv1alpha1.ProviderSpec{Value: newMachine.Spec.ProviderSpec.Value}, &config); err != nil {
				return false, err
			}
			if config.InstanceType != newInstanceType {
				return false, nil
			}
			glog.Infof("New machine comes with the expected new instance type: %s", newInstanceType)

			return true, nil
		}); err != nil {
			return err
		}
		glog.Info("Get new node")
		if err := wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
			glog.Infof("Wait for machine %s to have a nodeRef", newMachine.Name)
			refreshedNewMachine := &capiv1alpha1.Machine{}
			key := types.NamespacedName{
				Namespace: namespace,
				Name:      newMachine.Name,
			}
			if err := tc.client.Get(context.TODO(), key, refreshedNewMachine); err != nil {
				glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
				return false, nil
			}
			if refreshedNewMachine.Status.NodeRef == nil {
				return false, nil
			}

			newNodeRef := refreshedNewMachine.Status.NodeRef
			newNode := &corev1.Node{}
			key = types.NamespacedName{
				Namespace: namespace,
				Name:      newNodeRef.Name,
			}
			if err := tc.client.Get(context.TODO(), key, newNode); err != nil {
				glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
				return false, nil
			}
			glog.Infof("Wait for new node %s to become available", newNode.Name)
			return noderefutil.IsNodeReady(newNode), nil
		}); err != nil {
			return err
		}
	}

	glog.Info("Verify the old version of the rolled nodes are deleted. Exactly initial number of nodes are available")
	return wait.PollImmediate(1*time.Second, waitLong, func() (bool, error) {
		if err := tc.client.List(context.TODO(), &listOptions, &nodeList); err != nil {
			glog.Errorf("error querying api for nodeList object: %v, retrying...", err)
			return false, nil
		}
		readyNodes := 0
		for _, node := range nodeList.Items {
			if noderefutil.IsNodeReady(&node) {
				readyNodes++
			}
		}
		glog.Infof("Initial number of nodes: %d. Ready nodes: %d", clusterInitialTotalNodes, readyNodes)
		return readyNodes == expectedNumberOfNodes, nil
	})
}
