package main

// import (
// 	"flag"
// 	"github.com/golang/glog"
// 	osv1 "github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"
// 	"k8s.io/client-go/kubernetes/scheme"
// 	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
// 	"sigs.k8s.io/controller-runtime/pkg/client"
// 	"sigs.k8s.io/controller-runtime/pkg/client/config"
// )

// const (
// 	namespace = "openshift-cluster-api"
// )

// var (
// 	F *Framework
// )

// func init() {
// 	if err := capiv1alpha1.AddToScheme(scheme.Scheme); err != nil {
// 		glog.Fatal(err)
// 	}

// 	if err := osv1.AddToScheme(scheme.Scheme); err != nil {
// 		glog.Fatal(err)
// 	}
// 	if err := newClient(); err != nil {
// 		glog.Fatal(err)
// 	}
// }

// type Framework struct {
// 	Client client.Client
// }

// func newClient() error {
// 	// Get a config to talk to the apiserver
// 	cfg, err := config.GetConfig()
// 	if err != nil {
// 		return err
// 	}
// 	if err != nil {
// 		return err
// 	}

// 	client, err := client.New(cfg, client.Options{})
// 	if err != nil {
// 		return err
// 	}
// 	F = &Framework{
// 		Client: client,
// 	}
// 	return nil
// }

// func main() {
// 	flag.Parse()
// 	if err := runSuite(); err != nil {
// 		glog.Fatal(err)
// 	}
// }

// func runSuite() error {
// 	if err := ExpectOperatorAvailable(); err != nil {
// 		glog.Errorf("FAIL: ExpectOperatorAvailable: %v", err)
// 		return err
// 	}
// 	glog.Info("PASS: ExpectOperatorAvailable")

// 	if err := ExpectOneClusterObject(); err != nil {
// 		glog.Errorf("FAIL: ExpectOneClusterObject: %v", err)
// 		return err
// 	}
// 	glog.Info("PASS: ExpectOneClusterObject")

// 	if err := ExpectOperatorStatusConditionDone(); err != nil {
// 		glog.Errorf("FAIL: ExpectOperatorStatusConditionDone: %v", err)
// 		return err
// 	}
// 	glog.Info("PASS: ExpectOperatorStatusConditionDone")

// 	if err := ExpectAllMachinesLinkedToANode(); err != nil {
// 		glog.Errorf("FAIL: ExpectAllMachinesLinkedToANode: %v", err)
// 		return err
// 	}
// 	glog.Info("PASS: ExpectAllMachinesLinkedToANode")
// 	return nil
// }
