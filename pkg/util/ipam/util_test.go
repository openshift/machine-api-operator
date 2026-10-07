package ipam

import (
	"context"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ipamv1 "sigs.k8s.io/cluster-api/api/ipam/v1beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

func init() {
	if err := machinev1.Install(scheme.Scheme); err != nil {
		panic(err)
	}
	if err := ipamv1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
}

// TestEnsureIPAddressClaimWithEmptyPoolGroup verifies that a Machine API
// AddressesFromPool with an empty group (a valid, historically supported
// configuration) does not fail to create an IPAddressClaim against the
// v1beta2 CRD, which requires spec.poolRef.apiGroup to be non-empty.
func TestEnsureIPAddressClaimWithEmptyPoolGroup(t *testing.T) {
	g := NewWithT(t)

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "third_party", "cluster-api", "crd"),
		},
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	runtimeClient, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	g.Expect(err).ToNot(HaveOccurred())

	machine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-machine",
			Namespace: "default",
			UID:       types.UID("test-uid"),
		},
	}

	pool := machinev1.AddressesFromPool{
		Group:    "",
		Resource: "inclusteripaddresspools",
		Name:     "test-pool",
	}

	claim, err := EnsureIPAddressClaim(context.Background(), runtimeClient, "test-claim", machine, pool)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(claim.Spec.PoolRef.APIGroup).To(Equal(ipamv1.GroupVersion.Group))
}
