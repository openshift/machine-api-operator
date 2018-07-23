package client

import (
	"fmt"

	"github.com/golang/glog"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// CreatePodSecurityPolicy creates the PodSecurityPolicy.
func (c *Client) CreatePodSecurityPolicy(psp *extensionsv1beta1.PodSecurityPolicy) (*extensionsv1beta1.PodSecurityPolicy, error) {
	return c.ExtensionsV1beta1().PodSecurityPolicies().Create(psp)
}

// GetPodSecurityPolicy returns the existing PodSecurityPolicy.
func (c *Client) GetPodSecurityPolicy(name string) (*extensionsv1beta1.PodSecurityPolicy, error) {
	return c.ExtensionsV1beta1().PodSecurityPolicies().Get(name, metav1.GetOptions{})
}

// DeletePodSecurityPolicy deletes the PodSecurityPolicy.
func (c *Client) DeletePodSecurityPolicy(name string, options *metav1.DeleteOptions) error {
	return c.ExtensionsV1beta1().PodSecurityPolicies().Delete(name, options)
}

// UpdatePodSecurityPolicy will update the given PodSecurityPolicy resource.
func (c *Client) UpdatePodSecurityPolicy(psp *extensionsv1beta1.PodSecurityPolicy) (*extensionsv1beta1.PodSecurityPolicy, error) {
	glog.V(4).Infof("[UPDATE PodSecurityPolicy]: %s", psp.GetName())
	oldPsp, err := c.GetPodSecurityPolicy(psp.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldPsp, psp)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for PodSecurityPolicy: %v", err)
	}
	return c.ExtensionsV1beta1().PodSecurityPolicies().Patch(psp.GetName(), types.StrategicMergePatchType, patchBytes)
}
