/*
Copyright The Kubernetes Authors.

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

// Code generated by client-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"

	v1alpha1 "k8s.io/api/coordination/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	coordinationv1alpha1 "k8s.io/client-go/applyconfigurations/coordination/v1alpha1"
	gentype "k8s.io/client-go/gentype"
	scheme "k8s.io/client-go/kubernetes/scheme"
)

// LeaseCandidatesGetter has a method to return a LeaseCandidateInterface.
// A group's client should implement this interface.
type LeaseCandidatesGetter interface {
	LeaseCandidates(namespace string) LeaseCandidateInterface
}

// LeaseCandidateInterface has methods to work with LeaseCandidate resources.
type LeaseCandidateInterface interface {
	Create(ctx context.Context, leaseCandidate *v1alpha1.LeaseCandidate, opts v1.CreateOptions) (*v1alpha1.LeaseCandidate, error)
	Update(ctx context.Context, leaseCandidate *v1alpha1.LeaseCandidate, opts v1.UpdateOptions) (*v1alpha1.LeaseCandidate, error)
	Delete(ctx context.Context, name string, opts v1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts v1.DeleteOptions, listOpts v1.ListOptions) error
	Get(ctx context.Context, name string, opts v1.GetOptions) (*v1alpha1.LeaseCandidate, error)
	List(ctx context.Context, opts v1.ListOptions) (*v1alpha1.LeaseCandidateList, error)
	Watch(ctx context.Context, opts v1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts v1.PatchOptions, subresources ...string) (result *v1alpha1.LeaseCandidate, err error)
	Apply(ctx context.Context, leaseCandidate *coordinationv1alpha1.LeaseCandidateApplyConfiguration, opts v1.ApplyOptions) (result *v1alpha1.LeaseCandidate, err error)
	LeaseCandidateExpansion
}

// leaseCandidates implements LeaseCandidateInterface
type leaseCandidates struct {
	*gentype.ClientWithListAndApply[*v1alpha1.LeaseCandidate, *v1alpha1.LeaseCandidateList, *coordinationv1alpha1.LeaseCandidateApplyConfiguration]
}

// newLeaseCandidates returns a LeaseCandidates
func newLeaseCandidates(c *CoordinationV1alpha1Client, namespace string) *leaseCandidates {
	return &leaseCandidates{
		gentype.NewClientWithListAndApply[*v1alpha1.LeaseCandidate, *v1alpha1.LeaseCandidateList, *coordinationv1alpha1.LeaseCandidateApplyConfiguration](
			"leasecandidates",
			c.RESTClient(),
			scheme.ParameterCodec,
			namespace,
			func() *v1alpha1.LeaseCandidate { return &v1alpha1.LeaseCandidate{} },
			func() *v1alpha1.LeaseCandidateList { return &v1alpha1.LeaseCandidateList{} },
			gentype.PrefersProtobuf[*v1alpha1.LeaseCandidate](),
		),
	}
}
