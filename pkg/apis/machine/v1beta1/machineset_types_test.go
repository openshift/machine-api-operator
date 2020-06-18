/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/apitesting/fuzzer"
	metafuzzer "k8s.io/apimachinery/pkg/apis/meta/fuzzer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestStorageMachineSet(t *testing.T) {
	key := types.NamespacedName{Name: "foo", Namespace: "default"}
	created := &MachineSet{ObjectMeta: metav1.ObjectMeta{Name: "foo", Namespace: "default"}}

	gs := NewWithT(t)

	mgr, err := manager.New(cfg, manager.Options{
		MetricsBindAddress: "0",
		Port:               testEnv.WebhookInstallOptions.LocalServingPort,
		CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
	})

	gs.Expect(err).ToNot(HaveOccurred())

	mgr.GetWebhookServer().Register("/validate-machine-openshift-io-v1beta1-machineset-cp-delete", &webhook.Admission{Handler: createMachineSetMockHandler(true)})
	mgr.GetWebhookServer().Register("/validate-machine-openshift-io-v1beta1-machineset-cp-update", &webhook.Admission{Handler: createMachineSetMockHandler(true)})

	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		gs.Expect(mgr.Start(done)).To(Succeed())
		close(stopped)
	}()
	defer func() {
		close(done)
		<-stopped
	}()

	// Test Create
	fetched := &MachineSet{}
	if err := c.Create(context.TODO(), created); err != nil {
		t.Errorf("error creating machineset: %v", err)
	}

	if err := c.Get(context.TODO(), key, fetched); err != nil {
		t.Errorf("error getting machineset: %v", err)
	}
	if !reflect.DeepEqual(*fetched, *created) {
		t.Error("fetched value not what was created")
	}

	// Test Updating the Labels
	updated := fetched.DeepCopy()
	updated.Labels = map[string]string{"hello": "world"}
	if err := c.Update(context.TODO(), updated); err != nil {
		t.Errorf("error updating machineset: %v", err)
	}

	if err := c.Get(context.TODO(), key, fetched); err != nil {
		t.Errorf("error getting machineset: %v", err)
	}
	if !reflect.DeepEqual(*fetched, *updated) {
		t.Error("fetched value not what was updated")
	}

	// Test Delete
	if err := c.Delete(context.TODO(), fetched); err != nil {
		t.Errorf("error deleting machineset: %v", err)
	}
	if err := c.Get(context.TODO(), key, fetched); err == nil {
		t.Error("expected error getting machineset")
	}
}

func TestDefaults(t *testing.T) {
	ms := &MachineSet{ObjectMeta: metav1.ObjectMeta{Name: "foo"}}
	ms.Default()

	expected := string(RandomMachineSetDeletePolicy)
	got := ms.Spec.DeletePolicy
	if got != expected {
		t.Errorf("expected default machineset delete policy '%s', got '%s'", expected, got)
	}
}

func TestRoundTripMachineSet(t *testing.T) {
	codecs := serializer.NewCodecFactory(scheme.Scheme)
	seed := time.Now().UnixNano()
	fuzzer := fuzzer.FuzzerFor(fuzzer.MergeFuzzerFuncs(metafuzzer.Funcs, machineFuzzerFuncs), rand.NewSource(seed), codecs)
	ctx := context.Background()
	g := NewWithT(t)

	for i := 0; i < 100; i++ {
		machineSet := &MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "machineset-round-trip-test-",
				Namespace:    "default",
			},
		}
		// Fuzz the spec and status as those are the ones we need to check aren't
		// losing data
		spec := &MachineSetSpec{}
		status := &MachineSetStatus{}
		fuzzer.Fuzz(spec)
		fuzzer.Fuzz(status)

		machineSet.Spec = *spec.DeepCopy()
		g.Expect(c.Create(ctx, machineSet)).To(Succeed())
		machineSet.Status = *status.DeepCopy()
		g.Expect(c.Status().Update(ctx, machineSet)).To(Succeed())

		// Check the spec and status weren't modified during create
		//
		// Use JSON representation as order of fields in RawExtensions may change
		// during a round trip
		machineSetSpecJSON, err := json.Marshal(machineSet.Spec)
		g.Expect(err).ToNot(HaveOccurred())
		specJSON, err := json.Marshal(*spec)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(machineSetSpecJSON).To(MatchJSON(specJSON))

		machineSetStatusJSON, err := json.Marshal(machineSet.Status)
		g.Expect(err).ToNot(HaveOccurred())
		statusJSON, err := json.Marshal(*status)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(machineSetStatusJSON).To(MatchJSON(statusJSON))

		fetched := &MachineSet{}
		key := client.ObjectKey{Namespace: machineSet.Namespace, Name: machineSet.Name}
		g.Expect(c.Get(ctx, key, fetched)).To(Succeed())

		// Check the spec and status haven't changed server side
		g.Expect(fetched.Spec).To(Equal(machineSet.Spec))
		g.Expect(fetched.Status).To(Equal(machineSet.Status))
	}
}
