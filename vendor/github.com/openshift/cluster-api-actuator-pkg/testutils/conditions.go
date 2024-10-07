/*
Copyright 2022 Red Hat, Inc.

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

package testutils

import (
	"errors"
	"fmt"
	"strings"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// errActualTypeMismatchCondition is used when the type of the actual object does not match the expected type of Condition.
var errActualTypeMismatchCondition = errors.New("actual should be of type Condition")

// MatchConditions returns a custom matcher to check equality of a slice of metav1.Condtion.
func MatchConditions(expected []metav1.Condition) types.GomegaMatcher {
	return &matchConditions{
		expected: expected,
	}
}

type matchConditions struct {
	expected []metav1.Condition
}

// Match checks for equality between the actual and expected objects.
func (m matchConditions) Match(actual interface{}) (success bool, err error) {
	elems := []interface{}{}
	for _, condition := range m.expected {
		elems = append(elems, MatchCondition(condition))
	}

	ok, err := gomega.ConsistOf(elems).Match(actual)
	if !ok {
		return false, wrap(err, "conditions did not match")
	}

	return true, nil
}

// FailureMessage is the message returned to the test when the actual and expected
// objects do not match.
func (m matchConditions) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto match\n\t%#v\n", actual, m.expected)
}

// NegatedFailureMessage is the negated version of the FailureMessage.
func (m matchConditions) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto not match\n\t%#v\n", actual, m.expected)
}

// MatchCondition returns a custom matcher to check equality of metav1.Condition.
func MatchCondition(expected metav1.Condition) types.GomegaMatcher {
	return &matchCondition{
		expected: expected,
	}
}

type matchCondition struct {
	expected metav1.Condition
}

// Match checks for equality between the actual and expected objects.
//
//nolint:dupl
func (m matchCondition) Match(actual interface{}) (success bool, err error) {
	actualCondition, ok := actual.(metav1.Condition)
	if !ok {
		return false, errActualTypeMismatchCondition
	}

	ok, err = gomega.Equal(m.expected.Type).Match(actualCondition.Type)
	if !ok {
		return false, wrap(err, "condition type does not match")
	}

	ok, err = gomega.Equal(m.expected.Status).Match(actualCondition.Status)
	if !ok {
		return false, wrap(err, "condition status does not match")
	}

	ok, err = gomega.Equal(m.expected.Reason).Match(actualCondition.Reason)
	if !ok {
		return false, wrap(err, "condition reason does not match")
	}

	ok, err = gomega.Equal(m.expected.Message).Match(actualCondition.Message)
	if !ok {
		return false, wrap(err, "condition message does not match")
	}

	return true, nil
}

// FailureMessage is the message returned to the test when the actual and expected
// objects do not match.
func (m matchCondition) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto match\n\t%#v\n", actual, m.expected)
}

// NegatedFailureMessage is the negated version of the FailureMessage.
func (m matchCondition) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto not match\n\t%#v\n", actual, m.expected)
}

// errActualTypeMismatchClusterOperatorStatusCondition is used when the type of the actual object does not match
// the expected type of ClusterOperatorStatusCondition.
var errActualTypeMismatchClusterOperatorStatusCondition = errors.New("actual should be of type ClusterOperatorStatusCondition")

// MatchClusterOperatorStatusConditions returns a custom matcher to check equality of configv1.ClusterOperatorStatusConditions.
func MatchClusterOperatorStatusConditions(expected []configv1.ClusterOperatorStatusCondition) types.GomegaMatcher {
	return &matchClusterOperatorConditions{
		expected: expected,
	}
}

type matchClusterOperatorConditions struct {
	expected []configv1.ClusterOperatorStatusCondition
}

// Match checks for equality between the actual and expected objects.
func (m matchClusterOperatorConditions) Match(actual interface{}) (success bool, err error) {
	elems := []interface{}{}
	for _, condition := range m.expected {
		elems = append(elems, MatchClusterOperatorStatusCondition(condition))
	}

	ok, err := gomega.ConsistOf(elems).Match(actual)
	if !ok {
		return false, wrap(err, "conditions did not match")
	}

	return true, nil
}

// FailureMessage is the message returned to the test when the actual and expected
// objects do not match.
func (m matchClusterOperatorConditions) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto match\n\t%#v\n", actual, m.expected)
}

// NegatedFailureMessage is the negated version of the FailureMessage.
func (m matchClusterOperatorConditions) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto not match\n\t%#v\n", actual, m.expected)
}

// MatchClusterOperatorStatusCondition returns a custom matcher to check equality of configv1.ClusterOperatorStatusCondition.
func MatchClusterOperatorStatusCondition(expected configv1.ClusterOperatorStatusCondition) types.GomegaMatcher {
	return &matchClusterOperatorCondition{
		expected: expected,
	}
}

type matchClusterOperatorCondition struct {
	expected configv1.ClusterOperatorStatusCondition
}

// Match checks for equality between the actual and expected objects.
//
//nolint:dupl
func (m matchClusterOperatorCondition) Match(actual interface{}) (success bool, err error) {
	actualCondition, ok := actual.(configv1.ClusterOperatorStatusCondition)
	if !ok {
		return false, errActualTypeMismatchClusterOperatorStatusCondition
	}

	ok, err = gomega.Equal(m.expected.Type).Match(actualCondition.Type)
	if !ok {
		return false, wrap(err, "condition type does not match")
	}

	ok, err = gomega.Equal(m.expected.Status).Match(actualCondition.Status)
	if !ok {
		return false, wrap(err, "condition status does not match")
	}

	ok, err = gomega.Equal(m.expected.Reason).Match(actualCondition.Reason)
	if !ok {
		return false, wrap(err, "condition reason does not match")
	}

	ok, err = gomega.Equal(m.expected.Message).Match(actualCondition.Message)
	if !ok {
		return false, wrap(err, "condition message does not match")
	}

	return true, nil
}

// FailureMessage is the message returned to the test when the actual and expected
// objects do not match.
func (m matchClusterOperatorCondition) FailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto match\n\t%#v\n", actual, m.expected)
}

// NegatedFailureMessage is the negated version of the FailureMessage.
func (m matchClusterOperatorCondition) NegatedFailureMessage(actual interface{}) (message string) {
	return fmt.Sprintf("expected\n\t%#v\nto not match\n\t%#v\n", actual, m.expected)
}

// wrap wraps an error with the given message if the error isn't nil.
func wrap(err error, msg string) error {
	if err == nil {
		return nil
	}

	if !strings.HasSuffix(msg, "%w") {
		msg = fmt.Sprintf("%s: %%w", msg)
	}

	// We are expecting the passed messages to wrap the error, so skip linting.
	return fmt.Errorf(msg, err) //nolint:goerr113
}
