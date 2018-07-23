package client

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"

	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
)

const (
	// Timeouts
	podEvictionTimeout = 15 * time.Second

	// Polling durations
	cordonPollDuration       = 20 * time.Millisecond
	evictPollDuration        = time.Second
	atomicNodeUpdateDuration = time.Second
)

// ListNodes returns a list of Nodes.
func (c *Client) ListNodes(lo metav1.ListOptions) (*v1.NodeList, error) {
	glog.V(4).Info("[GET Node list]")
	return c.CoreV1().Nodes().List(lo)
}

// GetNode will return the Node object specified by the given name.
func (c *Client) GetNode(name string) (*v1.Node, error) {
	glog.V(4).Infof("[GET Node]: %s", name)
	return c.CoreV1().Nodes().Get(name, metav1.GetOptions{})
}

// UpdateNode will update the Node object given.
func (c *Client) UpdateNode(node *v1.Node) (*v1.Node, error) {
	glog.V(4).Infof("[UPDATE Node]: %s", node.GetName())
	oldNode, err := c.CoreV1().Nodes().Get(node.GetName(), metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("error getting existing Node %s for patch: %v", node.GetName(), err)
	}
	patchBytes, err := createPatch(oldNode, node)
	if err != nil {
		return nil, fmt.Errorf("error creating patch: %v", err)
	}
	return c.CoreV1().Nodes().Patch(node.GetName(), types.StrategicMergePatchType, patchBytes)
}

// AtomicUpdateNode will continue to apply the update function to the
// Node object referenced by the given name until the update
// succeeds without conflict or returns an error.
func (c *Client) AtomicUpdateNode(name string, f optypes.NodeModifier) (*v1.Node, error) {
	var n *v1.Node
	err := wait.PollInfinite(atomicNodeUpdateDuration, func() (bool, error) {
		var err error
		n, err = c.GetNode(name)
		if err != nil {
			return false, err
		}
		if err = f(n); err != nil {
			return false, err
		}
		n, err = c.UpdateNode(n)
		if err != nil {
			if apierrors.IsConflict(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return n, nil
}
