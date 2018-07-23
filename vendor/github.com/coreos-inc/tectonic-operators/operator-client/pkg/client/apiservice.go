package client

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
)

// CreateAPIService creates the APIService.
func (c *Client) CreateAPIService(svc *apiregistrationv1beta1.APIService) (*apiregistrationv1beta1.APIService, error) {
	return c.aggInterface.ApiregistrationV1beta1().APIServices().Create(svc)
}

// GetAPIService returns the existing APIService.
func (c *Client) GetAPIService(name string) (*apiregistrationv1beta1.APIService, error) {
	return c.aggInterface.ApiregistrationV1beta1().APIServices().Get(name, metav1.GetOptions{})
}

// DeleteAPIService deletes the APIService.
func (c *Client) DeleteAPIService(name string, options *metav1.DeleteOptions) error {
	return c.aggInterface.ApiregistrationV1beta1().APIServices().Delete(name, options)
}

// UpdateAPIService will update the given APIService resource.
func (c *Client) UpdateAPIService(svc *apiregistrationv1beta1.APIService) (*apiregistrationv1beta1.APIService, error) {
	glog.V(4).Infof("[UPDATE APIService]: %s", svc.GetName())
	current, err := c.GetAPIService(svc.GetName())
	if err != nil {
		return nil, err
	}
	original, modified := current, svc

	patchBytes, err := createThreeWayMergePatchPreservingCommands(original, modified, current)
	if err != nil {
		return nil, err
	}

	return c.aggInterface.ApiregistrationV1beta1().APIServices().Patch(svc.GetName(), types.StrategicMergePatchType, patchBytes)
}
