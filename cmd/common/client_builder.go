package common

import (
	"github.com/golang/glog"
	cvoclientset "github.com/openshift/cluster-version-operator/pkg/generated/clientset/versioned"
	apiext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	apiregistrationclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	clusterapiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

// ClientBuilder can create a variety of kubernetes client interface
// with its embeded rest.Config.
type ClientBuilder struct {
	config *rest.Config
}

// ClientOrDie returns the kubernetes client interface for machine config.
func (cb *ClientBuilder) ClusterAPIClientOrDie(name string) clusterapiclientset.Interface {
	return clusterapiclientset.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// ClientOrDie returns the kubernetes client interface for machine config.
func (cb *ClientBuilder) APIRegistrationClientOrDie(name string) apiregistrationclientset.Interface {
	return apiregistrationclientset.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// ClientOrDie returns the kubernetes client interface for general kubernetes objects.
func (cb *ClientBuilder) KubeClientOrDie(name string) kubernetes.Interface {
	return kubernetes.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// ClientOrDie returns the kubernetes client interface for extended kubernetes objects.
func (cb *ClientBuilder) APIExtClientOrDie(name string) apiext.Interface {
	return apiext.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// ClusterversionClientOrDie returns the kubernetes client interface for cluster version objects.
// TODO(yifan): Just return the client for the Operator Status objects.
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
