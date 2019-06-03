package machinehealthcheck

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/golang/glog"
	e2e "github.com/openshift/cluster-api-actuator-pkg/pkg/e2e/framework"
	mapiv1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("[Feature:MachineDisruptionBudget] MachineDisruptionBudget controller", func() {
	var client runtimeclient.Client
	var workerNode *corev1.Node
	var workerMachineSet *mapiv1beta1.MachineSet
	var testMdb *healthcheckingv1alpha1.MachineDisruptionBudget

	mdbName := "test-mdb"
	getCurrentHealthyMachines := func() int32 {
		updateMdb := &healthcheckingv1alpha1.MachineDisruptionBudget{}
		key := types.NamespacedName{
			Name:      mdbName,
			Namespace: e2e.TestContext.MachineApiNamespace,
		}
		err := client.Get(context.TODO(), key, updateMdb)
		if err != nil {
			return 0
		}
		return updateMdb.Status.CurrentHealthy
	}

	BeforeEach(func() {
		var err error
		client, err = e2e.LoadClient()
		Expect(err).ToNot(HaveOccurred())

		isKubemarkProvider, err := e2e.IsKubemarkProvider(client)
		Expect(err).ToNot(HaveOccurred())
		// TODO: remove once we can create or update kubemark machines
		// that will give use possibility to make this test work
		if isKubemarkProvider {
			glog.V(2).Info("Can not run this tests with the 'KubeMark' provider")
			Skip("Can not run this tests with the 'KubeMark' provider")
		}

		err = e2e.CreateOrUpdateTechPreviewFeatureGate()
		Expect(err).ToNot(HaveOccurred())

		By("Getting worker node")
		workerNodes, err := e2e.GetWorkerNodes(client)
		Expect(err).ToNot(HaveOccurred())

		readyWorkerNodes := e2e.FilterReadyNodes(workerNodes)
		Expect(readyWorkerNodes).ToNot(BeEmpty())

		workerNode = &readyWorkerNodes[0]
		glog.V(2).Infof("Worker node %s", workerNode.Name)

		By("Getting worker machine")
		workerMachine, err := e2e.GetMachineFromNode(client, workerNode)
		Expect(err).ToNot(HaveOccurred())
		glog.V(2).Infof("Worker machine %s", workerMachine.Name)

		By("Geting worker machine set")
		workerMachineSet, err = e2e.GetMachineMachinesSet(workerMachine)
		Expect(err).ToNot(HaveOccurred())

		glog.V(2).Infof("Create machine health check with label selector: %s", workerMachine.Labels)
		err = e2e.CreateMachineHealthCheck(workerMachine.Labels)
		Expect(err).ToNot(HaveOccurred())

		unhealthyConditions := &conditions.UnhealthyConditions{
			Items: []conditions.UnhealthyCondition{
				{
					Name:    "Ready",
					Status:  "Unknown",
					Timeout: "60s",
				},
			},
		}
		glog.V(2).Infof("Create node-unhealthy-conditions configmap")
		err = e2e.CreateUnhealthyConditionsConfigMap(unhealthyConditions)
		Expect(err).ToNot(HaveOccurred())
	})

	It("updates MDB status", func() {
		minAvailable := int32(3)
		testMdb = e2e.NewMachineDisruptionBudget(
			mdbName,
			workerMachineSet.Spec.Selector.MatchLabels,
			&minAvailable,
			nil,
		)
		By("Creating MachineDisruptionBudget")
		err := client.Create(context.TODO(), testMdb)
		Expect(err).ToNot(HaveOccurred())

		updateMdb := &healthcheckingv1alpha1.MachineDisruptionBudget{}
		Eventually(func() int32 {
			key := types.NamespacedName{
				Name:      mdbName,
				Namespace: e2e.TestContext.MachineApiNamespace,
			}
			err := client.Get(context.TODO(), key, updateMdb)
			if err != nil {
				return 0
			}
			return updateMdb.Status.ExpectedMachines
		}, 120*time.Second, time.Second).Should(Equal(*workerMachineSet.Spec.Replicas))

		currentHealthy := updateMdb.Status.CurrentHealthy
		Expect(currentHealthy).To(Equal(workerMachineSet.Status.ReadyReplicas))

		By(fmt.Sprintf("Stopping kubelet service on the node %s", workerNode.Name))
		err = e2e.StopKubelet(workerNode.Name)
		Expect(err).ToNot(HaveOccurred())

		// Waiting until worker machine will have unhealthy node
		Eventually(getCurrentHealthyMachines, 6*time.Minute, 10*time.Second).Should(Equal(currentHealthy - 1))

		// Waiting until machine set will create new healthy machine
		Eventually(getCurrentHealthyMachines, 15*time.Minute, 30*time.Second).Should(Equal(currentHealthy))
	})

	AfterEach(func() {
		err := client.Delete(context.TODO(), testMdb)
		Expect(err).ToNot(HaveOccurred())

		err = e2e.DeleteMachineHealthCheck(e2e.MachineHealthCheckName)
		Expect(err).ToNot(HaveOccurred())

		err = e2e.DeleteKubeletKillerPods()
		Expect(err).ToNot(HaveOccurred())

		err = e2e.DeleteUnhealthyConditionsConfigMap()
		Expect(err).ToNot(HaveOccurred())
	})
})
