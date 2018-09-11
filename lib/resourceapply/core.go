package resourceapply

import (
	"github.com/openshift/machine-api-operator/lib/resourcemerge"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

// ApplyService applies the required serviceaccount to the cluster.
func ApplyService(client coreclientv1.ServicesGetter, required *corev1.Service) (*corev1.Service, bool, error) {
	existing, err := client.Services(required.Namespace).Get(required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := client.Services(required.Namespace).Create(required)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	resourcemerge.EnsureObjectMeta(modified, &existing.ObjectMeta, required.ObjectMeta)
	if !*modified {
		return existing, false, nil
	}

	actual, err := client.Services(required.Namespace).Update(existing)
	return actual, true, err
}
