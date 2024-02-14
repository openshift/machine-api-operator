# Overview
`third_party` contains types which are not readily vendored due to dependencies they may 
reference which are incompatible with modules/module versions on which `machine-api-operator` depends.

## Types in `third_party`

### DeepCopy generation

To generate DeepCopy functions:

~~~sh
./hack/go-mod.sh
make generate-third-party-deepcopy
~~~