package operators

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	g "github.com/onsi/ginkgo"
	o "github.com/onsi/gomega"
	e2e "github.com/openshift/cluster-api-actuator-pkg/pkg/e2e/framework"
	kappsapi "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

var _ = g.Describe("[Feature:Operators] Machine API operator deployment should", func() {
	defer g.GinkgoRecover()

	g.It("be available", func() {
		var err error
		client, err := e2e.LoadClient()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(isDeploymentAvailable(client, "machine-api-operator")).To(o.BeTrue())
	})

	g.It("reconcile controllers deployment", func() {
		var err error
		client, err := e2e.LoadClient()
		o.Expect(err).NotTo(o.HaveOccurred())

		controllersDeploymentKey := types.NamespacedName{
			Namespace: e2e.TestContext.MachineApiNamespace,
			Name:      "clusterapi-manager-controllers",
		}
		initialDeployment := &kappsapi.Deployment{}
		glog.Infof("Get deployment %q", controllersDeploymentKey.Name)
		err = wait.PollImmediate(1*time.Second, e2e.WaitShort, func() (bool, error) {
			if err := client.Get(context.TODO(), controllersDeploymentKey, initialDeployment); err != nil {
				glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
				return false, nil
			}
			return true, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By(fmt.Sprintf("checking deployment %q is available", controllersDeploymentKey.Name))
		o.Expect(isDeploymentAvailable(client, controllersDeploymentKey.Name)).To(o.BeTrue())

		g.By(fmt.Sprintf("deleting deployment %q", controllersDeploymentKey.Name))
		err = wait.PollImmediate(1*time.Second, e2e.WaitShort, func() (bool, error) {
			if err := client.Delete(context.TODO(), initialDeployment); err != nil {
				glog.Errorf("error querying api for Deployment object: %v, retrying...", err)
				return false, nil
			}
			return true, nil
		})
		o.Expect(err).NotTo(o.HaveOccurred())

		g.By(fmt.Sprintf("checking deployment %q is available again", controllersDeploymentKey.Name))
		o.Expect(isDeploymentAvailable(client, controllersDeploymentKey.Name)).To(o.BeTrue())
	})

})

var _ = g.Describe("[Feature:Operators] Machine API cluster operator status should", func() {
	defer g.GinkgoRecover()

	g.It("be available", func() {
		var err error
		client, err := e2e.LoadClient()
		o.Expect(err).NotTo(o.HaveOccurred())
		o.Expect(isStatusAvailable(client, "machine-api")).To(o.BeTrue())
	})
})
