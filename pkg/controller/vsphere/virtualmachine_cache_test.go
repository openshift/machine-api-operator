package vsphere

import (
	"testing"

	. "github.com/onsi/gomega"
	"github.com/vmware/govmomi/vim25/types"
)

// TestVirtualMachineGetUUIDUsesCache verifies that getUUID() returns the cached UUID without
// touching vm.Obj. A nil vm.Obj is used deliberately: if the cache were bypassed, calling
// vm.Obj.UUID() would panic, proving the cached value was returned instead.
func TestVirtualMachineGetUUIDUsesCache(t *testing.T) {
	g := NewWithT(t)

	vm := &virtualMachine{cachedUUID: "cached-uuid-1234"}

	g.Expect(vm.getUUID()).To(Equal("cached-uuid-1234"))
}

// TestVirtualMachineGetPowerStateUsesCache verifies that getPowerState() returns the cached
// power state without touching vm.Obj. A nil vm.Obj is used deliberately: if the cache were
// bypassed, calling vm.Obj.PowerState() would panic, proving the cached value was returned.
func TestVirtualMachineGetPowerStateUsesCache(t *testing.T) {
	g := NewWithT(t)

	cached := types.VirtualMachinePowerStatePoweredOn
	vm := &virtualMachine{cachedPowerState: &cached}

	state, err := vm.getPowerState()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(state).To(Equal(types.VirtualMachinePowerStatePoweredOn))
}

// TestReconcilerGetVMRefUsesCachedRef verifies that getVMRef() returns the VM reference
// cached by a preceding exists() call without performing a vCenter lookup. A machineScope
// with a nil session is used deliberately: if the cache were bypassed, findVM() would panic
// on the nil session, proving the cached value was returned instead.
func TestReconcilerGetVMRefUsesCachedRef(t *testing.T) {
	g := NewWithT(t)

	ref := types.ManagedObjectReference{Type: "VirtualMachine", Value: "vm-123"}
	r := &Reconciler{machineScope: &machineScope{cachedVMRef: &ref}}

	got, err := r.getVMRef()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(got).To(Equal(ref))
}
