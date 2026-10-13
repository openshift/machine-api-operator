Administrators may wish to attach additional tags to newly provisioned VMs. The cluster API provider for vSphere
allows for this in the [`VirtualMachineCloneSpec`](https://github.com/kubernetes-sigs/cluster-api-provider-vsphere/blob/b9b2c22ea68c13cbf706f2116ced804b5afb124e/apis/v1beta1/types.go#L189-L193).
A similar provision has been made in the [`VSphereMachineProviderSpec`](https://github.com/openshift/api/blob/6d48d55c0598ec78adacdd847dcf934035ec2e1b/machine/v1beta1/types_vsphereprovider.go#L54-L59).

Up to 10 tags are attachable by their tag URN(for example: `urn:vmomi:InventoryServiceTag:f6dfddfd-b28a-44da-9503-635d3fc245ac:GLOBAL`), not by the name of the tag and tag category. As a result, a given tag must exist before the 
machine controller will attempt to associate the tag.  The URN can be retrieved from the vCenter UI by accessing the `Tag & Custom Attributes` screen and selecting the desired tag.  The tag URN will be in the URL: 
```
https://v8c-2-vcenter.ocp2.dev.cluster.com/ui/app/tags/tag/urn:vmomi:InventoryServiceTag:f6dfddfd-b28a-44da-9503-635d3fc245ac:GLOBAL/permissions
```

The below example demonstrates a machine spec which defines `tagIDs`:

```yaml
apiVersion: machine.openshift.io/v1beta1
kind: Machine
metadata:
  generateName: ci-ln-ll0lbgk-c1627-gslcw-worker-0-
  name: ci-ln-ll0lbgk-c1627-gslcw-worker-0-6nppd
spec:
  lifecycleHooks: {}
  metadata: {}
  providerID: 'vsphere://421bb8b8-e7da-6bfa-3e1b-cc6ef9945343'
  providerSpec:
    value:
      numCoresPerSocket: 4
      diskGiB: 120
      snapshot: ''
      userDataSecret:
        name: worker-user-data
      memoryMiB: 16384
      credentialsSecret:
        name: vsphere-cloud-credentials
      network:
        devices:
          - networkName: ci-vlan-1207
      metadata:
        creationTimestamp: null
      numCPUs: 4
      kind: VSphereMachineProviderSpec
      workspace:
        datacenter: IBMCloud
        datastore: /IBMCloud/datastore/vsanDatastore
        folder: /IBMCloud/vm/ci-ln-ll0lbgk-c1627-gslcw
        resourcePool: /IBMCloud/host/vcs-ci-workload/Resources/ipi-ci-clusters
        server: v8c-2-vcenter.ocp2.dev.cluster.com
      template: ci-ln-ll0lbgk-c1627-gslcw-rhcos-generated-region-generated-zone
      tagIDs:
        - 'urn:vmomi:InventoryServiceTag:8d893b87-28de-4ef0-90d2-af24f21b0f26:GLOBAL'
      apiVersion: machine.openshift.io/v1beta1
status: {}
```


`machinesets` may also be defined to attach tags to `machines` created by the `machineset`.