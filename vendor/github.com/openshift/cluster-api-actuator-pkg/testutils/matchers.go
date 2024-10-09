/*
Copyright 2024 Red Hat, Inc.

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
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"
)

// MatchViaJSON returns a custom matcher to check equality of an object by converting it to JSON.
// Converting to JSON avoids the need to implement a custom matcher for each type.
// This is useful for comparing objects that are not directly comparable (maps may have differing orders).
func MatchViaJSON(expected interface{}) types.GomegaMatcher {
	return &matchViaJSON{
		expected: expected,
	}
}

// matchViaJSON is the implementation of the MatchViaJSON matcher.
// It holds a generic expected object to compare against.
type matchViaJSON struct {
	expected interface{}
}

// Match implements the matching logic for the MatchViaJSON matcher.
// It converts the input objects to JSON and compares them.
func (m matchViaJSON) Match(actual interface{}) (success bool, err error) {
	expectedMachineJSON, err := json.Marshal(m.expected)
	if err != nil {
		return false, err
	}

	actualMachineJSON, err := json.Marshal(actual)
	if err != nil {
		return false, err
	}

	return gomega.MatchJSON(expectedMachineJSON).Match(actualMachineJSON)
}

// FailureMessage implements the failure message for the MatchViaJSON matcher.
func (m matchViaJSON) FailureMessage(actual interface{}) (message string) {
	actualString, expectedString, _ := m.prettyPrint(actual)
	return format.Message(actualString, "to match JSON of", expectedString)
}

// NegatedFailureMessage implements the negated failure message for the MatchViaJSON matcher.
func (m matchViaJSON) NegatedFailureMessage(actual interface{}) (message string) {
	actualString, expectedString, _ := m.prettyPrint(actual)
	return format.Message(actualString, "not to match JSON of", expectedString)
}

// prettyPrint formats the actual and expected objects as JSON.
// This is somewhat copied from the gomega.MatchJSON matcher so that the output looks similar.
func (m *matchViaJSON) prettyPrint(actual interface{}) (actualFormatted, expectedFormatted string, err error) {
	expectedMachineJSON, err := json.Marshal(m.expected)
	if err != nil {
		return "", "", err
	}

	actualMachineJSON, err := json.Marshal(actual)
	if err != nil {
		return "", "", err
	}

	abuf := new(bytes.Buffer)
	ebuf := new(bytes.Buffer)

	if err := json.Indent(abuf, actualMachineJSON, "", "  "); err != nil {
		// Ignore the style linter so that we are consistent with the gomega.MatchJSON matcher.
		return "", "", fmt.Errorf("Actual '%s' should be valid JSON, but it is not.\nUnderlying error:%w", string(actualMachineJSON), err) //nolint:stylecheck
	}

	if err := json.Indent(ebuf, expectedMachineJSON, "", "  "); err != nil {
		// Ignore the style linter so that we are consistent with the gomega.MatchJSON matcher.
		return "", "", fmt.Errorf("Expected '%s' should be valid JSON, but it is not.\nUnderlying error:%w", string(expectedMachineJSON), err) //nolint:stylecheck
	}

	return abuf.String(), ebuf.String(), nil
}
