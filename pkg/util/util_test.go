package util

import (
	"sort"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMergeCommaSeparatedKeyValues(t *testing.T) {
	testCases := []struct {
		lists              []string
		sortedExpectedList []string // The list of key=value pairs string in the alphabetically sorted order
		name               string
	}{
		{
			name: "with three lists and two overlapping keys",
			lists: []string{
				"foo=bar,bar=baz,io.k8s.something=else-value_1,kubernetes.io/arch=arm64",
				"bar=overwritten",
				"io.k8s.something=else-value_2",
			},
			sortedExpectedList: []string{
				"bar=overwritten",
				"foo=bar",
				"io.k8s.something=else-value_2",
				"kubernetes.io/arch=arm64",
			},
		},
		{
			name: "with three lists and overwriting the kubernetes.io/arch label too",
			lists: []string{
				"foo=bar,bar=baz,io.k8s.something=else-value_1,kubernetes.io/arch=amd64",
				"bar=overwritten",
				"io.k8s.something=else-value_2,kubernetes.io/arch=arm64",
			},
			sortedExpectedList: []string{
				"bar=overwritten",
				"foo=bar",
				"io.k8s.something=else-value_2",
				"kubernetes.io/arch=arm64",
			},
		},
		{name: "with three lists and a wrong key-value pair (missing the `=`)",
			lists: []string{
				"foo=bar,bar=baz,io.k8s.something=else-value_1,kubernetes.io/arch=amd64",
				"bar=overwritten,wrong-key-value-pair",
				"io.k8s.something=else-value_2,kubernetes.io/arch=arm64",
			},
			sortedExpectedList: []string{
				"bar=overwritten",
				"foo=bar",
				"io.k8s.something=else-value_2",
				"kubernetes.io/arch=arm64",
			},
		},
		{name: "with three lists and an empty value for the key `bar`",
			lists: []string{
				"foo=bar,bar=baz,io.k8s.something=else-value_1,kubernetes.io/arch=amd64",
				"bar=",
				"io.k8s.something=else-value_2,kubernetes.io/arch=arm64",
			},
			sortedExpectedList: []string{
				"bar=",
				"foo=bar",
				"io.k8s.something=else-value_2",
				"kubernetes.io/arch=arm64",
			},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(tt *testing.T) {
			g := NewWithT(tt)
			mergedList := strings.Split(MergeCommaSeparatedKeyValuePairs(tc.lists...), ",")
			// As we use maps, the order of key-value pairs in the final string is not guaranteed:
			// we must ensure the equality check is done against the sorted list
			sort.Strings(mergedList)
			g.Expect(mergedList).To(Equal(tc.sortedExpectedList))
		})
	}
}
