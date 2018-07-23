package client

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	pdbDeletePollInterval = 3 * time.Second
	pdbDeletePollTimeout  = 1 * time.Minute
)

// CreatePodDisruptionBudget creates the PodDisruptionBudget.
func (c *Client) CreatePodDisruptionBudget(pdb *policyv1beta1.PodDisruptionBudget) (*policyv1beta1.PodDisruptionBudget, error) {
	return c.PolicyV1beta1().PodDisruptionBudgets(pdb.GetNamespace()).Create(pdb)
}

// GetPodDisruptionBudget returns the existing PodDisruptionBudget.
func (c *Client) GetPodDisruptionBudget(namespace, name string) (*policyv1beta1.PodDisruptionBudget, error) {
	return c.PolicyV1beta1().PodDisruptionBudgets(namespace).Get(name, metav1.GetOptions{})
}

// DeletePodDisruptionBudget deletes the PodDisruptionBudget.
func (c *Client) DeletePodDisruptionBudget(namespace, name string, options *metav1.DeleteOptions) error {
	return c.PolicyV1beta1().PodDisruptionBudgets(namespace).Delete(name, options)
}

// UpdatePodDisruptionBudget will update the given PodDisruptionBudget resource.
//
// Note that since updating the 'Spec' field is forbidden, so we have to do a delete and create
// instead.
func (c *Client) UpdatePodDisruptionBudget(pdb *policyv1beta1.PodDisruptionBudget) (*policyv1beta1.PodDisruptionBudget, error) {
	glog.V(4).Infof("[UPDATE PodDisruptionBudget]: %s", pdb.GetName())
	oldPdb, err := c.GetPodDisruptionBudget(pdb.GetNamespace(), pdb.GetName())
	if err != nil {
		return nil, err
	}

	if equality.Semantic.DeepEqual(oldPdb.Spec, pdb.Spec) {
		return oldPdb, nil
	}

	delPol := metav1.DeletePropagationForeground
	delOpt := metav1.DeleteOptions{PropagationPolicy: &delPol}

	if err := c.DeletePodDisruptionBudget(pdb.GetNamespace(), pdb.GetName(), &delOpt); err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("delete pod disruption budget: %v", err)
	}

	// Wait for the PodDisruptionBudget to be deleted in a retry loop because sometimes the foreground
	// delete doesn't actually delete in the foreground.
	if err := wait.PollImmediate(pdbDeletePollInterval, pdbDeletePollTimeout, func() (bool, error) {
		if _, err := c.GetPodDisruptionBudget(pdb.GetNamespace(), pdb.GetName()); err != nil {
			if apierrors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}); err != nil {
		return nil, fmt.Errorf("wait for pod disruption budget to delete: %v", err)
	}

	// Make sure the ResourceVersion is not set or the Create operation will fail.
	pdb.ObjectMeta.ResourceVersion = ""

	// Re-create the PodDisruptionBudget.
	updated, err := c.CreatePodDisruptionBudget(pdb)
	if err != nil {
		return nil, fmt.Errorf("create pod disruption budget: %v", err)
	}

	return updated, nil
}
