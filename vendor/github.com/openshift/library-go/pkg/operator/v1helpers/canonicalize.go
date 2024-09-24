package v1helpers

import (
	"fmt"
	"slices"
	"strings"

	operatorv1 "github.com/openshift/api/operator/v1"
	"k8s.io/apimachinery/pkg/util/json"

	"k8s.io/utils/ptr"

	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"
)

// ToStaticPodOperator returns the equivalent typed kind for the applyconfiguration. Due to differences in serialization like
// omitempty on strings versus pointers, the returned values can be slightly different.  This is an expensive way to diff the
// result, but it is an effective one.
func ToStaticPodOperator(in *applyoperatorv1.StaticPodOperatorStatusApplyConfiguration) (*operatorv1.StaticPodOperatorStatus, error) {
	if in == nil {
		return nil, nil
	}
	jsonBytes, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("unable to serialize: %w", err)
	}

	ret := &operatorv1.StaticPodOperatorStatus{}
	if err := json.Unmarshal(jsonBytes, ret); err != nil {
		return nil, fmt.Errorf("unable to deserialize: %w", err)
	}

	return ret, nil
}

func CanonicalizeStaticPodOperatorStatus(obj *applyoperatorv1.StaticPodOperatorStatusApplyConfiguration) {
	if obj == nil {
		return
	}
	CanonicalizeOperatorStatus(&obj.OperatorStatusApplyConfiguration)
	slices.SortStableFunc(obj.NodeStatuses, CompareNodeStatusByNode)
}

func CanonicalizeOperatorStatus(obj *applyoperatorv1.OperatorStatusApplyConfiguration) {
	if obj == nil {
		return
	}
	slices.SortStableFunc(obj.Conditions, CompareOperatorConditionByType)
	slices.SortStableFunc(obj.Generations, CompareGenerationStatusByKeys)
}

func CompareOperatorConditionByType(a, b applyoperatorv1.OperatorConditionApplyConfiguration) int {
	return strings.Compare(ptr.Deref(a.Type, ""), ptr.Deref(b.Type, ""))
}

func CompareGenerationStatusByKeys(a, b applyoperatorv1.GenerationStatusApplyConfiguration) int {
	if c := strings.Compare(ptr.Deref(a.Group, ""), ptr.Deref(b.Group, "")); c != 0 {
		return c
	}
	if c := strings.Compare(ptr.Deref(a.Resource, ""), ptr.Deref(b.Resource, "")); c != 0 {
		return c
	}
	if c := strings.Compare(ptr.Deref(a.Namespace, ""), ptr.Deref(b.Namespace, "")); c != 0 {
		return c
	}
	if c := strings.Compare(ptr.Deref(a.Name, ""), ptr.Deref(b.Name, "")); c != 0 {
		return c
	}

	return 0
}

func CompareNodeStatusByNode(a, b applyoperatorv1.NodeStatusApplyConfiguration) int {
	return strings.Compare(ptr.Deref(a.NodeName, ""), ptr.Deref(b.NodeName, ""))
}
