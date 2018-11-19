package main

import (
	"flag"
	"github.com/golang/glog"
	"k8s.io/client-go/kubernetes/scheme"
	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	namespace = "openshift-cluster-api"
)

var (
	F *Framework
)

func init() {
	if err := capiv1alpha1.AddToScheme(scheme.Scheme); err != nil {
		glog.Fatal(err)
	}

	if err := newClient(); err != nil {
		glog.Fatal(err)
	}
}

type Framework struct {
	Client client.Client
}

func newClient() error {
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	// TODO:(alberto) add schemes
	client, err := client.New(cfg, client.Options{})
	if err != nil {
		return err
	}
	F = &Framework{
		Client: client,
	}
	return nil
}

func main() {
	flag.Parse()
	if err := runSuite(); err != nil {
		glog.Fatal(err)
	}
}

func runSuite() error {
	if err := ExpectOperatorAvailable(); err != nil {
		glog.Errorf("FAIL: ExpectOperatorAvailable: %v", err)
		return err
	}
	glog.Info("PASS: ExpectOperatorAvailable")

	if err := ExpectOneClusterObject(); err != nil {
		glog.Errorf("FAIL: ExpectOneClusterObject: %v", err)
		return err
	}
	glog.Info("PASS: ExpectOneClusterObject")
	return nil
}
