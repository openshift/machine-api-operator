# Overview
`third_party` contains types which are not readily vendored due to dependencies they may 
reference which are incompatible with modules/module versions on which `machine-api-operator` depends.

## Types in `third_party`

### cluster-api IPAddress/IPAddressClaim

IPAddressClaim references [Condition](https://github.com/kubernetes-sigs/cluster-api/blob/main/api/v1beta1/condition_types.go) 
which drags the v1beta1 package.  In that package, [machine_webhook.go](https://github.com/kubernetes-sigs/cluster-api/blob/main/api/v1beta1/machine_webhook.go#L45)
has a dependency on `sigs.k8s.io/controller-runtime v0.14.6` which is incompatible with `sigs.k8s.io/controller-runtime v0.15.0-alpha.0.0.20230509061743-4d3244f28eef`.  
In order to reference the IPAddress/IPAddressClaim types for static IP support, those types are copied to `third_party`.

### DeepCopy generation

To generate DeepCopy functions:

~~~sh
./hack/go-mod.sh
make generate-third-party-deepcopy
~~~