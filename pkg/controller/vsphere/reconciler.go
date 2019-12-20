package vsphere

import (
	"context"
	"fmt"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	vsphereapi "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/pkg/errors"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"k8s.io/klog"
)

const (
	minMemMB     = 2048
	minCPU       = 2
	diskMoveType = string(types.VirtualMachineRelocateDiskMoveOptionsMoveAllDiskBackingsAndConsolidate)
)

// Reconciler runs the logic to reconciles a machine resource towards its desired state
type Reconciler struct {
	*machineScope
}

func newReconciler(scope *machineScope) *Reconciler {
	return &Reconciler{
		machineScope: scope,
	}
}

// create creates machine if it does not exists.
func (r *Reconciler) create() error {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return fmt.Errorf("%v: failed validating machine provider spec: %v", r.machine.GetName(), err)
	}

	moTask, err := r.session.GetTask(r.Context, r.providerStatus.TaskRef)
	if err != nil {
		if !isRetrieveMONotFound(r.providerStatus.TaskRef, err) {
			return err
		}
	}
	if taskIsFinished, err := taskIsFinished(moTask); err != nil || !taskIsFinished {
		if !taskIsFinished {
			return fmt.Errorf("task %v has not finished", moTask.Reference().Value)
		}
		return err
	}

	if _, err := findVM(r.machineScope); err != nil {
		if !IsNotFound(err) {
			return err
		}
		if r.machineScope.session.IsVC() {
			klog.Infof("%v: cloning", r.machine.GetName())
			return clone(r.machineScope)
		}
		return fmt.Errorf("%v: not connected to a vCenter", r.machine.GetName())
	}

	return nil
}

// update finds a vm and reconciles the machine resource status against it.
func (r *Reconciler) update() error {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return fmt.Errorf("%v: failed validating machine provider spec: %v", r.machine.GetName(), err)
	}

	taskRef, err := r.session.GetTask(r.Context, r.providerStatus.TaskRef)
	if err != nil {
		if !isRetrieveMONotFound(r.providerStatus.TaskRef, err) {
			return err
		}
	}
	if taskIsFinished, err := taskIsFinished(taskRef); err != nil || !taskIsFinished {
		if !taskIsFinished {
			return fmt.Errorf("task %v has not finished", taskRef.Value)
		}
		return err
	}

	vmRef, err := findVM(r.machineScope)
	if err != nil {
		if !IsNotFound(err) {
			return err
		}
		return errors.Wrap(err, "vm not found on update")
	}

	vm := &virtualMachine{
		Context: r.machineScope.Context,
		Obj:     object.NewVirtualMachine(r.machineScope.session.Client.Client, vmRef),
		Ref:     vmRef,
	}

	// TODO: we won't always want to reconcile power state
	//  but as per comment in clone() function, powering on right on creation might be problematic
	if ok, err := vm.reconcilePowerState(); err != nil || !ok {
		return err
	}
	return r.reconcileMachineWithCloudState(vm)
}

// exists returns true if machine exists.
func (r *Reconciler) exists() (bool, error) {
	if err := validateMachine(*r.machine, *r.providerSpec); err != nil {
		return false, fmt.Errorf("%v: failed validating machine provider spec: %v", r.machine.GetName(), err)
	}

	if _, err := findVM(r.machineScope); err != nil {
		if !IsNotFound(err) {
			return false, err
		}
		klog.Infof("%v: does not exist", r.machine.GetName())
		return false, nil
	}
	klog.Infof("%v: already exists", r.machine.GetName())
	return true, nil
}

func (r *Reconciler) delete() error {
	// TODO: implement
	return nil
}

// reconcileMachineWithCloudState reconcile machineSpec and status with the latest cloud state
func (r *Reconciler) reconcileMachineWithCloudState(vm *virtualMachine) error {
	klog.V(3).Infof("%v: reconciling machine with cloud state", r.machine.GetName())
	// TODO: reconcile providerID
	// TODO: reconcile task
	return r.reconcileNetwork(vm)
}

func (r *Reconciler) reconcileNetwork(vm *virtualMachine) error {
	// TODO: implement
	return nil
}

func validateMachine(machine machinev1.Machine, providerSpec vsphereapi.VSphereMachineProviderSpec) error {
	if machine.Labels[machinev1.MachineClusterIDLabel] == "" {
		return machinecontroller.InvalidMachineConfiguration("%v: missing %q label", machine.GetName(), machinev1.MachineClusterIDLabel)
	}

	return nil
}

func findVM(s *machineScope) (types.ManagedObjectReference, error) {
	uuid := string(s.machine.UID)
	objRef, err := s.GetSession().FindRefByInstanceUUID(s, uuid)
	if err != nil {
		return types.ManagedObjectReference{}, err
	}
	if objRef == nil {
		return types.ManagedObjectReference{}, errNotFound{instanceUUID: true, uuid: uuid}
	}
	return objRef.Reference(), nil
}

// errNotFound is returned by the findVM function when a VM is not found.
type errNotFound struct {
	instanceUUID bool
	uuid         string
}

func (e errNotFound) Error() string {
	if e.instanceUUID {
		return fmt.Sprintf("vm with instance uuid %s not found", e.uuid)
	}
	return fmt.Sprintf("vm with bios uuid %s not found", e.uuid)
}

func IsNotFound(err error) bool {
	switch err.(type) {
	case errNotFound, *errNotFound:
		return true
	default:
		return false
	}
}

func isRetrieveMONotFound(taskRef string, err error) bool {
	return err.Error() == fmt.Sprintf("ServerFaultCode: The object 'vim.Task:%v' has already been deleted or has not been completely created", taskRef)
}

func clone(s *machineScope) error {
	vmTemplate, err := s.GetSession().FindVM(*s, s.providerSpec.Template)
	if err != nil {
		return err
	}

	var folderPath, datastorePath, resourcepoolPath string
	if s.providerSpec.Workspace != nil {
		folderPath = s.providerSpec.Workspace.Folder
		datastorePath = s.providerSpec.Workspace.Datastore
		resourcepoolPath = s.providerSpec.Workspace.ResourcePool
	}

	folder, err := s.GetSession().Finder.FolderOrDefault(s, folderPath)
	if err != nil {
		return errors.Wrapf(err, "unable to get folder for %q", folderPath)
	}

	datastore, err := s.GetSession().Finder.DatastoreOrDefault(s, datastorePath)
	if err != nil {
		return errors.Wrapf(err, "unable to get datastore for %q", datastorePath)
	}

	resourcepool, err := s.GetSession().Finder.ResourcePoolOrDefault(s, resourcepoolPath)
	if err != nil {
		return errors.Wrapf(err, "unable to get resource pool for %q", resourcepool)
	}

	numCPUs := s.providerSpec.NumCPUs
	if numCPUs < minCPU {
		numCPUs = minCPU
	}
	numCoresPerSocket := s.providerSpec.NumCoresPerSocket
	if numCoresPerSocket == 0 {
		numCoresPerSocket = numCPUs
	}
	memMiB := s.providerSpec.MemoryMiB
	if memMiB == 0 {
		memMiB = minMemMB
	}

	spec := types.VirtualMachineCloneSpec{
		Config: &types.VirtualMachineConfigSpec{
			Annotation: s.machine.GetName(),
			// Assign the clone's InstanceUUID the value of the Kubernetes Machine
			// object's UID. This allows lookup of the cloned VM prior to knowing
			// the VM's UUID.
			InstanceUuid: string(s.machine.UID),
			Flags:        newVMFlagInfo(),
			// TODO: set userData
			//ExtraConfig:       extraConfig,
			// TODO: set devices
			//DeviceChange:      deviceSpecs,
			NumCPUs:           numCPUs,
			NumCoresPerSocket: numCoresPerSocket,
			MemoryMB:          memMiB,
		},
		Location: types.VirtualMachineRelocateSpec{
			Datastore:    types.NewReference(datastore.Reference()),
			DiskMoveType: diskMoveType,
			Folder:       types.NewReference(folder.Reference()),
			Pool:         types.NewReference(resourcepool.Reference()),
		},
		// This is implicit, but making it explicit as it is important to not
		// power the VM on before its virtual hardware is created and the MAC
		// address(es) used to build and inject the VM with cloud-init metadata
		// are generated.
		PowerOn: false,
	}

	task, err := vmTemplate.Clone(s, folder, s.machine.GetName(), spec)
	if err != nil {
		return errors.Wrapf(err, "error triggering clone op for machine %v", s)
	}

	s.providerStatus.TaskRef = task.Reference().Value
	klog.V(3).Infof("%v: running task: %+v", s.machine.GetName(), s.providerStatus.TaskRef)
	return nil
}

func newVMFlagInfo() *types.VirtualMachineFlagInfo {
	diskUUIDEnabled := true
	return &types.VirtualMachineFlagInfo{
		DiskUuidEnabled: &diskUUIDEnabled,
	}
}

func taskIsFinished(task *mo.Task) (bool, error) {
	if task == nil {
		return true, nil
	}

	// Otherwise the course of action is determined by the state of the task.
	klog.V(3).Infof("task: %v, state: %v, description-id: %v", task.Reference().Value, task.Info.State, task.Info.DescriptionId)
	switch task.Info.State {
	case types.TaskInfoStateQueued:
		return false, nil
	case types.TaskInfoStateRunning:
		return false, nil
	case types.TaskInfoStateSuccess:
		return true, nil
	case types.TaskInfoStateError:
		return true, nil
	default:
		return false, errors.Errorf("task: %v, unknown state %v", task.Reference().Value, task.Info.State)
	}
}

type virtualMachine struct {
	context.Context
	Ref types.ManagedObjectReference
	Obj *object.VirtualMachine
}

func (vm *virtualMachine) reconcilePowerState() (bool, error) {
	powerState, err := vm.getPowerState()
	if err != nil {
		return false, err
	}
	switch powerState {
	case types.VirtualMachinePowerStatePoweredOff:
		klog.Infof("%v: powering on", vm.Obj.Reference().Value)
		_, err := vm.powerOnVM()
		if err != nil {
			return false, errors.Wrapf(err, "failed to trigger power on op for vm %q", vm)
		}
		// TODO: store task in providerStatus/conditions?
		klog.Infof("%v: requeue to wait for power on state", vm.Obj.Reference().Value)
		return false, nil
	case types.VirtualMachinePowerStatePoweredOn:
		klog.Infof("%v: powered on", vm.Obj.Reference().Value)
	default:
		return false, errors.Errorf("unexpected power state %q for vm %q", powerState, vm)
	}

	return true, nil
}

func (vm *virtualMachine) powerOnVM() (string, error) {
	task, err := vm.Obj.PowerOn(vm.Context)
	if err != nil {
		return "", err
	}
	return task.Reference().Value, nil
}

func (vm *virtualMachine) powerOffVM() (string, error) {
	task, err := vm.Obj.PowerOff(vm.Context)
	if err != nil {
		return "", err
	}
	return task.Reference().Value, nil
}

func (vm *virtualMachine) getPowerState() (types.VirtualMachinePowerState, error) {
	powerState, err := vm.Obj.PowerState(vm.Context)
	if err != nil {
		return "", err
	}

	switch powerState {
	case types.VirtualMachinePowerStatePoweredOn:
		return types.VirtualMachinePowerStatePoweredOn, nil
	case types.VirtualMachinePowerStatePoweredOff:
		return types.VirtualMachinePowerStatePoweredOff, nil
	case types.VirtualMachinePowerStateSuspended:
		return types.VirtualMachinePowerStateSuspended, nil
	default:
		return "", errors.Errorf("unexpected power state %q for vm %v", powerState, vm)
	}
}
