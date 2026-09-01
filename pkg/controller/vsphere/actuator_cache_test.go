package vsphere

import (
	"sync"
	"testing"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
)

// TestGetOrCreateScopeReusesCachedScope verifies that getOrCreateScope returns the exact
// same *machineScope instance that was previously cached for a machine's UID (as would
// happen after Exists() populates the cache), updating its machine and Context references
// rather than constructing a brand new scope (which would incur vCenter API calls).
func TestGetOrCreateScopeReusesCachedScope(t *testing.T) {
	g := NewWithT(t)

	machineUID := apimachinerytypes.UID("test-uid")
	cachedScope := &machineScope{}

	a := &Actuator{}
	a.scopeCache.Store(machineUID, cachedScope)

	newMachine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			UID:  machineUID,
		},
	}

	ctx := t.Context()
	scope, err := a.getOrCreateScope(ctx, newMachine)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(scope).To(BeIdenticalTo(cachedScope))
	g.Expect(scope.machine).To(BeIdenticalTo(newMachine))
	g.Expect(scope.Context).To(Equal(ctx))
}

// TestGetOrCreateScopeCreatesNewScopeWhenNotCached verifies that, absent a cache entry,
// getOrCreateScope falls back to newMachineScope. A nil context is used to force a cheap,
// deterministic failure from newMachineScope without needing a real vCenter/K8s client.
func TestGetOrCreateScopeCreatesNewScopeWhenNotCached(t *testing.T) {
	g := NewWithT(t)

	a := &Actuator{}
	machine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			UID:  apimachinerytypes.UID("some-other-uid"),
		},
	}

	_, err := a.getOrCreateScope(nil, machine) //nolint:staticcheck
	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(ContainSubstring("machine scope require a context"))
}

// TestTaskIDCacheConcurrencySafety exercises the sync.Map-based TaskIDCache concurrently to
// guard against regressions reintroducing an unsynchronized map (see Change 4's concurrency
// requirements). Run with `go test -race` to catch data races.
func TestTaskIDCacheConcurrencySafety(t *testing.T) {
	a := &Actuator{TaskIDCache: &sync.Map{}}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "machine"
			a.TaskIDCache.Store(name, "task-id")
			a.TaskIDCache.Load(name)
			a.TaskIDCache.Delete(name)
		}(i)
	}
	wg.Wait()
}

// TestScopeCacheConcurrencySafety exercises the sync.Map-based scopeCache concurrently to
// guard against regressions reintroducing an unsynchronized map.
func TestScopeCacheConcurrencySafety(t *testing.T) {
	a := &Actuator{}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			uid := apimachinerytypes.UID("uid")
			a.scopeCache.Store(uid, &machineScope{})
			a.scopeCache.Load(uid)
			a.scopeCache.Delete(uid)
		}(i)
	}
	wg.Wait()
}
