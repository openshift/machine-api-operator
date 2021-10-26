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

package conditions

import (
	"errors"
	"testing"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
	machinev1 "github.com/openshift/api/machine/v1beta1"
)

var (
	nil1          *machinev1.Condition
	true1         = TrueCondition("true1")
	unknown1      = UnknownCondition("unknown1", "reason unknown1", "message unknown1")
	falseInfo1    = FalseCondition("falseInfo1", "reason falseInfo1", machinev1.ConditionSeverityInfo, "message falseInfo1")
	falseWarning1 = FalseCondition("falseWarning1", "reason falseWarning1", machinev1.ConditionSeverityWarning, "message falseWarning1")
	falseError1   = FalseCondition("falseError1", "reason falseError1", machinev1.ConditionSeverityError, "message falseError1")
)

func TestGet(t *testing.T) {
	g := NewWithT(t)

	mhc := &machinev1.MachineHealthCheck{}
	g.Expect(Get(mhc, "conditionBaz")).To(BeNil())

	mhc.Status.Conditions = conditionList(TrueCondition("conditionBaz"))
	g.Expect(Get(mhc, "conditionBaz")).To(haveSameStateOf(TrueCondition("conditionBaz")))
}

func conditionList(conditions ...*machinev1.Condition) machinev1.Conditions {
	cs := machinev1.Conditions{}
	for _, x := range conditions {
		if x != nil {
			cs = append(cs, *x)
		}
	}
	return cs
}

func haveSameStateOf(expected *machinev1.Condition) types.GomegaMatcher {
	return &ConditionMatcher{
		Expected: expected,
	}
}

type ConditionMatcher struct {
	Expected *machinev1.Condition
}

func (matcher *ConditionMatcher) Match(actual interface{}) (success bool, err error) {
	actualCondition, ok := actual.(*machinev1.Condition)
	if !ok {
		return false, errors.New("Value should be a condition")
	}

	return hasSameState(actualCondition, matcher.Expected), nil
}

func (matcher *ConditionMatcher) FailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "to have the same state of", matcher.Expected)
}
func (matcher *ConditionMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return format.Message(actual, "not to have the same state of", matcher.Expected)
}
