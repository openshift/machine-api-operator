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

package windows

import (
	"testing"
)

func TestAddPowershellTags(t *testing.T) {
	testcases := []struct {
		name     string
		target   string
		expected string
	}{
		{
			name:     "String is wrapped properly",
			target:   "some stuff",
			expected: "<powershell>some stuff</powershell>\n<persist>true</persist>",
		},
		{
			name:     "String is already wrapped, does not wrap a second time",
			target:   "<powershell>some stuff</powershell>\n<persist>true</persist>",
			expected: "<powershell>some stuff</powershell>\n<persist>true</persist>",
		},
		{
			name:     "String has open tag but no close, does wrap",
			target:   "<powershell>some stuff",
			expected: "<powershell><powershell>some stuff</powershell>\n<persist>true</persist>",
		},
		{
			name:     "String has close tag but no open, does wrap",
			target:   "some stuff</powershell>",
			expected: "<powershell>some stuff</powershell></powershell>\n<persist>true</persist>",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			observed := AddPowershellTags(tc.target)
			if tc.expected != observed {
				t.Errorf("observed: \"%v\", expected: \"%v\"", observed, tc.expected)
			}
		})
	}
}

func TestHasPowershellTags(t *testing.T) {
	testcases := []struct {
		name     string
		target   string
		expected bool
	}{
		{
			name:     "String has both tags",
			target:   "<powershell>some stuff</powershell>\n<persist>true</persist>",
			expected: true,
		},
		{
			name:     "String has open tag only",
			target:   "<powershell>some stuff",
			expected: false,
		},
		{
			name:     "String has close tag only",
			target:   "some stuff</powershell>\n<persist>true</persist>",
			expected: false,
		},
		{
			name:     "String has no tags",
			target:   "some stuff",
			expected: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			observed := HasPowershellTags(tc.target)
			if tc.expected != observed {
				t.Errorf("observed: %v, expected: %v", observed, tc.expected)
			}
		})
	}
}

func TestRemovePowershellTags(t *testing.T) {
	testcases := []struct {
		name     string
		target   string
		expected string
	}{
		{
			name:     "String has no tags",
			target:   "some stuff",
			expected: "some stuff",
		},
		{
			name:     "String has both tags",
			target:   "<powershell>some stuff</powershell>\n<persist>true</persist>",
			expected: "some stuff",
		},
		{
			name:     "String has open tag only, does not remove",
			target:   "<powershell>some stuff",
			expected: "<powershell>some stuff",
		},
		{
			name:     "String has close tag only, does not remove",
			target:   "some stuff</powershell>\n<persist>true</persist>",
			expected: "some stuff</powershell>\n<persist>true</persist>",
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			observed := RemovePowershellTags(tc.target)
			if tc.expected != observed {
				t.Errorf("observed: %v, expected: %v", observed, tc.expected)
			}
		})
	}
}
