package resourceread

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	clusterv1alpha "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

var (
	clusterAPIScheme = runtime.NewScheme()
	clusterAPICodecs = serializer.NewCodecFactory(clusterAPIScheme)
)

func init() {
	if err := clusterv1alpha.AddToScheme(clusterAPIScheme); err != nil {
		panic(err)
	}
}

// ReadMachineSetV1alphaOrDie reads MachineSet object from bytes. Panics on error.
func ReadMachineSetV1alphaOrDie(objBytes []byte) *clusterv1alpha.MachineSet {
	requiredObj, err := runtime.Decode(clusterAPICodecs.UniversalDecoder(clusterv1alpha.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return requiredObj.(*clusterv1alpha.MachineSet)
}

// ReadClusterV1alphaOrDie reads Cluster object from bytes. Panics on error.
func ReadClusterV1alphaOrDie(objBytes []byte) *clusterv1alpha.Cluster {
	requiredObj, err := runtime.Decode(clusterAPICodecs.UniversalDecoder(clusterv1alpha.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return requiredObj.(*clusterv1alpha.Cluster)
}
