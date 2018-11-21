package common

import (
	"github.com/golang/glog"
	cvoclientset "github.com/openshift/cluster-version-operator/pkg/generated/clientset/versioned"
	apiext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// ClientBuilder can create a variety of kubernetes client interface
// with its embeded rest.Config.
type ClientBuilder struct {
	config *rest.Config
}

// KubeClientOrDie returns the kubernetes client interface for general kubernetes objects.
func (cb *ClientBuilder) KubeClientOrDie(name string) kubernetes.Interface {
	return kubernetes.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// APIExtClientOrDie returns the kubernetes client interface for extended kubernetes objects.
func (cb *ClientBuilder) APIExtClientOrDie(name string) apiext.Interface {
	return apiext.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// ClusterversionClientOrDie returns the kubernetes client interface for cluster version objects.
// TODO(alberto): Return the client for the Cluster Operator Status objects under config.io.
func (cb *ClientBuilder) ClusterversionClientOrDie(name string) cvoclientset.Interface {
	return cvoclientset.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// NewClientBuilder returns a *ClientBuilder with the given kubeconfig.
func NewClientBuilder(kubeconfig string) (*ClientBuilder, error) {
	var config *rest.Config
	var err error

	if kubeconfig != "" {
		glog.V(4).Infof("Loading kube client config from path %q", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		glog.V(4).Infof("Using in-cluster kube client config")
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	return &ClientBuilder{
		config: config,
	}, nil
}
