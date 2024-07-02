package v1

import (
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// NutanixMachineProviderConfig is the Schema for the nutanixmachineproviderconfigs API
// Compatibility level 1: Stable within a major release for a minimum of 12 months or 3 minor releases (whichever is longer).
// +openshift:compatibility-gen:level=1
// +k8s:openapi-gen=true
type NutanixMachineProviderConfig struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// cluster is to identify the cluster (the Prism Element under management
	// of the Prism Central), in which the Machine's VM will be created.
	// The cluster identifier (uuid or name) can be obtained from the Prism Central console
	// or using the prism_central API.
	// +kubebuilder:validation:Required
	Cluster NutanixResourceIdentifier `json:"cluster"`

	// image is to identify the rhcos image uploaded to the Prism Central (PC)
	// The image identifier (uuid or name) can be obtained from the Prism Central console
	// or using the prism_central API.
	// +kubebuilder:validation:Required
	Image NutanixResourceIdentifier `json:"image"`

	// subnets holds a list of identifiers (one or more) of the cluster's network subnets
	// for the Machine's VM to connect to. The subnet identifiers (uuid or name) can be
	// obtained from the Prism Central console or using the prism_central API.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinItems=1
	Subnets []NutanixResourceIdentifier `json:"subnets"`

	// vcpusPerSocket is the number of vCPUs per socket of the VM
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	VCPUsPerSocket int32 `json:"vcpusPerSocket"`

	// vcpuSockets is the number of vCPU sockets of the VM
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	VCPUSockets int32 `json:"vcpuSockets"`

	// memorySize is the memory size (in Quantity format) of the VM
	// The minimum memorySize is 2Gi bytes
	// +kubebuilder:validation:Required
	MemorySize resource.Quantity `json:"memorySize"`

	// systemDiskSize is size (in Quantity format) of the system disk of the VM
	// The minimum systemDiskSize is 20Gi bytes
	// +kubebuilder:validation:Required
	SystemDiskSize resource.Quantity `json:"systemDiskSize"`

	// bootType indicates the boot type (Legacy, UEFI or SecureBoot) the Machine's VM uses to boot.
	// If this field is empty or omitted, the VM will use the default boot type "Legacy" to boot.
	// "SecureBoot" depends on "UEFI" boot, i.e., enabling "SecureBoot" means that "UEFI" boot is also enabled.
	// +kubebuilder:validation:Enum="";Legacy;UEFI;SecureBoot
	// +optional
	BootType NutanixBootType `json:"bootType"`

	// project optionally identifies a Prism project for the Machine's VM to associate with.
	// +optional
	Project NutanixResourceIdentifier `json:"project"`

	// categories optionally adds one or more prism categories (each with key and value) for
	// the Machine's VM to associate with. All the category key and value pairs specified must
	// already exist in the prism central.
	// +listType=map
	// +listMapKey=key
	// +optional
	Categories []NutanixCategory `json:"categories"`

	// gpus is a list of GPU devices to add to the machine's VM.
	// +listType=map
	// +listMapKey=type
	// +optional
	GPUs []NutanixGPU `json:"gpus,omitempty"`

	// dataDisks holds information of the data disks attached to the Machine's VM
	// +listType=set
	// +optional
	DataDisks []NutanixVMDisk `json:"dataDisks,omitempty"`

	// userDataSecret is a local reference to a secret that contains the
	// UserData to apply to the VM
	UserDataSecret *corev1.LocalObjectReference `json:"userDataSecret,omitempty"`

	// credentialsSecret is a local reference to a secret that contains the
	// credentials data to access Nutanix PC client
	// +kubebuilder:validation:Required
	CredentialsSecret *corev1.LocalObjectReference `json:"credentialsSecret"`

	// failureDomain refers to the name of the FailureDomain with which this Machine is associated.
	// If this is configured, the Nutanix machine controller will use the prism_central endpoint
	// and credentials defined in the referenced FailureDomain to communicate to the prism_central.
	// It will also verify that the 'cluster' and subnets' configuration in the NutanixMachineProviderConfig
	// is consistent with that in the referenced failureDomain.
	// +optional
	FailureDomain *NutanixFailureDomainReference `json:"failureDomain"`
}

// NutanixCategory identifies a pair of prism category key and value
type NutanixCategory struct {
	// key is the prism category key name
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// value is the prism category value associated with the key
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=64
	// +kubebuilder:validation:Required
	Value string `json:"value"`
}

// NutanixBootType is an enumeration of different boot types for Nutanix VM.
type NutanixBootType string

const (
	// NutanixLegacyBoot is the legacy BIOS boot type
	NutanixLegacyBoot NutanixBootType = "Legacy"

	// NutanixUEFIBoot is the UEFI boot type
	NutanixUEFIBoot NutanixBootType = "UEFI"

	// NutanixSecureBoot is the Secure boot type
	NutanixSecureBoot NutanixBootType = "SecureBoot"
)

// NutanixIdentifierType is an enumeration of different resource identifier types.
type NutanixIdentifierType string

const (
	// NutanixIdentifierUUID is a resource identifier identifying the object by UUID.
	NutanixIdentifierUUID NutanixIdentifierType = "uuid"

	// NutanixIdentifierName is a resource identifier identifying the object by Name.
	NutanixIdentifierName NutanixIdentifierType = "name"
)

// NutanixResourceIdentifier holds the identity of a Nutanix PC resource (cluster, image, subnet, etc.)
// +union
type NutanixResourceIdentifier struct {
	// Type is the identifier type to use for this resource.
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum:=uuid;name
	Type NutanixIdentifierType `json:"type"`

	// uuid is the UUID of the resource in the PC.
	// +optional
	UUID *string `json:"uuid,omitempty"`

	// name is the resource name in the PC
	// +optional
	Name *string `json:"name,omitempty"`
}

// NutanixGPUIdentifierType is an enumeration of different resource identifier types for GPU entities.
type NutanixGPUIdentifierType string

const (
	// NutanixGPUIdentifierName identifies a GPU by Name.
	NutanixGPUIdentifierName NutanixGPUIdentifierType = "Name"

	// NutanixGPUIdentifierDeviceID identifies a GPU by device ID.
	NutanixGPUIdentifierDeviceID NutanixGPUIdentifierType = "DeviceID"
)

// NutanixGPU holds the identity of a Nutanix GPU resource in the Prism Central
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'DeviceID' ?  has(self.deviceID) : !has(self.deviceID)",message="deviceID configuration is required when type is DeviceID, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Name' ?  has(self.name) : !has(self.name)",message="name configuration is required when type is Name, and forbidden otherwise"
// +union
type NutanixGPU struct {
	// type is the identifier type to use for this GPU resource.
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	Type NutanixGPUIdentifierType `json:"type"`

	// deviceID is the id of the GPU entity.
	// +optional
	// +unionMember
	DeviceID *int64 `json:"deviceID,omitempty"`

	// name is the GPU name
	// +optional
	// +unionMember
	Name *string `json:"name,omitempty"`
}

// NutanixStorageContainerReferenceType is an enumeration of different storage_container reference types.
// +kubebuilder:validation:Enum:=UUID;Name
type NutanixStorageContainerReferenceType string

const (
	// NutanixStorageContainerReferenceUUID is a reference type identifying the storage_container object by UUID.
	NutanixStorageContainerReferenceUUID NutanixStorageContainerReferenceType = "UUID"

	// NutanixStorageContainerReferenceName is a reference type identifying the storage_container object by Name.
	NutanixStorageContainerReferenceName NutanixStorageContainerReferenceType = "Name"

	// NutanixStorageContainerReferenceURL is a reference type identifying the storage_container object by URL.
	NutanixStorageContainerReferenceURL NutanixStorageContainerReferenceType = "URL"
)

// NutanixStorageContainerReference references to a storage_container object.
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'UUID' ?  has(self.uuid) : !has(self.uuid)",message="uuid configuration is required when type is UUID, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'Name' ?  has(self.name) : !has(self.name)",message="name configuration is required when type is Name, and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.type) && self.type == 'URL' ?  has(self.url) : !has(self.url)",message="url configuration is required when type is URL, and forbidden otherwise"
// +union
type NutanixStorageContainerReference struct {
	// type is the reference type for reference to the storage_container object.
	// +unionDiscriminator
	// +kubebuilder:validation:Required
	Type NutanixStorageContainerReferenceType `json:"type"`

	// uuid is the UUID of the storage_container object. It cannot be empty if the type is UUID.
	// +optional
	// +unionMember
	UUID *string `json:"uuid,omitempty"`

	// name is the name of the storage_container object. It cannot be empty if the type is Name.
	// +optional
	// +unionMember
	Name *string `json:"name,omitempty"`

	// url is the source URL of the storage_container object. It cannot be empty if the type is URL.
	// +optional
	// +unionMember
	URL *string `json:"url,omitempty"`
}

// NutanixVMStorageConfig specifies the storage configuration parameters for VM disks.
type NutanixVMStorageConfig struct {
	// flashMode specifies whether to pin the VM disk to the flash tier.
	// +optional
	FlashMode bool `json:"flashMode,omitempty"`

	// storageContainer refers to the storage_container used by the VM disk.
	// +optional
	StorageContainer *NutanixStorageContainerReference `json:"storageContainer,omitempty"`
}

// NutanixDiskAdapterType is the disk device adapter type
type NutanixDiskAdapterType string

const (
	// NutanixDiskAdapterTypeSCSI represents the disk adapter type "SCSI".
	NutanixDiskAdapterTypeSCSI NutanixDiskAdapterType = "SCSI"

	// NutanixDiskAdapterTypeIDE represents the disk adapter type "IDE".
	NutanixDiskAdapterTypeIDE NutanixDiskAdapterType = "IDE"

	// NutanixDiskAdapterTypePCI represents the disk adapter type "PCI".
	NutanixDiskAdapterTypePCI NutanixDiskAdapterType = "PCI"

	// NutanixDiskAdapterTypeSATA represents the disk adapter type "SATA".
	NutanixDiskAdapterTypeSATA NutanixDiskAdapterType = "SATA"
)

// NutanixDiskAddress specifies the disk address.
type NutanixDiskAddress struct {
	// adapterType is the adapter type of the disk address.
	// +kubebuilder:default=SCSI
	// +kubebuilder:validation:Enum=SCSI;IDE;PCI;SATA
	AdapterType NutanixDiskAdapterType `json:"adapterType,omitempty"`

	// deviceIndex is the index of the disk address.
	DeviceIndex int64 `json:"deviceIndex,omitempty"`
}

// NutanixDiskDeviceType is the VM disk device type
type NutanixDiskDeviceType string

const (
	// NutanixDiskDeviceTypeDisk specifies the VM disk device type is DISK.
	NutanixDiskDeviceTypeDisk NutanixDiskDeviceType = "DISK"

	// NutanixDiskDeviceTypeCDROM specifies the VM disk device type is DISK.
	NutanixDiskDeviceTypeCDROM NutanixDiskDeviceType = "CDROM"
)

// NutanixVMDiskDeviceProperties specifies the
type NutanixVMDiskDeviceProperties struct {
	// deviceType is a disk type. The default is DISK.
	// +kubebuilder:default=DISK
	// +kubebuilder:validation:Enum=DISK;CDROM
	DeviceType NutanixDiskDeviceType `json:"deviceType"`

	// diskAddress is the address of disk to boot from.
	DiskAddress NutanixDiskAddress `json:"diskAddress,omitempty"`
}

// NutanixDataDisk specifies the VM data disk configuration parameters.
type NutanixVMDisk struct {
	// diskSize is size (in Quantity format) of the disk attached to the VM
	// The minimum diskSize is 1Gi bytes
	// +kubebuilder:validation:Required
	DiskSize resource.Quantity `json:"diskSize"`

	// deviceProperties are the properties of the disk device.
	// +optional
	DeviceProperties *NutanixVMDiskDeviceProperties `json:"deviceProperties,omitempty"`

	// storageConfig are the storage configuration parameters of the VM disks.
	// +optional
	StorageConfig *NutanixVMStorageConfig `json:"storageConfig,omitempty"`

	// dataSource refers to a data source image for the VM disk
	// +optional
	DataSource *configv1.NutanixResourceIdentifier `json:"dataSource,omitempty"`
}

// NutanixMachineProviderStatus is the type that will be embedded in a Machine.Status.ProviderStatus field.
// It contains nutanix-specific status information.
// Compatibility level 1: Stable within a major release for a minimum of 12 months or 3 minor releases (whichever is longer).
// +openshift:compatibility-gen:level=1
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type NutanixMachineProviderStatus struct {
	metav1.TypeMeta `json:",inline"`

	// conditions is a set of conditions associated with the Machine to indicate
	// errors or other status
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// vmUUID is the Machine associated VM's UUID
	// The field is missing before the VM is created.
	// Once the VM is created, the field is filled with the VM's UUID and it will not change.
	// The vmUUID is used to find the VM when updating the Machine status,
	// and to delete the VM when the Machine is deleted.
	// +optional
	VmUUID *string `json:"vmUUID,omitempty"`
}
