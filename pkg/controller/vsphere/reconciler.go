package vsphere

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/vmware/govmomi/task"

	"github.com/openshift/machine-api-operator/pkg/util/ipam"

	"github.com/google/uuid"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	apimachineryutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"

	apifeatures "github.com/openshift/api/features"
	machinev1 "github.com/openshift/api/machine/v1beta1"

	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere/session"
	"github.com/openshift/machine-api-operator/pkg/metrics"
)

const (
	fullCloneDiskMoveType = string(types.VirtualMachineRelocateDiskMoveOptionsMoveAllDiskBackingsAndConsolidate)
	linkCloneDiskMoveType = string(types.VirtualMachineRelocateDiskMoveOptionsCreateNewChildDiskBacking)
	ethCardType           = "vmxnet3"
	providerIDPrefix      = "vsphere://"
	regionKey             = "region"
	zoneKey               = "zone"
	minimumHWVersion      = 15
	// maxUnitNumber constant is used to define the maximum number of devices that can be assigned to a virtual machine's controller.
	// Not all controllers support up to 30, but the maximum is 30.
	// xref: https://docs.vmware.com/en/VMware-vSphere/8.0/vsphere-vm-administration/GUID-5872D173-A076-42FE-8D0B-9DB0EB0E7362.html#:~:text=If%20you%20add%20a%20hard,values%20from%200%20to%2014.
	maxUnitNumber = 30
	// VSphereCSIDriverName is the CSI driver name for vSphere volumes.
	VSphereCSIDriverName = "csi.vsphere.vmware.com"
	// VSphereInTreePluginName is the in-tree plugin name for vSphere volumes.
	VSphereInTreePluginName = "kubernetes.io/vsphere-volume"
	// csiPluginPrefix is the prefix for CSI volume names in node.Status.VolumesAttached.
	// Format: kubernetes.io/csi/<driverName>^<volumeHandle>
	csiPluginPrefix = "kubernetes.io/csi/"
	// csiVolNameSep separates the driver name from the volume handle in CSI volume names.
	csiVolNameSep = "^"
)

// These are the guestinfo variables used by Ignition.
// https://access.redhat.com/documentation/en-us/openshift_container_platform/4.1/html/installing/installing-on-vsphere
const (
	GuestInfoIgnitionData     = "guestinfo.ignition.config.data"
	GuestInfoIgnitionEncoding = "guestinfo.ignition.config.data.encoding"
	GuestInfoHostname         = "guestinfo.hostname"
	GuestInfoNetworkKargs     = "guestinfo.afterburn.initrd.network-kargs"
	StealClock                = "stealclock.enable"
)

// vSphere tasks description IDs, for determinate task types (clone, delete, etc)
const (
	cloneVmTaskDescriptionId    = "VirtualMachine.clone"
	destroyVmTaskDescriptionId  = "VirtualMachine.destroy"
	powerOffVmTaskDescriptionId = "VirtualMachine.powerOff"
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

// addVMGroupAndPowerOn restores VM group membership (if configured) and
// powers the machine on, persisting the power-on task ref in the provider
// status. Shared by the recovered-VM path and the completed-clone path in
// create() so the two cannot drift apart.
func (r *Reconciler) addVMGroupAndPowerOn(kind string) error {
	if r.machineScope.providerSpec.Workspace.VMGroup != "" {
		klog.Infof("Adding %s machine: %s to vm group: %s", kind, r.machine.Name, r.machineScope.providerSpec.Workspace.VMGroup)
		if err := modifyVMGroup(r.machineScope, false); err != nil {
			var taskError task.Error
			if errors.As(err, &taskError) {
				return fmt.Errorf("could not update VM Group membership: %w", taskError)
			}
			return fmt.Errorf("could not update VM Group membership: %w", err)
		}
	}
	klog.Infof("Powering on %s machine: %v", kind, r.machine.Name)
	task, err := powerOn(r.machineScope)
	if err != nil {
		metrics.RegisterFailedInstanceCreate(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    "PowerOn task finished with error",
		})
		conditionFailed := conditionFailed()
		conditionFailed.Message = err.Error()
		if statusError := setProviderStatus(task, conditionFailed, r.machineScope, nil); statusError != nil {
			return fmt.Errorf("failed to set provider status: %w", err)
		}
		return fmt.Errorf("%v: failed to power on machine: %w", r.machine.GetName(), err)
	}
	return setProviderStatus(task, conditionSuccess(), r.machineScope, nil)
}

// create creates machine if it does not exists.
func (r *Reconciler) create() error {
	if err := validateMachine(*r.machine); err != nil {
		return fmt.Errorf("%v: failed validating machine provider spec: %w", r.machine.GetName(), err)
	}

	if r.providerSpec.Workspace.VMGroup != "" && !r.featureGates.Enabled(featuregate.Feature(apifeatures.FeatureGateVSphereHostVMGroupZonal)) {
		return fmt.Errorf("%v: vmGroup is only available with the VSphereHostVMGroupZonal feature gate", r.machine.GetName())
	}

	if ipam.HasStaticIPConfiguration(r.providerSpec) {
		outstandingClaims, err := ipam.HasOutstandingIPAddressClaims(
			r.Context,
			r.client,
			r.machine,
			r.providerSpec.Network.Devices,
		)
		if err != nil {
			return err
		}
		condition := metav1.Condition{
			Type:    string(machinev1.IPAddressClaimedCondition),
			Reason:  machinev1.IPAddressClaimedReason,
			Message: "All IP address claims are bound",
			Status:  metav1.ConditionTrue,
		}

		if outstandingClaims > 0 {
			condition.Message = fmt.Sprintf("Waiting on %d IP address claims to be bound", outstandingClaims)
			condition.Reason = machinev1.WaitingForIPAddressReason
			condition.Status = metav1.ConditionFalse
			klog.Infof("Waiting for IPAddressClaims associated with machine %s to be bound", r.machine.Name)
		}
		if err := setProviderStatus("", condition, r.machineScope, nil); err != nil {
			return fmt.Errorf("could not set provider status: %w", err)
		}
	}

	// We only clone the VM template if we have no taskRef.
	if r.providerStatus.TaskRef == "" {
		klog.V(4).Infof("%v: ProviderStatus does not have TaskRef", r.machine.GetName())
		if !r.machineScope.session.IsVC() {
			return fmt.Errorf("%v: not connected to a vCenter", r.machine.GetName())
		}

		// A missing TaskRef usually means the VM has not been cloned yet. It can
		// also mean we cloned the VM successfully but lost the TaskRef because
		// the status patch that would have persisted it failed (for example, a
		// transient admission-webhook denial during install). Look the VM up
		// directly in vCenter before cloning so that a lost TaskRef never
		// results in a duplicate VM: if the VM already exists we adopt it and
		// power it on, otherwise we clone the template.
		if _, err := findVM(r.machineScope); err != nil {
			if !isNotFound(err) {
				metrics.RegisterFailedInstanceCreate(&metrics.MachineLabels{
					Name:      r.machine.Name,
					Namespace: r.machine.Namespace,
					Reason:    "FindVM finished with error",
				})
				return err
			}

			klog.Infof("%v: cloning", r.machine.GetName())
			// A new clone has a different identity. Clear values from a VM that
			// may have disappeared so the next update records the new VM identity.
			r.machine.Spec.ProviderID = nil
			r.providerStatus.InstanceID = nil
			task, err := clone(r.machineScope)
			if err != nil {
				metrics.RegisterFailedInstanceCreate(&metrics.MachineLabels{
					Name:      r.machine.Name,
					Namespace: r.machine.Namespace,
					Reason:    "Clone task finished with error",
				})
				conditionFailed := conditionFailed()
				conditionFailed.Message = err.Error()
				statusError := setProviderStatus(task, conditionFailed, r.machineScope, nil)
				if statusError != nil {
					return fmt.Errorf("failed to set provider status: %w", err)
				}
				return err
			}
			return setProviderStatus(task, conditionSuccess(), r.machineScope, nil)
		}

		// The VM already exists but we have no TaskRef for it: we cloned it
		// previously and lost the TaskRef. Complete the post-clone sequence to
		// recover — restore VM group membership (if configured) and power the VM
		// on, recording the power-on task so subsequent reconciles can track it,
		// instead of requeueing forever. This mirrors the completed-clone path
		// below so a recovered VM is not left outside its configured VM group.
		klog.Infof("%v: VM already exists without a persisted taskRef, recovering", r.machine.GetName())
		return r.addVMGroupAndPowerOn("recovered")
	}

	moTask, err := r.session.GetTask(r.Context, r.providerStatus.TaskRef)
	if err != nil {
		if !isRetrieveMONotFound(r.providerStatus.TaskRef, err) {
			metrics.RegisterFailedInstanceCreate(&metrics.MachineLabels{
				Name:      r.machine.Name,
				Namespace: r.machine.Namespace,
				Reason:    "GetTask finished with error",
			})
			return err
		}
		// Task history eviction or a session restart can make the clone
		// task ref permanently unavailable. Clear it; the moTask == nil
		// block below probes for the VM instead of failing forever.
		klog.Infof("%v: task %s no longer found, clearing TaskRef", r.machine.GetName(), r.providerStatus.TaskRef)
		r.providerStatus.TaskRef = ""
	}

	if moTask == nil {
		// The clone task is gone from vCenter. If the VM exists the clone
		// succeeded; reconcile it into steady state instead of failing on
		// the missing task.
		if vmRef, err := findVM(r.machineScope); err != nil {
			return err
		} else if vmRef != (types.ManagedObjectReference{}) {
			klog.Infof("%v: clone task gone but VM found, reconciling VM state", r.machine.GetName())
			vm := r.machineScope.newVM(r.machineScope.Context, vmRef)
			return r.reconcileMachineWithCloudState(vm, r.providerStatus.TaskRef)
		}
		// Possible eventual consistency problem from vsphere
		// TODO: change error message here to indicate this might be expected.
		return fmt.Errorf("unexpected moTask nil")
	}

	if taskIsFinished, err := taskIsFinished(moTask); err != nil {
		if taskIsFinished {
			metrics.RegisterFailedInstanceCreate(&metrics.MachineLabels{
				Name:      r.machine.Name,
				Namespace: r.machine.Namespace,
				Reason:    "Task finished with error",
			})
			conditionFailed := conditionFailed()
			conditionFailed.Message = err.Error()
			statusError := setProviderStatus(moTask.Reference().Value, conditionFailed, r.machineScope, nil)
			if statusError != nil {
				return fmt.Errorf("failed to set provider status: %w", statusError)
			}
			return machinecontroller.CreateMachine("%s", err.Error())
		} else {
			return fmt.Errorf("failed to check task status: %w", err)
		}
	} else {
		if taskIsFinished {
			klog.V(4).Infof("%v task %v has completed", moTask.Info.DescriptionId, moTask.Reference().Value)
		} else {
			return fmt.Errorf("%v task %v has not finished", moTask.Info.DescriptionId, moTask.Reference().Value)
		}
	}

	// if clone task finished successfully, power on the vm
	// The simulator task.Info.DescriptionId is different (VirtualMachine.cloneVM)
	if strings.Contains(moTask.Info.DescriptionId, cloneVmTaskDescriptionId) {
		return r.addVMGroupAndPowerOn("cloned")
	}

	// If taskIsFinished then next reconcile should result in update.
	return nil
}

// update finds a vm and reconciles the machine resource status against it.
func (r *Reconciler) update() error {
	if err := validateMachine(*r.machine); err != nil {
		return fmt.Errorf("%v: failed validating machine provider spec: %w", r.machine.GetName(), err)
	}

	if r.providerStatus.TaskRef != "" {
		moTask, err := r.session.GetTask(r.Context, r.providerStatus.TaskRef)
		if err != nil {
			if !isRetrieveMONotFound(r.providerStatus.TaskRef, err) {
				metrics.RegisterFailedInstanceUpdate(&metrics.MachineLabels{
					Name:      r.machine.Name,
					Namespace: r.machine.Namespace,
					Reason:    "GetTask finished with error",
				})
				return err
			}
			// Task history eviction or a session restart can make a task
			// ref permanently unavailable. Clear it so future resyncs do
			// not keep issuing the same GetTask request.
			klog.Infof("%v: task %s no longer found, clearing TaskRef", r.machine.GetName(), r.providerStatus.TaskRef)
			r.providerStatus.TaskRef = ""
		}
		if moTask != nil {
			if taskIsFinished, err := taskIsFinished(moTask); err != nil {
				metrics.RegisterFailedInstanceUpdate(&metrics.MachineLabels{
					Name:      r.machine.Name,
					Namespace: r.machine.Namespace,
					Reason:    "Task finished with error",
				})
				if taskIsFinished {
					// A terminally failed task cannot transition to success. Clear
					// its ref so retries reconcile the VM instead of polling it forever.
					r.providerStatus.TaskRef = ""
				}
				return fmt.Errorf("%v task %v finished with error: %w", moTask.Info.DescriptionId, moTask.Reference().Value, err)
			} else if !taskIsFinished {
				return fmt.Errorf("%v task %v has not finished", moTask.Info.DescriptionId, moTask.Reference().Value)
			} else {
				// A completed task can never transition again. Clear its ref
				// so steady-state resyncs skip GetTask entirely.
				klog.Infof("%v: task %v has completed, clearing TaskRef", r.machine.GetName(), moTask.Reference().Value)
				r.providerStatus.TaskRef = ""
			}
		}
	}

	vmRef, err := findVM(r.machineScope)
	if err != nil {
		metrics.RegisterFailedInstanceUpdate(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    "FindVM finished with error",
		})
		if !isNotFound(err) {
			return err
		}
		return fmt.Errorf("vm not found on update: %w", err)
	}

	vm := r.machineScope.newVM(r.machineScope.Context, vmRef)

	if err := vm.reconcileTags(r.Context, r.session.GetCachingTagsManager(), r.machine, r.providerSpec); err != nil {
		metrics.RegisterFailedInstanceUpdate(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    "ReconcileTags finished with error",
		})
		return fmt.Errorf("failed to reconcile tags: %w", err)
	}

	if err := r.reconcileMachineWithCloudState(vm, r.providerStatus.TaskRef); err != nil {
		metrics.RegisterFailedInstanceUpdate(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    "ReconcileWithCloudState finished with error",
		})
		return err
	}

	return nil
}

// exists returns true if machine exists.
func (r *Reconciler) exists() (bool, error) {
	if err := validateMachine(*r.machine); err != nil {
		return false, fmt.Errorf("%v: failed validating machine provider spec: %w", r.machine.GetName(), err)
	}

	vmRef, err := findVM(r.machineScope)
	if err != nil {
		if !isNotFound(err) {
			return false, err
		}
		klog.Infof("%v: does not exist", r.machine.GetName())
		return false, nil
	}

	// Check if machine was powered on after clone.
	// If it is powered off and in "Provisioning" phase, treat machine as non-existed yet and proceed with creation procedure.
	powerState := types.VirtualMachinePowerState(ptr.Deref(r.machineScope.providerStatus.InstanceState, ""))
	if powerState == "" || ptr.Deref(r.machine.Status.Phase, "") == machinev1.PhaseProvisioning {
		vm := r.machineScope.newVM(r.machineScope.Context, vmRef)
		powerState, err = vm.getPowerState()
		if err != nil {
			return false, fmt.Errorf("%v: failed checking machine's power state: %w", r.machine.GetName(), err)
		}
	}

	if ptr.Deref(r.machine.Status.Phase, "") == machinev1.PhaseProvisioning && powerState == types.VirtualMachinePowerStatePoweredOff {
		klog.Infof("%v: already exists, but was not powered on after clone", r.machine.GetName())
		r.machineScope.providerStatus.InstanceState = ptr.To(string(powerState))
		if err := r.machineScope.PatchMachine(); err != nil {
			return false, fmt.Errorf("%v: failed to patch machine: %w", r.machine.GetName(), err)
		}
		return false, nil
	}

	klog.Infof("%v: already exists", r.machine.GetName())
	return true, nil
}

func (r *Reconciler) delete() error {
	if r.providerStatus.TaskRef != "" {
		// TODO: We need to use a separate status field for the create and the
		// delete taskref.
		moTask, err := r.session.GetTask(r.Context, r.providerStatus.TaskRef)
		if err != nil {
			if !isRetrieveMONotFound(r.providerStatus.TaskRef, err) {
				return err
			}
		}
		if moTask != nil {
			if taskIsFinished, err := taskIsFinished(moTask); err != nil {
				// Check if latest task is not a task for vm cloning
				if taskIsFinished && moTask.Info.DescriptionId != cloneVmTaskDescriptionId {
					metrics.RegisterFailedInstanceDelete(&metrics.MachineLabels{
						Name:      r.machine.Name,
						Namespace: r.machine.Namespace,
						Reason:    "Task finished with error",
					})
					klog.Errorf("Delete task finished with error: %v", err)
					return fmt.Errorf("%v task %v finished with error: %w", moTask.Info.DescriptionId, moTask.Reference().Value, err)
				} else {
					klog.Warningf(
						"TaskRef points to clone task which finished with error: %v. Proceeding with machine deletion", err,
					)
				}
			} else if !taskIsFinished {
				return fmt.Errorf("%v task %v has not finished", moTask.Info.DescriptionId, moTask.Reference().Value)
			}
		}
	}

	vmRef, err := findVM(r.machineScope)
	if err != nil {
		if !isNotFound(err) {
			metrics.RegisterFailedInstanceDelete(&metrics.MachineLabels{
				Name:      r.machine.Name,
				Namespace: r.machine.Namespace,
				Reason:    "FindVM finished with error",
			})
			return err
		}
		klog.Infof("%v: vm does not exist", r.machine.GetName())

		// remove any finalizers for IPAddressClaims which may be associated with the machine
		err = ipam.RemoveFinalizersForIPAddressClaims(r.Context, r.client, *r.machine)
		if err != nil {
			return fmt.Errorf("unable to remove finalizer for IP address claims: %w", err)
		}

		return nil
	}

	vm := r.machineScope.newVM(r.Context, vmRef)

	powerState, err := vm.getPowerState()
	if err != nil {
		return fmt.Errorf("can not determine %v vm power state: %w", r.machine.GetName(), err)
	}
	if powerState != types.VirtualMachinePowerStatePoweredOff {
		powerOffTaskRef, err := vm.powerOffVM()
		if err != nil {
			return fmt.Errorf("%v: failed to power off vm: %w", r.machine.GetName(), err)
		}
		if err := setProviderStatus(powerOffTaskRef, conditionSuccess(), r.machineScope, vm); err != nil {
			return fmt.Errorf("failed to set provider status: %w", err)
		}
		return fmt.Errorf("powering off vm is in progress, requeuing")
	}

	// At this point node should be drained and vm powered off already.
	// We need to check attached disks and ensure that all disks potentially related to PVs were detached
	// to prevent possible data loss.
	// Destroying a VM with attached disks might lead to data loss in case pvs are handled by the intree storage driver.

	_, drainSkipped := r.machine.ObjectMeta.Annotations[machinecontroller.ExcludeNodeDrainingAnnotation]

	// If node linked to the machine, and node was drained checking node status first
	if r.machineScope.isNodeLinked() && !drainSkipped {
		// After node draining, make sure volumes are detached before deleting the Node.
		attached, err := r.nodeHasVolumesAttached(r.Context, r.machine.Status.NodeRef.Name, r.machine.Name)
		if err != nil {
			return fmt.Errorf("failed to determine if node %v has attached volumes: %w", r.machine.Status.NodeRef.Name, err)
		}
		if attached {
			// If there are volumes still attached, it's possible that node draining did not fully finish,
			// this might happen if the kubelet was non-functional during the draining procedure.
			// Try forcefully deleting pods in the "Terminating" state to trigger persistent volumes detachment.
			klog.Warningf(
				"Attached volumes detected on a powered off node, node draining may not succeed. " +
					"Attempting to delete unevicted pods",
			)
			numPodsDeleted, err := r.machineScope.deleteUnevictedPods()
			klog.Warningf("Deleted %d pods", numPodsDeleted)
			if err != nil {
				return fmt.Errorf("unable to fully drain node, can not delete unevicted pods: %w", err)
			}
			return fmt.Errorf("node %v has attached volumes, requeuing", r.machine.Status.NodeRef.Name)
		}
	}

	klog.V(3).Infof("Checking attached disks before vm destroy")
	disks, err := vm.getAttachedDisks()
	if err != nil {
		return fmt.Errorf("%v: can not obtain virtual disks attached to the vm: %w", r.machine.GetName(), err)
	}

	additionalDisks := len(r.providerSpec.DataDisks)
	// Currently, MAPI only allows VMs to be configured w/ 1 primary disk in the template and a limited number of additional
	// disks via the data disks configuration.  So, we are expecting the VM to have only one disk, which is OS disk, plus
	// the additional disks defined in the DataDisks configuration.
	if len(disks) > 1+additionalDisks {
		// If node drain was skipped we need to detach disks forcefully to prevent possible data corruption.
		if drainSkipped {
			klog.V(1).Infof(
				"%s: drain was skipped for the machine, detaching disks before vm destruction to prevent data loss",
				r.machine.GetName(),
			)
			if err := vm.detachDisks(filterOutVmOsDisk(disks, r.machine)); err != nil {
				return fmt.Errorf("failed to detach disks: %w", err)
			}
			klog.V(1).Infof(
				"%s: disks were detached", r.machine.GetName(),
			)
			return errors.New(
				"disks were detached, vm will be attempted to destroy in next reconciliation, requeuing",
			)
		}

		// Block vm destruction till attach-detach controller has properly detached disks
		return errors.New(
			"additional attached disks detected, block vm destruction and wait for disks to be detached",
		)
	}

	task, err := vm.Obj.Destroy(r.Context)
	if err != nil {
		metrics.RegisterFailedInstanceDelete(&metrics.MachineLabels{
			Name:      r.machine.Name,
			Namespace: r.machine.Namespace,
			Reason:    "Destroy finished with error",
		})
		return fmt.Errorf("%v: failed to destroy vm: %w", r.machine.GetName(), err)
	}

	if r.machineScope.providerSpec.Workspace.VMGroup != "" {
		klog.Infof("Removing machine: %v from vm group: %v", r.machine.Name, r.machineScope.providerSpec.Workspace.VMGroup)
		if err := modifyVMGroup(r.machineScope, true); err != nil {
			return fmt.Errorf("failed to remove machine from vm group: %w", err)
		}
	}

	if err := setProviderStatus(task.Reference().Value, conditionSuccess(), r.machineScope, vm); err != nil {
		return fmt.Errorf("failed to set provider status: %w", err)
	}

	// TODO: consider returning an error to specify retry time here
	return fmt.Errorf("destroying vm in progress, requeuing")
}

// nodeHasVolumesAttached returns true if node status still has vSphere-backed volumes attached.
// Pod deletion and volume detach happen asynchronously, so pod could be deleted before volume detached from the node.
// This is problematic for vSphere volumes because if the node is deleted before detach succeeds,
// the underlying VMDK will be deleted together with the Machine.
// Non-vSphere volumes (NFS, iSCSI, etc.) do not have this risk since vSphere Destroy_Task does not affect them.
func (r *Reconciler) nodeHasVolumesAttached(ctx context.Context, nodeName string, machineName string) (bool, error) {
	node := &corev1.Node{}
	if err := r.apiReader.Get(ctx, apimachinerytypes.NamespacedName{Name: nodeName}, node); err != nil {
		if apierrors.IsNotFound(err) {
			klog.Errorf("Could not find node from noderef, it may have already been deleted: %v", err)
			return false, nil
		}
		return true, err
	}

	if len(node.Status.VolumesAttached) == 0 {
		return false, nil
	}

	klog.V(3).Infof("Machine %s: checking %d attached volumes on node %s for vSphere-backed volumes", machineName, len(node.Status.VolumesAttached), nodeName)

	var vsphereVolumes []string
	var nonVSphereVolumes []string
	var unknownVolumes []string

	for _, vol := range node.Status.VolumesAttached {
		volName := string(vol.Name)

		switch classifyAttachedVolume(volName) {
		case volumeClassVSphere:
			vsphereVolumes = append(vsphereVolumes, volName)
		case volumeClassNonVSphere:
			nonVSphereVolumes = append(nonVSphereVolumes, volName)
		case volumeClassUnknown:
			unknownVolumes = append(unknownVolumes, volName)
		}
	}

	if len(vsphereVolumes) > 0 {
		klog.Warningf("Machine %s: vSphere-backed volumes still attached on node %s: %v", machineName, nodeName, vsphereVolumes)
	}
	if len(nonVSphereVolumes) > 0 {
		klog.V(3).Infof("Machine %s: non-vSphere volumes attached (safe to ignore): %v", machineName, nonVSphereVolumes)
	}
	if len(unknownVolumes) > 0 {
		klog.Warningf("Machine %s on node %s: volumes with unrecognized name format, conservatively blocking deletion: %v", machineName, nodeName, unknownVolumes)
	}

	return len(vsphereVolumes) > 0 || len(unknownVolumes) > 0, nil
}

type volumeClass int

const (
	volumeClassVSphere volumeClass = iota
	volumeClassNonVSphere
	volumeClassUnknown
)

// classifyAttachedVolume determines whether an attached volume is vSphere-backed
// by parsing the UniqueVolumeName format from node.Status.VolumesAttached.
// CSI volumes use the format: kubernetes.io/csi/<driverName>^<volumeHandle>
// In-tree vSphere volumes use the prefix: kubernetes.io/vsphere-volume/
func classifyAttachedVolume(volName string) volumeClass {
	if strings.HasPrefix(volName, VSphereInTreePluginName+"/") {
		return volumeClassVSphere
	}

	if strings.HasPrefix(volName, csiPluginPrefix) {
		remainder := strings.TrimPrefix(volName, csiPluginPrefix)
		if sepIdx := strings.Index(remainder, csiVolNameSep); sepIdx > 0 {
			driver := remainder[:sepIdx]
			if driver == VSphereCSIDriverName {
				return volumeClassVSphere
			}
			return volumeClassNonVSphere
		}
	}

	return volumeClassUnknown
}

// reconcileMachineWithCloudState reconcile machineSpec and status with the latest cloud state
func (r *Reconciler) reconcileMachineWithCloudState(vm *virtualMachine, taskRef string) error {
	klog.V(3).Infof("%v: reconciling machine with cloud state", r.machine.GetName())
	// TODO: reconcile task

	if err := r.reconcileRegionAndZoneLabels(vm); err != nil {
		// Not treating this is as a fatal error for now.
		klog.Errorf("Failed to reconcile region and zone labels: %v", err)
	}

	klog.V(3).Infof("%v: reconciling providerID", r.machine.GetName())
	if err := r.reconcileProviderID(vm); err != nil {
		return err
	}

	klog.V(3).Infof("%v: reconciling network", r.machine.GetName())
	if err := r.reconcileNetwork(vm); err != nil {
		return err
	}

	klog.V(3).Infof("%v: reconciling powerstate annotation", r.machine.GetName())
	if err := r.reconcilePowerStateAnnontation(vm); err != nil {
		return err
	}

	return setProviderStatus(taskRef, conditionSuccess(), r.machineScope, vm)
}

// reconcileRegionAndZoneLabels reconciles the labels on the Machine containing
// region and zone information -- provided the vSphere cloud provider has been
// configured with the labels that identify region and zone, and the configured
// tags are found somewhere in the ancestry of the given virtual machine.
func (r *Reconciler) reconcileRegionAndZoneLabels(vm *virtualMachine) error {
	if r.vSphereConfig == nil {
		klog.Warning("No vSphere cloud provider config. " +
			"Will not set region and zone labels.")
		return nil
	}

	// Region/zone come from tags on the VM's ancestry and are immutable
	// after provisioning. If the labels are already set, skip the tag
	// traversal (HostSystem + Ancestors + N REST tag calls per resync).
	if r.machine.Labels[machinecontroller.MachineRegionLabelName] != "" &&
		r.machine.Labels[machinecontroller.MachineAZLabelName] != "" {
		return nil
	}

	regionLabel := r.vSphereConfig.Labels.Region
	zoneLabel := r.vSphereConfig.Labels.Zone

	// Use cached tag manager to avoid creating new REST sessions.
	// This eliminates excessive vCenter login/logout cycles.
	tagManager := r.session.GetCachingTagsManager()
	res, err := vm.getRegionAndZone(tagManager, regionLabel, zoneLabel)
	if err != nil {
		return err
	}

	if r.machine.Labels == nil {
		r.machine.Labels = make(map[string]string)
	}

	r.machine.Labels[machinecontroller.MachineRegionLabelName] = res[regionKey]
	r.machine.Labels[machinecontroller.MachineAZLabelName] = res[zoneKey]

	return nil
}

func (r *Reconciler) reconcileProviderID(vm *virtualMachine) error {
	if r.machine.Spec.ProviderID != nil && *r.machine.Spec.ProviderID != "" {
		return nil
	}

	providerID, err := convertUUIDToProviderID(vm.Obj.UUID(vm.Context))
	if err != nil {
		return err
	}
	r.machine.Spec.ProviderID = &providerID
	return nil
}

// convertUUIDToProviderID transforms a UUID string into a provider ID.
func convertUUIDToProviderID(UUID string) (string, error) {
	parsedUUID, err := uuid.Parse(UUID)
	if err != nil {
		return "", err
	}
	return providerIDPrefix + parsedUUID.String(), nil
}

func (r *Reconciler) reconcileNetwork(vm *virtualMachine) error {
	// The same property call also seeds the power-state cache.
	currentNetworkStatusList, vmName, err := vm.getNetworkAndPowerStatus(r.session.Client.Client)
	if err != nil {
		return fmt.Errorf("error getting network status: %v", err)
	}

	//If the VM is powered on then issue requeues until all of the VM's
	//networks have IP addresses.
	expectNetworkLen, currentNetworkLen := len(r.providerSpec.Network.Devices), len(currentNetworkStatusList)
	if expectNetworkLen != currentNetworkLen {
		return fmt.Errorf("invalid network count: expected=%d current=%d", expectNetworkLen, currentNetworkLen)
	}

	var ipAddrs []corev1.NodeAddress
	for _, netStatus := range currentNetworkStatusList {
		for _, ip := range netStatus.IPAddrs {
			ipAddrs = append(ipAddrs, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: ip,
			})
		}
	}

	ipAddrs = append(ipAddrs, corev1.NodeAddress{
		Type:    corev1.NodeInternalDNS,
		Address: vmName,
	})

	klog.V(3).Infof("%v: reconciling network: IP addresses: %v", r.machine.GetName(), ipAddrs)
	r.machine.Status.Addresses = ipAddrs

	// If static IP, verify machine still has IPAddressClaim w/ owner field configure
	if ipam.HasStaticIPConfiguration(r.providerSpec) {
		err = ipam.VerifyIPAddressOwners(r.Context, r.client, r.machine, r.providerSpec.Network.Devices)
		if err != nil {
			return fmt.Errorf("error verifying ip address claims: %v", err)
		}
	}

	return nil
}

func (r *Reconciler) reconcilePowerStateAnnontation(vm *virtualMachine) error {
	if vm == nil {
		return errors.New("provided VM is nil")
	}

	// This can return an error if machine is being deleted
	powerState, err := vm.getPowerState()
	if err != nil {
		return err
	}

	if r.machine.Annotations == nil {
		r.machine.Annotations = map[string]string{}
	}
	r.machine.Annotations[machinecontroller.MachineInstanceStateAnnotationName] = string(powerState)

	return nil
}

func validateMachine(machine machinev1.Machine) error {
	if machine.Labels[machinev1.MachineClusterIDLabel] == "" {
		return machinecontroller.InvalidMachineConfiguration("%v: missing %q label", machine.GetName(), machinev1.MachineClusterIDLabel)
	}

	return nil
}

func findVM(s *machineScope) (types.ManagedObjectReference, error) {
	uuid := string(s.machine.UID)

	vm, err := s.GetSession().FindVM(s.Context, uuid, s.machine.Name)
	if err != nil {
		if isNotFound(err) {
			return types.ManagedObjectReference{}, errNotFound{instanceUUID: true, uuid: uuid}
		}
		return types.ManagedObjectReference{}, err
	}

	if vm == nil {
		return types.ManagedObjectReference{}, errNotFound{instanceUUID: true, uuid: uuid}
	}

	return vm.Reference(), nil
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

func isNotFound(err error) bool {
	switch err.(type) {
	case errNotFound, *errNotFound, *find.NotFoundError:
		return true
	default:
		return false
	}
}

func getSubnetMask(prefix netip.Prefix) (string, error) {
	prefixLength := net.IPv4len * 8
	if prefix.Addr().Is6() {
		prefixLength = net.IPv6len * 8
	}
	ipMask := net.CIDRMask(prefix.Masked().Bits(), prefixLength)
	maskBytes, err := hex.DecodeString(ipMask.String())
	if err != nil {
		return "", fmt.Errorf("could not translate ip mask: %w", err)
	}
	ip := net.IP(maskBytes)
	maskStr := ip.To16().String()
	return maskStr, nil
}

// getAddressesFromPool retrieves IP addresses and associated gateway from IP address pools
func getAddressesFromPool(configIdx int, networkConfig machinev1.NetworkDeviceSpec, s *machineScope) ([]string, string, error) {
	addresses := []string{}
	var gateway string
	for poolIdx := range networkConfig.AddressesFromPools {
		claimName := ipam.GetIPAddressClaimName(s.machine, configIdx, poolIdx)
		ipAddress, err := ipam.RetrieveBoundIPAddress(s.Context, s.client, s.machine, claimName)
		if err != nil {
			return nil, "", fmt.Errorf("error retrieving bound IP address: %w", err)
		}
		ipAddressSpec := ipAddress.Spec
		addresses = append(addresses, fmt.Sprintf("%s/%d", ipAddressSpec.Address, ipAddressSpec.Prefix))
		if len(ipAddressSpec.Gateway) > 0 {
			gateway = ipAddressSpec.Gateway
		}
	}
	return addresses, gateway, nil
}

// constructKargsFromNetworkConfig builds a string which comprises ip and nameserver stanzas
// which are consumed by guestinfo.afterburn.initrd.network-kargs.
func constructKargsFromNetworkConfig(s *machineScope) (string, error) {
	outKargs := ""
	networkConfigs := s.providerSpec.Network.Devices
	for configIdx, networkConfig := range networkConfigs {
		// retrieve any IP addresses assigned by an IP address pool
		addressesFromPool, gatewayFromPool, err := getAddressesFromPool(configIdx, networkConfig, s)
		if err != nil {
			return "", fmt.Errorf("error getting addresses from IP pool: %w", err)
		}
		var gateway string
		if len(gatewayFromPool) > 0 {
			gateway = gatewayFromPool
		} else {
			gateway = networkConfig.Gateway
		}

		var gatewayIp netip.Addr
		if len(gateway) > 0 {
			gatewayIp, err = netip.ParseAddr(gateway)
			if err != nil {
				return "", fmt.Errorf("error parsing gateway address: %w", err)
			}
		}

		ipAddresses := []string{}
		ipAddresses = append(ipAddresses, networkConfig.IPAddrs...)
		ipAddresses = append(ipAddresses, addressesFromPool...)

		// construct IP address network kargs for each IP address
		for _, address := range ipAddresses {
			prefix, err := netip.ParsePrefix(address)
			if err != nil {
				return "", fmt.Errorf("error parsing prefix: %w", err)
			}
			var ipStr, gatewayStr, maskStr string
			addr := prefix.Addr()
			// IPv6 addresses must be wrapped in [] for dracut network kargs
			if addr.Is6() {
				maskStr = fmt.Sprintf("%d", prefix.Bits())
				ipStr = fmt.Sprintf("[%s]", addr.String())
				if len(gateway) > 0 && gatewayIp.Is6() {
					gatewayStr = fmt.Sprintf("[%s]", gateway)
				}
			} else if addr.Is4() {
				maskStr, err = getSubnetMask(prefix)
				if err != nil {
					return "", fmt.Errorf("error getting subnet mask: %w", err)
				}
				if len(gateway) > 0 && gatewayIp.Is4() {
					gatewayStr = gateway
				}
				ipStr = addr.String()
			} else {
				return "", errors.New("IP address must adhere to IPv4 or IPv6 format")
			}

			outKargs = outKargs + fmt.Sprintf("ip=%s::%s:%s:::none ", ipStr, gatewayStr, maskStr)
		}

		// construct nameserver network karg for each defined nameserver
		for _, nameserver := range networkConfig.Nameservers {
			ip := net.ParseIP(nameserver)
			if ip.To4() == nil {
				nameserver = fmt.Sprintf("[%s]", nameserver)
			}
			outKargs = outKargs + fmt.Sprintf("nameserver=%s ", nameserver)
		}
	}
	return outKargs, nil
}

func isRetrieveMONotFound(taskRef string, err error) bool {
	if err == nil {
		return false
	}
	errMessage := err.Error()
	return errMessage == fmt.Sprintf("ServerFaultCode: The object 'vim.Task:%v' has already been deleted or has not been completely created", taskRef) ||
		errMessage == "ServerFaultCode: The object has already been deleted or has not been completely created"
}

func getHwVersion(ctx context.Context, vm *object.VirtualMachine) (int, error) {
	var _vm mo.VirtualMachine
	if err := vm.Properties(ctx, vm.Reference(), []string{"config.version"}, &_vm); err != nil {
		return 0, fmt.Errorf("error getting hw version information for vm %s: %w", vm.Name(), err)
	}

	versionString := _vm.Config.Version
	version := strings.TrimPrefix(versionString, "vmx-")
	parsedVersion, err := strconv.Atoi(version)
	if err != nil {
		return 0, fmt.Errorf("can not extract hardware version from version string: %s, format unknown", versionString)
	}
	return parsedVersion, nil
}

func clone(s *machineScope) (string, error) {
	userData, err := s.GetUserData()
	if err != nil {
		return "", err
	}

	vmTemplate, err := s.GetSession().FindVM(*s, "", s.providerSpec.Template)
	if err != nil {
		const multipleFoundMsg = "multiple templates found, specify one in config"
		const notFoundMsg = "template not found, specify valid value"
		defaultError := fmt.Errorf("unable to get template %q: %w", s.providerSpec.Template, err)
		return "", handleVSphereError(multipleFoundMsg, notFoundMsg, defaultError, err)
	}

	hwVersion, err := getHwVersion(s.Context, vmTemplate)
	if err != nil {
		return "", machinecontroller.InvalidMachineConfiguration(
			"Unable to detect machine template HW version for machine '%s': %v", s.machine.GetName(), err,
		)
	}
	if hwVersion < minimumHWVersion {
		return "", machinecontroller.InvalidMachineConfiguration(
			"Hardware lower than %d is not supported, clone stopped. "+
				"Detected machine template version is %d. "+
				"Please update machine template: https://docs.openshift.com/container-platform/latest/updating/updating_a_cluster/updating-hardware-on-nodes-running-on-vsphere.html",
			minimumHWVersion, hwVersion,
		)
	}

	// Default clone type is FullClone, having snapshot on clonee template will cause incorrect disk sizing.
	diskMoveType := fullCloneDiskMoveType
	var snapshotRef *types.ManagedObjectReference

	// If a linked clone is requested then a MoRef for a snapshot must be
	// found with which to perform the linked clone.
	// Empty clone mode is a full clone,
	// because otherwise disk size from provider spec will not be respected.
	if s.providerSpec.CloneMode == machinev1.LinkedClone {
		if s.providerSpec.DiskGiB > 0 {
			klog.Warningf("LinkedClone mode is set. Disk size parameter from ProviderSpec will be ignored")
		}
		if s.providerSpec.Snapshot == "" {
			klog.V(3).Infof("%v: no snapshot name provided, getting snapshot using template", s.machine.GetName())
			var vm mo.VirtualMachine
			if err := vmTemplate.Properties(s.Context, vmTemplate.Reference(), []string{"snapshot"}, &vm); err != nil {
				return "", fmt.Errorf("error getting snapshot information for template %s: %w", vmTemplate.Name(), err)
			}

			if vm.Snapshot != nil {
				snapshotRef = vm.Snapshot.CurrentSnapshot
			}
		} else {
			klog.V(3).Infof("%v: searching for snapshot by name %s", s.machine.GetName(), s.providerSpec.Snapshot)
			var err error
			snapshotRef, err = vmTemplate.FindSnapshot(s.Context, s.providerSpec.Snapshot)
			if err != nil {
				// Maybe return an error there?
				klog.V(3).Infof("%v: failed to find snapshot %s, fallback to FullClone", s.machine.GetName(), s.providerSpec.Snapshot)
			}
		}

		if snapshotRef != nil {
			diskMoveType = linkCloneDiskMoveType
		}
	}

	var folderPath, datastorePath, resourcepoolPath string
	if s.providerSpec.Workspace != nil {
		folderPath = s.providerSpec.Workspace.Folder
		datastorePath = s.providerSpec.Workspace.Datastore
		resourcepoolPath = s.providerSpec.Workspace.ResourcePool
	}

	folder, err := s.GetSession().Finder.FolderOrDefault(s, folderPath)
	if err != nil {
		const multipleFoundMsg = "multiple folders found, specify one in config"
		const notFoundMsg = "folder not found, specify valid value"
		defaultError := fmt.Errorf("unable to get folder for %q: %w", folderPath, err)
		return "", handleVSphereError(multipleFoundMsg, notFoundMsg, defaultError, err)
	}

	datastore, err := s.GetSession().Finder.DatastoreOrDefault(s, datastorePath)
	if err != nil {
		const multipleFoundMsg = "multiple datastores found, specify one in config"
		const notFoundMsg = "datastore not found, specify valid value"
		defaultError := fmt.Errorf("unable to get datastore for %q: %w", datastorePath, err)
		return "", handleVSphereError(multipleFoundMsg, notFoundMsg, defaultError, err)
	}

	resourcepool, err := s.GetSession().Finder.ResourcePoolOrDefault(s, resourcepoolPath)
	if err != nil {
		const multipleFoundMsg = "multiple resource pools found, specify one in config"
		const notFoundMsg = "resource pool not found, specify valid value"
		defaultError := fmt.Errorf("unable to get resource pool for %q: %w", resourcepool, err)
		return "", handleVSphereError(multipleFoundMsg, notFoundMsg, defaultError, err)
	}

	numCPUs := s.providerSpec.NumCPUs

	numCoresPerSocket := s.providerSpec.NumCoresPerSocket
	if numCoresPerSocket == 0 {
		numCoresPerSocket = numCPUs
	}

	devices, err := vmTemplate.Device(s.Context)
	if err != nil {
		return "", fmt.Errorf("error getting devices %v", err)
	}

	// Create a new list of device specs for cloning the VM.
	deviceSpecs := []types.BaseVirtualDeviceConfigSpec{}

	// Only non-linked clones may expand the size of the template's disk.
	if snapshotRef == nil {
		diskSpec, err := getDiskSpec(s, devices)
		if err != nil {
			return "", fmt.Errorf("error getting disk spec for %q: %w", s.providerSpec.Snapshot, err)
		}
		deviceSpecs = append(deviceSpecs, diskSpec)
	}

	// Process all DataDisks definitions to dynamically create and add disks to the VM
	additionalDisks, err := createDataDisks(s, devices)
	if err != nil {
		return "", fmt.Errorf("error getting additional disk specs: %w", err)
	}
	deviceSpecs = append(deviceSpecs, additionalDisks...)

	klog.V(3).Infof("Getting network devices")
	networkDevices, err := getNetworkDevices(s, resourcepool, devices)
	if err != nil {
		return "", fmt.Errorf("error getting network specs: %w", err)
	}

	deviceSpecs = append(deviceSpecs, networkDevices...)

	extraConfig := []types.BaseOptionValue{}

	extraConfig = append(extraConfig, IgnitionConfig(userData)...)
	extraConfig = append(extraConfig, &types.OptionValue{
		Key:   GuestInfoHostname,
		Value: s.machine.GetName(),
	})
	extraConfig = append(extraConfig, &types.OptionValue{
		Key:   StealClock,
		Value: "TRUE",
	})

	if ipam.HasStaticIPConfiguration(s.providerSpec) {
		networkKargs, err := constructKargsFromNetworkConfig(s)
		if err != nil {
			return "", err
		}
		if len(networkKargs) > 0 {
			extraConfig = append(extraConfig, &types.OptionValue{
				Key:   GuestInfoNetworkKargs,
				Value: networkKargs,
			})
		}
	}

	spec := types.VirtualMachineCloneSpec{
		Config: &types.VirtualMachineConfigSpec{
			Annotation: s.machine.GetName(),
			// Assign the clone's InstanceUUID the value of the Kubernetes Machine
			// object's UID. This allows lookup of the cloned VM prior to knowing
			// the VM's UUID.
			InstanceUuid:      string(s.machine.UID),
			Flags:             newVMFlagInfo(),
			ExtraConfig:       extraConfig,
			DeviceChange:      deviceSpecs,
			NumCPUs:           numCPUs,
			NumCoresPerSocket: &numCoresPerSocket,
			MemoryMB:          s.providerSpec.MemoryMiB,
		},
		Location: types.VirtualMachineRelocateSpec{
			Datastore:    types.NewReference(datastore.Reference()),
			Folder:       types.NewReference(folder.Reference()),
			Pool:         types.NewReference(resourcepool.Reference()),
			DiskMoveType: diskMoveType,
		},
		PowerOn:  false, // Create powered off machine, for power it on later in "create" procedure
		Snapshot: snapshotRef,
	}

	task, err := vmTemplate.Clone(s, folder, s.machine.GetName(), spec)
	if err != nil {
		return "", fmt.Errorf("error triggering clone op for machine %v: %w", s, err)
	}
	taskVal := task.Reference().Value
	klog.V(3).Infof("%v: running task: %+v", s.machine.GetName(), taskVal)
	return taskVal, nil
}

// newVM builds a virtualMachine for the given managed object reference.
func (s *machineScope) newVM(ctx context.Context, vmRef types.ManagedObjectReference) *virtualMachine {
	return &virtualMachine{
		Context: ctx,
		Obj:     object.NewVirtualMachine(s.session.Client.Client, vmRef),
		Ref:     vmRef,
	}
}

func modifyVMGroup(s *machineScope, delete bool) error {
	vmRef, err := findVM(s)
	if err != nil {
		if isNotFound(err) {
			return fmt.Errorf("virtual machine %s was not found: %w", s.machine.Name, err)
		}
		return fmt.Errorf("error finding virtual machine: %w", err)
	}

	rp, err := s.session.Finder.ResourcePool(s.Context, s.providerSpec.Workspace.ResourcePool)
	if err != nil {
		return fmt.Errorf("error getting resource pool %s: %w", s.providerSpec.Workspace.ResourcePool, err)
	}

	ownerRef, err := rp.Owner(s.Context)
	if err != nil {
		return fmt.Errorf("error getting cluster owner reference from resource pool %s: %w", s.providerSpec.Workspace.ResourcePool, err)
	}

	var ccr *object.ClusterComputeResource
	var ok bool
	if ccr, ok = ownerRef.(*object.ClusterComputeResource); !ok {
		return fmt.Errorf("error getting cluster from resource pool %s: %w", s.providerSpec.Workspace.ResourcePool, err)
	}

	clusterConfig, err := ccr.Configuration(s.Context)
	if err != nil {
		return fmt.Errorf("error getting cluster %s configuration: %w", s.providerSpec.Workspace.ResourcePool, err)
	}

	var clusterVmGroup *types.ClusterVmGroup

	for _, g := range clusterConfig.Group {
		if vmg, ok := g.(*types.ClusterVmGroup); ok {
			if vmg.Name == s.providerSpec.Workspace.VMGroup {
				clusterVmGroup = vmg
				break
			}
		}
	}

	switch {
	case clusterVmGroup == nil:
		clusterVmGroup = &types.ClusterVmGroup{
			Vm: []types.ManagedObjectReference{vmRef},
		}
	case slices.Contains(clusterVmGroup.Vm, vmRef) && delete:
		clusterVmGroup.Vm = slices.DeleteFunc(clusterVmGroup.Vm, func(ref types.ManagedObjectReference) bool {
			return vmRef.Value == ref.Value
		})
	case !slices.Contains(clusterVmGroup.Vm, vmRef):
		clusterVmGroup.Vm = append(clusterVmGroup.Vm, vmRef)
	default:
		return nil
	}

	clusterConfigSpec := &types.ClusterConfigSpecEx{
		GroupSpec: []types.ClusterGroupSpec{
			{
				ArrayUpdateSpec: types.ArrayUpdateSpec{
					Operation: types.ArrayUpdateOperation("edit"),
				},
				Info: &types.ClusterVmGroup{
					ClusterGroupInfo: types.ClusterGroupInfo{
						Name: s.providerSpec.Workspace.VMGroup,
					},
					Vm: clusterVmGroup.Vm,
				},
			},
		},
	}

	clusterTask, err := ccr.Reconfigure(s.Context, clusterConfigSpec, true)
	if err != nil {
		return fmt.Errorf("error reconfiguring cluster %s for vm-host group %s: %w", ccr.Name(), clusterVmGroup.Name, err)
	}

	return clusterTask.Wait(s.Context)
}

func powerOn(s *machineScope) (string, error) {
	vmRef, err := findVM(s)
	if err != nil {
		if !isNotFound(err) {
			return "", err
		}
		return "", fmt.Errorf("vm not found during creation for powering on: %w", err)
	}

	datacenter := s.session.Datacenter
	if datacenter == nil { // if there is no dataceneter, fallback to old powerOn method via vm object
		vm := s.newVM(s.Context, vmRef)

		return vm.powerOnVM()
	}

	overrideDRS := &types.OptionValue{
		Key:   string(types.ClusterPowerOnVmOptionOverrideAutomationLevel),
		Value: string(types.DrsBehaviorFullyAutomated),
	}
	task, err := datacenter.PowerOnVM(s.Context, []types.ManagedObjectReference{vmRef}, overrideDRS)
	if err != nil {
		return "", fmt.Errorf("error powering on %s vm: %w", s.machine.Name, err)
	}
	return task.Reference().Value, nil
}

func getDiskSpec(s *machineScope, devices object.VirtualDeviceList) (types.BaseVirtualDeviceConfigSpec, error) {
	disks := devices.SelectByType((*types.VirtualDisk)(nil))
	if len(disks) != 1 {
		return nil, fmt.Errorf("invalid disk count: %d", len(disks))
	}

	disk := disks[0].(*types.VirtualDisk)
	cloneCapacityKB := int64(s.providerSpec.DiskGiB) * 1024 * 1024
	if disk.CapacityInKB > cloneCapacityKB {
		return nil, machinecontroller.InvalidMachineConfiguration(
			"can't resize template disk down, initial capacity is larger: %dKiB > %dKiB",
			disk.CapacityInKB, cloneCapacityKB)
	}
	disk.CapacityInKB = cloneCapacityKB

	return &types.VirtualDeviceConfigSpec{
		Operation: types.VirtualDeviceConfigSpecOperationEdit,
		Device:    disk,
	}, nil
}

func createDataDisks(s *machineScope, devices object.VirtualDeviceList) ([]types.BaseVirtualDeviceConfigSpec, error) {
	var diskSpecs []types.BaseVirtualDeviceConfigSpec

	// Get primary disk
	disks := devices.SelectByType((*types.VirtualDisk)(nil))
	if len(disks) == 0 {
		return nil, fmt.Errorf("invalid disk count: %d", len(disks))
	}

	// There is at least one disk
	primaryDisk := disks[0].(*types.VirtualDisk)

	// Get the controller of the primary disk.
	controller, ok := devices.FindByKey(primaryDisk.ControllerKey).(types.BaseVirtualController)
	if !ok {
		return nil, fmt.Errorf("unable to find controller with key=%v", primaryDisk.ControllerKey)
	}

	controllerKey := controller.GetVirtualController().Key
	unitNumberAssigner, err := newUnitNumberAssigner(controller, devices)
	if err != nil {
		return nil, fmt.Errorf("unable to create unit number assigner: %v", err)
	}

	// Let's create the data disks now
	for i, dataDisk := range s.providerSpec.DataDisks {
		klog.V(2).InfoS("Adding disk", "name", dataDisk.Name, "spec", dataDisk)

		backing := &types.VirtualDiskFlatVer2BackingInfo{
			DiskMode: string(types.VirtualDiskModePersistent),
			VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
				FileName: "",
			},
		}

		// Set provisioning type for the new data disk.
		// Currently, if ThinProvisioned is not set, GOVC will set default to false.  We may want to change this behavior
		// to match what template image OS disk has configured to make them match if not set.
		switch dataDisk.ProvisioningMode {
		case machinev1.ProvisioningModeThin:
			backing.ThinProvisioned = types.NewBool(true)
		case machinev1.ProvisioningModeThick:
			backing.ThinProvisioned = types.NewBool(false)
		case machinev1.ProvisioningModeEagerlyZeroed:
			backing.ThinProvisioned = types.NewBool(false)
			backing.EagerlyScrub = types.NewBool(true)
		default:
			klog.V(2).Infof("No provisioning type detected.  Leaving configuration empty.")
		}

		dev := &types.VirtualDisk{
			VirtualDevice: types.VirtualDevice{
				// Key needs to be unique and cannot match another new disk being added.  So we'll use the index as an
				// input to NewKey.  NewKey() will always return same value since our new devices are not part of devices yet.
				Key:           devices.NewKey() - int32(i),
				Backing:       backing,
				ControllerKey: controller.GetVirtualController().Key,
			},
			CapacityInKB: int64(dataDisk.SizeGiB) * 1024 * 1024,
		}

		vd := dev.GetVirtualDevice()
		vd.ControllerKey = controllerKey

		// Assign unit number to the new disk.  Should be next available slot on the controller.
		unitNumber, err := unitNumberAssigner.assign()
		if err != nil {
			return nil, err
		}
		vd.UnitNumber = &unitNumber

		klog.V(2).InfoS("Created device for data disk device", "name", dataDisk.Name, "spec", dataDisk, "device", dev)
		diskSpecs = append(diskSpecs, &types.VirtualDeviceConfigSpec{
			Device:        dev,
			Operation:     types.VirtualDeviceConfigSpecOperationAdd,
			FileOperation: types.VirtualDeviceConfigSpecFileOperationCreate,
		})
	}

	return diskSpecs, nil
}

type unitNumberAssigner struct {
	used   []bool
	offset int32
}

func newUnitNumberAssigner(controller types.BaseVirtualController, existingDevices object.VirtualDeviceList) (*unitNumberAssigner, error) {
	if controller == nil {
		return nil, errors.New("controller parameter cannot be nil")
	}
	used := make([]bool, maxUnitNumber)

	// SCSIControllers also use a unit.
	if scsiController, ok := controller.(types.BaseVirtualSCSIController); ok {
		used[scsiController.GetVirtualSCSIController().ScsiCtlrUnitNumber] = true
	}
	controllerKey := controller.GetVirtualController().Key

	// Mark all unit numbers of existing devices as used
	for _, device := range existingDevices {
		d := device.GetVirtualDevice()
		if d.ControllerKey == controllerKey && d.UnitNumber != nil {
			used[*d.UnitNumber] = true
		}
	}

	// Set offset to 0, it will auto-increment on the first assignment.
	return &unitNumberAssigner{used: used, offset: 0}, nil
}

func (a *unitNumberAssigner) assign() (int32, error) {
	if int(a.offset) > len(a.used) {
		return -1, fmt.Errorf("all unit numbers are already in-use")
	}
	for i, isInUse := range a.used[a.offset:] {
		unit := int32(i) + a.offset
		if !isInUse {
			a.used[unit] = true
			a.offset++
			return unit, nil
		}
	}
	return -1, fmt.Errorf("all unit numbers are already in-use")
}

func getNetworkDevices(s *machineScope, resourcepool *object.ResourcePool, devices object.VirtualDeviceList) ([]types.BaseVirtualDeviceConfigSpec, error) {
	var networkDevices []types.BaseVirtualDeviceConfigSpec
	// Remove any existing NICs
	for _, dev := range devices.SelectByType((*types.VirtualEthernetCard)(nil)) {
		networkDevices = append(networkDevices, &types.VirtualDeviceConfigSpec{
			Device:    dev,
			Operation: types.VirtualDeviceConfigSpecOperationRemove,
		})
	}

	// Add new NICs based on the machine config.
	for i := range s.providerSpec.Network.Devices {
		var ccrMo mo.ClusterComputeResource
		var backing types.BaseVirtualDeviceBackingInfo

		netSpec := &s.providerSpec.Network.Devices[i]
		klog.V(3).Infof("Adding device: %v", netSpec.NetworkName)

		clusterRef, err := resourcepool.Owner(s.Context)
		if err != nil {
			return nil, fmt.Errorf("unable to find cluster resource: %w", err)
		}

		clusterRes := object.NewClusterComputeResource(s.GetSession().Client.Client, clusterRef.Reference())
		err = clusterRes.Properties(s.Context, clusterRef.Reference(), []string{"network"}, &ccrMo)
		if err != nil {
			return nil, fmt.Errorf("unable to get list of networks in cluster: %w", err)
		}

		for _, netRef := range ccrMo.Network {
			// Use generic network object to get name
			genericNetwork := object.NewNetwork(s.GetSession().Client.Client, netRef)
			networkName, err := genericNetwork.ObjectName(s.Context)
			if err != nil {
				return nil, fmt.Errorf("unable to get network name: %w", err)
			}
			if netSpec.NetworkName == networkName {
				// Use more specific network reference to get Ethernet info
				ref := object.NewReference(s.GetSession().Client.Client, netRef)
				networkObject, ok := ref.(object.NetworkReference)
				if !ok {
					return nil, fmt.Errorf("unable to create new ethernet card backing info for network %q: network type failure: %s", netSpec.NetworkName, ref.Reference().Type)
				}

				backing, err = networkObject.EthernetCardBackingInfo(s.Context)
				if err != nil {
					return nil, fmt.Errorf("unable to create new ethernet card backing info for network %q: %w", netSpec.NetworkName, err)
				}
				break
			}
		}

		if backing == nil {
			return nil, machinecontroller.InvalidMachineConfiguration("unable to get network for %q", netSpec.NetworkName)
		}

		dev, err := object.EthernetCardTypes().CreateEthernetCard(ethCardType, backing)
		if err != nil {
			return nil, fmt.Errorf("unable to create new ethernet card %q for network %q: %w", ethCardType, netSpec.NetworkName, err)
		}

		// Get the actual NIC object. This is safe to assert without a check
		// because "object.EthernetCardTypes().CreateEthernetCard" returns a
		// "types.BaseVirtualEthernetCard" as a "types.BaseVirtualDevice".
		nic := dev.(types.BaseVirtualEthernetCard).GetVirtualEthernetCard()
		// Assign a temporary device key to ensure that a unique one will be
		// generated when the device is created.
		nic.Key = int32(i)

		networkDevices = append(networkDevices, &types.VirtualDeviceConfigSpec{
			Device:    dev,
			Operation: types.VirtualDeviceConfigSpecOperationAdd,
		})
		klog.V(3).Infof("Adding device: eth card type: %v, network spec: %+v, device info: %+v",
			ethCardType, netSpec, dev.GetVirtualDevice().Backing)
	}

	return networkDevices, nil
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
		return true, errors.New(task.Info.Error.LocalizedMessage)
	default:
		return false, fmt.Errorf("task: %v, unknown state %v", task.Reference().Value, task.Info.State)
	}
}

// setProviderStatus updates the Machine's ProviderStatus with a task reference,
// a condition, and (optionally) instance metadata. It is called from create(),
// update(), and delete() flows; pass "" for taskRef when there is no active task.
func setProviderStatus(taskRef string, condition metav1.Condition, scope *machineScope, vm *virtualMachine) error {
	klog.Infof("%s: Updating provider status", scope.machine.Name)

	if vm != nil {
		if scope.providerStatus.InstanceID == nil || *scope.providerStatus.InstanceID == "" {
			id := vm.Obj.UUID(scope.Context)
			scope.providerStatus.InstanceID = &id
		}

		// This can return an error if machine is being deleted
		powerState, err := vm.getPowerState()
		if err != nil {
			klog.V(3).Infof("%s: Failed to get power state during provider status update: %v", scope.machine.Name, err)
		} else {
			powerStateString := string(powerState)
			scope.providerStatus.InstanceState = &powerStateString
		}
	}

	if taskRef != "" {
		scope.providerStatus.TaskRef = taskRef
	}

	scope.providerStatus.Conditions = setConditions(condition, scope.providerStatus.Conditions)

	return nil
}

func handleVSphereError(multipleFoundMsg, notFoundMsg string, defaultError, vsphereError error) error {
	var multipleFoundError *find.MultipleFoundError
	if errors.As(vsphereError, &multipleFoundError) {
		return machinecontroller.InvalidMachineConfiguration("%s", multipleFoundMsg)
	}

	var notFoundError *find.NotFoundError
	if errors.As(vsphereError, &notFoundError) {
		return machinecontroller.InvalidMachineConfiguration("%s", notFoundMsg)
	}

	return defaultError
}

type virtualMachine struct {
	context.Context
	Ref types.ManagedObjectReference
	Obj *object.VirtualMachine

	// powerState cache: one PowerState call per reconcile pass.
	// The struct is built fresh per reconcile (see update()/exists()),
	// so the cache never leaks across passes.
	ps      types.VirtualMachinePowerState
	psKnown bool
}

// getHostSystemAncestors looks up and returns vm's host system ancestors, such as "Cluster" and "Datacenter".
// Host system is using there because in vCenter cluster is an ancestor of the hypervisor host but not the vm.
func (vm *virtualMachine) getHostSystemAncestors() ([]mo.ManagedEntity, error) {
	client := vm.Obj.Client()
	pc := client.ServiceContent.PropertyCollector

	host, err := vm.Obj.HostSystem(vm.Context)
	if err != nil {
		return nil, err
	}

	return mo.Ancestors(vm.Context, client, pc, host.Reference())
}

// getRegionAndZone checks the virtual machine and each of its ancestors for the
// given region and zone labels and returns their values if found.
func (vm *virtualMachine) getRegionAndZone(tagsMgr *session.CachingTagsManager, regionLabel, zoneLabel string) (map[string]string, error) {
	result := make(map[string]string)

	objects, err := vm.getHostSystemAncestors()
	if err != nil {
		klog.Errorf("Failed to get ancestors for %s: %v", vm.Ref, err)
		return nil, err
	}

	for i := range objects {
		obj := objects[len(objects)-1-i] // Reverse order.
		klog.V(4).Infof("getRegionAndZone: Name: %s, Type: %s",
			obj.Self.Value, obj.Self.Type)

		tags, err := tagsMgr.ListAttachedTags(vm.Context, obj)
		if err != nil {
			klog.Warningf("Failed to list attached tags: %v", err)
			return nil, err
		}

		for _, value := range tags {
			tag, err := tagsMgr.GetTag(vm.Context, value)
			if err != nil {
				klog.Errorf("Failed to get tag: %v", err)
				return nil, err
			}

			category, err := tagsMgr.GetCategory(vm.Context, tag.CategoryID)
			if err != nil {
				klog.Errorf("Failed to get tag category: %v", err)
				return nil, err
			}

			switch {
			case regionLabel != "" && category.Name == regionLabel:
				result[regionKey] = tag.Name
				klog.V(2).Infof("%s has region tag (%s) with value %s",
					vm.Ref, category.Name, tag.Name)

			case zoneLabel != "" && category.Name == zoneLabel:
				result[zoneKey] = tag.Name
				klog.V(2).Infof("%s has zone tag (%s) with value %s",
					vm.Ref, category.Name, tag.Name)
			}

			// We've found both tags, return early.
			if result[regionKey] != "" && result[zoneKey] != "" {
				return result, nil
			}
		}
	}

	return result, nil
}

func (vm *virtualMachine) powerOnVM() (string, error) {
	// Invalidate the power-state cache so a subsequent getPowerState in
	// the same reconcile pass observes the new state, not the stale one.
	vm.psKnown = false
	task, err := vm.Obj.PowerOn(vm.Context)
	if err != nil {
		return "", err
	}
	return task.Reference().Value, nil
}

func (vm *virtualMachine) powerOffVM() (string, error) {
	// Invalidate the power-state cache so a subsequent getPowerState in
	// the same reconcile pass observes the new state, not the stale one.
	vm.psKnown = false
	task, err := vm.Obj.PowerOff(vm.Context)
	if err != nil {
		return "", err
	}
	return task.Reference().Value, nil
}

func (vm *virtualMachine) getPowerState() (types.VirtualMachinePowerState, error) {
	if vm.psKnown {
		return vm.ps, nil
	}

	powerState, err := vm.Obj.PowerState(vm.Context)
	if err != nil {
		return "", err
	}

	if powerState == "" {
		return "", fmt.Errorf("unexpected power state %q for vm %v", powerState, vm)
	}
	vm.ps = powerState
	vm.psKnown = true
	return powerState, nil
}

// reconcileTags ensures that the required tags are present on the virtual machine, eg the Cluster ID
// that is used by the installer on cluster deletion to ensure ther are no leaked resources.
// The attached-tag list is fetched once per reconcile via the batch
// list-attached-on-objects endpoint (the per-object list-attached action is
// documented as much slower at scale, per the Broadcom vCenter tagging
// performance white paper); every required tag is checked against it in
// memory, and any missing tags are attached in one attach-multiple call.
func (vm *virtualMachine) reconcileTags(ctx context.Context, tagManager *session.CachingTagsManager, machine *machinev1.Machine, providerSpec *machinev1.VSphereMachineProviderSpec) error {
	clusterID := machine.Labels[machinev1.MachineClusterIDLabel]
	tagIDs := append([]string{clusterID}, providerSpec.TagIDs...)
	klog.Infof("%v: Reconciling %s tags to vm", machine.GetName(), tagIDs)

	var toAttach []string

	objs, err := tagManager.ListAttachedTagsOnObjects(ctx, []mo.Reference{vm.Ref})
	if err != nil {
		return fmt.Errorf("failed to list attached tags for vm %v: %w", vm.Ref, err)
	}
	// The list may be empty (unrecognized reference) or contain more
	// entries than requested; iterate defensively instead of assuming
	// objs[0] exists.
	attachedIDs := make(map[string]bool)
	for _, obj := range objs {
		for _, id := range obj.TagIDs {
			attachedIDs[id] = true
		}
	}

	for _, tagID := range tagIDs {
		if tagID == "" {
			continue
		}
		if attachedIDs[tagID] {
			continue
		}

		if session.IsName(tagID) {
			// Resolve the name to an ID. A missing tag is not an error:
			// clusters may run without the cluster-ID tag, and attaching
			// would fail anyway.
			tag, err := tagManager.GetTag(ctx, tagID)
			if err != nil {
				if isNotFoundErr(err) {
					klog.V(3).Infof("%v: tag %q not found in vCenter, skipping attach", machine.GetName(), tagID)
					continue
				}
				return err
			}
			if attachedIDs[tag.ID] {
				continue
			}
			// Mark the queued ID so duplicate entries (repeated cluster-ID
			// or providerSpec.TagIDs) are not attached twice.
			attachedIDs[tag.ID] = true
			toAttach = append(toAttach, tag.ID)
		} else if _, err := tagManager.GetTag(ctx, tagID); err != nil {
			// Unknown tag ID: fail loudly, matching previous behavior.
			return err
		} else {
			attachedIDs[tagID] = true
			toAttach = append(toAttach, tagID)
		}
	}

	// One batched attach for everything missing (white paper:
	// attach-multiple-tags-to-object has flat latency; per-tag attach()
	// scales linearly with tag count).
	if len(toAttach) > 0 {
		klog.Infof("%v: Attaching %d tag(s) to vm", machine.GetName(), len(toAttach))
		if err := tagManager.AttachMultipleTagsToObject(ctx, toAttach, vm.Ref); err != nil {
			return err
		}
	}
	return nil
}

type NetworkStatus struct {
	// Connected is a flag that indicates whether this network is currently
	// connected to the VM.
	Connected bool

	// IPAddrs is one or more IP addresses reported by vm-tools.
	IPAddrs []string

	// MACAddr is the MAC address of the network device.
	MACAddr string

	// NetworkName is the name of the network.
	NetworkName string
}

// getNetworkAndPowerStatus fetches the VM's network status, name, and power state
// in a single property call, seeding the power-state cache for the reconcile pass.
func (vm *virtualMachine) getNetworkAndPowerStatus(client *vim25.Client) ([]NetworkStatus, string, error) {
	var obj mo.VirtualMachine
	var pc = property.DefaultCollector(client)
	var props = []string{
		"config.hardware.device",
		"guest.net",
		"name",
		"runtime.powerState",
	}

	if err := pc.RetrieveOne(vm.Context, vm.Ref, props, &obj); err != nil {
		return nil, "", fmt.Errorf("unable to fetch props %v for vm %v: %w", props, vm.Ref, err)
	}

	// Seed the power-state cache so a subsequent getPowerState in the same
	// reconcile pass does not issue a second property call. Missing states
	// are left uncached so getPowerState() still re-fetches and errors.
	if obj.Runtime.PowerState != "" {
		vm.ps = obj.Runtime.PowerState
		vm.psKnown = true
	}

	klog.V(3).Infof("Getting network status: object reference: %v", obj.Reference().Value)
	if obj.Config == nil {
		return nil, "", errors.New("config.hardware.device is nil")
	}

	var networkStatusList []NetworkStatus
	for _, device := range obj.Config.Hardware.Device {
		if dev, ok := device.(types.BaseVirtualEthernetCard); ok {
			nic := dev.GetVirtualEthernetCard()
			klog.V(3).Infof("Getting network status: device: %v, macAddress: %v", nic.DeviceInfo.GetDescription().Summary, nic.MacAddress)
			netStatus := NetworkStatus{
				MACAddr: nic.MacAddress,
			}
			if obj.Guest != nil {
				klog.V(3).Infof("Getting network status: getting guest info")
				for _, i := range obj.Guest.Net {
					klog.V(3).Infof("Getting network status: getting guest info: network: %+v", i)
					if strings.EqualFold(nic.MacAddress, i.MacAddress) {
						//TODO: sanitizeIPAddrs
						netStatus.IPAddrs = i.IpAddress
						netStatus.NetworkName = i.Network
						netStatus.Connected = i.Connected
					}
				}
			}
			networkStatusList = append(networkStatusList, netStatus)
		}
	}

	return networkStatusList, obj.Name, nil
}

type attachedDisk struct {
	device   *types.VirtualDisk
	fileName string
	diskMode string
}

// Filters out disks that look like vm OS disk or any of the additional disks.
// VM os disks filename contains the machine name in it
// and has the format like "[DATASTORE] path-within-datastore/machine-name.vmdk".
// This is based on vSphere behavior, an OS disk file gets a name that equals the target VM name during the clone operation.
func filterOutVmOsDisk(attachedDisks []attachedDisk, machine *machinev1.Machine) []attachedDisk {
	var disks []attachedDisk
	regex, _ := regexp.Compile(fmt.Sprintf(".*\\/%s(_\\d*)?.vmdk", machine.GetName()))

	for _, disk := range attachedDisks {
		if regex.MatchString(disk.fileName) {
			continue
		}
		disks = append(disks, disk)
	}
	return disks
}

func (vm *virtualMachine) getAttachedDisks() ([]attachedDisk, error) {
	var attachedDiskList []attachedDisk
	devices, err := vm.Obj.Device(vm.Context)
	if err != nil {
		return nil, err
	}

	for _, disk := range devices.SelectByType((*types.VirtualDisk)(nil)) {
		backingInfo := disk.GetVirtualDevice().Backing.(types.BaseVirtualDeviceFileBackingInfo).(*types.VirtualDiskFlatVer2BackingInfo)
		attachedDiskList = append(attachedDiskList, attachedDisk{
			device:   disk.(*types.VirtualDisk),
			fileName: backingInfo.FileName,
			diskMode: backingInfo.DiskMode,
		})
	}

	return attachedDiskList, nil
}

func (vm *virtualMachine) detachDisks(disks []attachedDisk) error {
	var errList []error

	for _, disk := range disks {
		klog.V(3).Infof("Detaching disk associated with file %v", disk.fileName)
		if err := vm.Obj.RemoveDevice(vm.Context, true, disk.device); err != nil {
			errList = append(errList, err)
			klog.Errorf("Failed to detach disk associated with file %v ", disk.fileName)
		} else {
			klog.V(3).Infof("Disk associated with file %v has been detached", disk.fileName)
		}
	}
	if len(errList) > 0 {
		return apimachineryutilerrors.NewAggregate(errList)
	}
	return nil
}

// IgnitionConfig returns a slice of option values that set the given data as
// the guest's ignition config.
func IgnitionConfig(data []byte) []types.BaseOptionValue {
	config := EncodeIgnitionConfig(data)

	if config == "" {
		return nil
	}

	return []types.BaseOptionValue{
		&types.OptionValue{
			Key:   GuestInfoIgnitionData,
			Value: config,
		},
		&types.OptionValue{
			Key:   GuestInfoIgnitionEncoding,
			Value: "base64",
		},
	}
}

// EncodeIgnitionConfig attempts to decode the given data until it looks to be
// plain-text, then returns a base64 encoded version of that plain-text.
func EncodeIgnitionConfig(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	for {
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			break
		}

		data = decoded
	}

	return base64.StdEncoding.EncodeToString(data)
}
