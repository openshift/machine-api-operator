#!/usr/bin/env bash

set -eu

echo "Building controller-gen tool..."
# github.com/openshift/machine-api-operator/vendor/
go build -o bin/controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen

dir=$(mktemp -d -t XXXXXXXX)
echo $dir
mkdir -p $dir/src/github.com/openshift/machine-api-operator/pkg/apis
mkdir -p $dir/src/github.com/openshift/machine-api-operator/vendor

cp -r pkg/apis/healthchecking $dir/src/github.com/openshift/machine-api-operator/pkg/apis/.
cp -r PROJECT $dir/src/github.com/openshift/machine-api-operator/.
cp -r vendor/github.com/openshift/cluster-api/pkg/apis/machine $dir/src/github.com/openshift/machine-api-operator/pkg/apis
# Some dependencies need to be coppied as well. Othwerwise, controller-gen will complain about non-existing kind Unsupported
cp -r vendor/k8s.io $dir/src/github.com/openshift/machine-api-operator/vendor/.
cp -r vendor/github.com $dir/src/github.com/openshift/machine-api-operator/vendor/.

cwd=$(pwd)
pushd $dir/src/github.com/openshift/machine-api-operator
GOPATH=$dir ${cwd}/bin/controller-gen crd --domain openshift.io --skip-map-validation=false
popd

echo "Coping generated CRDs"
cp $dir/src/github.com/openshift/machine-api-operator/config/crds/healthchecking_v1alpha1_machinehealthcheck.yaml install/0000_30_machine-api-operator_07_machinehealthcheck.crd.yaml
cp $dir/src/github.com/openshift/machine-api-operator/config/crds/machine_v1beta1_machineset.yaml install/0000_30_machine-api-operator_03_machineset.crd.yaml
cp $dir/src/github.com/openshift/machine-api-operator/config/crds/machine_v1beta1_machine.yaml install/0000_30_machine-api-operator_02_machine.crd.yaml

rm -rf $dir
