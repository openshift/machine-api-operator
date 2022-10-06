/*
Copyright 2020 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"reflect"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (c *IPPool) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(c).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-ipam-metal3-io-v1alpha1-ippool,mutating=false,failurePolicy=fail,groups=ipam.metal3.io,resources=ippools,versions=v1alpha1,name=validation.ippool.ipam.metal3.io,matchPolicy=Equivalent,sideEffects=None,admissionReviewVersions=v1;v1beta1
// +kubebuilder:webhook:verbs=create;update,path=/mutate-ipam-metal3-io-v1alpha1-ippool,mutating=true,failurePolicy=fail,groups=ipam.metal3.io,resources=ippools,versions=v1alpha1,name=default.ippool.ipam.metal3.io,matchPolicy=Equivalent,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.Defaulter = &IPPool{}
var _ webhook.Validator = &IPPool{}

func (c *IPPool) Default() {
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (c *IPPool) ValidateCreate() error {
	return c.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (c *IPPool) ValidateUpdate(old runtime.Object) error {
	allErrs := field.ErrorList{}
	oldM3ipp, ok := old.(*IPPool)
	if !ok || oldM3ipp == nil {
		return apierrors.NewInternalError(errors.New("unable to convert existing object"))
	}

	if !reflect.DeepEqual(c.Spec.NamePrefix, oldM3ipp.Spec.NamePrefix) {
		allErrs = append(allErrs,
			field.Invalid(
				field.NewPath("spec", "NamePrefix"),
				c.Spec.NamePrefix,
				"cannot be modified",
			),
		)
	}
	allocationOutOfBonds, inUseOutOfBonds := c.checkPoolBonds(oldM3ipp)
	if len(allocationOutOfBonds) != 0 {
		for _, address := range allocationOutOfBonds {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "preAllocations"),
					address,
					"is out of bonds of the pools given",
				),
			)
		}
	}
	if len(inUseOutOfBonds) != 0 {
		for _, address := range inUseOutOfBonds {
			allErrs = append(allErrs,
				field.Invalid(
					field.NewPath("spec", "pools"),
					address,
					"is in use but out of bonds of the pools given",
				),
			)
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("Metal3Data").GroupKind(), c.Name, allErrs)
}

func (c *IPPool) checkPoolBonds(old *IPPool) ([]IPAddressStr, []IPAddressStr) {
	allocationOutOfBonds := []IPAddressStr{}
	inUseOutOfBonds := []IPAddressStr{}
	for _, address := range c.Spec.PreAllocations {
		inBonds := c.isAddressInBonds(address)

		if !inBonds {
			allocationOutOfBonds = append(allocationOutOfBonds, address)
		}
	}
	for _, address := range old.Status.Allocations {
		inBonds := c.isAddressInBonds(address)

		if !inBonds {
			inUseOutOfBonds = append(inUseOutOfBonds, address)
		}
	}
	return allocationOutOfBonds, inUseOutOfBonds
}

func (c *IPPool) isAddressInBonds(address IPAddressStr) bool {
	inBonds := false
	for _, pool := range c.Spec.Pools {
		if inBonds {
			break
		}
		index := 0
		for !inBonds {
			allocatedAddress, err := GetIPAddress(pool, index)
			if err != nil {
				break
			}
			index++
			if allocatedAddress == address {
				inBonds = true
				break
			}
		}
	}
	return inBonds
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (c *IPPool) ValidateDelete() error {
	return nil
}

// No further validation for now.
func (c *IPPool) validate() error {
	var allErrs field.ErrorList

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("IPPool").GroupKind(), c.Name, allErrs)
}
