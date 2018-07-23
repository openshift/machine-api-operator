package client

import (
	"github.com/golang/glog"
	netv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateNetworkPolicy creates the NetworkPolicy.
func (c *Client) CreateNetworkPolicy(np *netv1.NetworkPolicy) (*netv1.NetworkPolicy, error) {
	return c.NetworkingV1().NetworkPolicies(np.GetNamespace()).Create(np)
}

// GetNetworkPolicy retrieves an existing NetworkPolicy.
func (c *Client) GetNetworkPolicy(namespace, name string) (*netv1.NetworkPolicy, error) {
	return c.NetworkingV1().NetworkPolicies(namespace).Get(name, metav1.GetOptions{})
}

// UpdateNetworkPolicy replaces an existing NeworkPolicy object.
func (c *Client) UpdateNetworkPolicy(modified *netv1.NetworkPolicy) (*netv1.NetworkPolicy, error) {
	namespace, name := modified.GetNamespace(), modified.GetName()
	glog.V(4).Infof("[UPDATE NetworkPolicy]: %s:%s", namespace, name)
	return c.NetworkingV1().NetworkPolicies(namespace).Update(modified)
}

// DeleteNetworkPolicy deletes a NetworkPolicy.
func (c *Client) DeleteNetworkPolicy(namespace, name string, options *metav1.DeleteOptions) error {
	return c.NetworkingV1().NetworkPolicies(namespace).Delete(name, options)
}
