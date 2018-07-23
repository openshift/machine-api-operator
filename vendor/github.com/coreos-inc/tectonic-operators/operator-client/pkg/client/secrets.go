package client

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
)

// CreateSecret will create the given Secret resource.
func (c *Client) CreateSecret(namespace string, secret *v1.Secret) (*v1.Secret, error) {
	glog.V(4).Infof("[CREATE Secret]: %s", secret.GetName())
	return c.CoreV1().Secrets(namespace).Create(secret)
}

// GetSecret will return the Secret resource for the given name.
func (c *Client) GetSecret(namespace, name string) (*v1.Secret, error) {
	glog.V(4).Infof("[GET Secret]: %s", name)
	return c.CoreV1().Secrets(namespace).Get(name, metav1.GetOptions{})
}

// UpdateSecret will update the given Secret resource.
func (c *Client) UpdateSecret(secret *v1.Secret) (*v1.Secret, error) {
	glog.V(4).Infof("[UPDATE Secret]: %s", secret.GetName())
	oldSecret, err := c.GetSecret(secret.GetNamespace(), secret.GetName())
	if err != nil {
		return nil, err
	}
	patchBytes, err := createPatch(oldSecret, secret)
	if err != nil {
		return nil, fmt.Errorf("error creating patch for Secret: %v", err)
	}
	return c.CoreV1().Secrets(secret.GetNamespace()).Patch(secret.GetName(), types.StrategicMergePatchType, patchBytes)
}

// AtomicUpdateSecret takes an update function which is executed before attempting
// to update the Secret resource. Upon conflict, the update function is run
// again, until the update is successful or a non-conflict error is returned.
func (c *Client) AtomicUpdateSecret(namespace, name string, f optypes.SecretModifier) (*v1.Secret, error) {
	glog.V(4).Infof("[ATOMIC UPDATE Secret]: %s", name)
	var newSecret *v1.Secret
	err := wait.ExponentialBackoff(wait.Backoff{
		Duration: time.Second,
		Factor:   2.0,
		Jitter:   0.1,
		Steps:    5,
	}, func() (bool, error) {
		secret, err := c.GetSecret(namespace, name)
		if err != nil {
			return false, err
		}
		if err = f(secret); err != nil {
			return false, err
		}
		newSecret, err = c.UpdateSecret(secret)
		if err != nil {
			if errors.IsConflict(err) {
				glog.Warningf("conflict updating Secret resource, will try again: %v", err)
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return newSecret, err
}

// ListSecretsWithLabels list the Secrets which contain the given labels.
// An empty list will be returned if no such Secrets are found.
func (c *Client) ListSecretsWithLabels(namespace string, labels labels.Set) (*v1.SecretList, error) {
	glog.V(4).Infof("[LIST Secrets] with labels: %s", labels)
	opts := metav1.ListOptions{LabelSelector: labels.String()}
	return c.CoreV1().Secrets(namespace).List(opts)
}

// DeleteSecret deletes the Secret with the given name.
func (c *Client) DeleteSecret(namespace, name string, options *metav1.DeleteOptions) error {
	glog.V(4).Infof("DELETE Secret]: %s", name)
	return c.CoreV1().Secrets(namespace).Delete(name, options)
}
