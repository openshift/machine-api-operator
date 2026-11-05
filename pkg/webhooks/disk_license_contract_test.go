package webhooks

import (
	"encoding/json"
	"strings"
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	"k8s.io/apimachinery/pkg/util/validation/field"
)

func TestDiskLicenseAPIContract(t *testing.T) {
	t.Parallel()
	relative := "projects/my-project/global/licenses/my-license"
	boundary := "projects/" + strings.Repeat("a.", 80) + "aaaaa:p/global/licenses/" + strings.Repeat("l", 63)
	if len(boundary) != 256 {
		t.Fatalf("boundary length = %d", len(boundary))
	}
	eight := []string{relative, relative, relative, relative, relative, relative, relative, relative}
	omittedJSON, err := json.Marshal(machinev1.GCPDisk{Licenses: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var omittedDisk machinev1.GCPDisk
	if err := json.Unmarshal(omittedJSON, &omittedDisk); err != nil {
		t.Fatal(err)
	}
	testCases := []struct {
		name     string
		licenses []string
		valid    bool
	}{
		{name: "omitted", licenses: nil, valid: true},
		{name: "explicitly empty", licenses: []string{}},
		{name: "empty slice omitted by JSON", licenses: omittedDisk.Licenses, valid: true},
		{name: "relative", licenses: []string{relative}, valid: true},
		{name: "numeric project", licenses: []string{"projects/123456789012/global/licenses/license"}, valid: true},
		{name: "single digit project", licenses: []string{"projects/1/global/licenses/a"}, valid: true},
		{name: "single letter project", licenses: []string{"projects/a/global/licenses/a"}, valid: true},
		{name: "domain scoped", licenses: []string{"projects/example.com:my-project/global/licenses/license"}, valid: true},
		{name: "prefix scoped numeric", licenses: []string{"projects/example:123/global/licenses/license"}, valid: true},
		{name: "numeric domain label", licenses: []string{"projects/123.example.com:123/global/licenses/license"}, valid: true},
		{name: "63 character project", licenses: []string{"projects/" + strings.Repeat("p", 63) + "/global/licenses/license"}, valid: true},
		{name: "63 character license", licenses: []string{"projects/p/global/licenses/" + strings.Repeat("l", 63)}, valid: true},
		{name: "256 character reference", licenses: []string{boundary}, valid: true},
		{name: "257 character reference", licenses: []string{strings.Replace(boundary, ":p/", "a:p/", 1)}},
		{name: "eight", licenses: eight, valid: true},
		{name: "nine", licenses: append(append([]string{}, eight...), relative)},
		{name: "empty item", licenses: []string{""}},
		{name: "full URL", licenses: []string{"https://www.googleapis.com/compute/v1/" + relative}},
		{name: "alternate full URL", licenses: []string{"https://compute.googleapis.com/compute/v1/" + relative}},
		{name: "query", licenses: []string{relative + "?key=value"}},
		{name: "fragment", licenses: []string{relative + "#fragment"}},
		{name: "leading whitespace", licenses: []string{" " + relative}},
		{name: "trailing whitespace", licenses: []string{relative + " "}},
		{name: "newline", licenses: []string{relative + "\n"}},
		{name: "embedded whitespace", licenses: []string{"projects/my project/global/licenses/license"}},
		{name: "uppercase project", licenses: []string{"projects/Project/global/licenses/license"}},
		{name: "uppercase license", licenses: []string{"projects/project/global/licenses/License"}},
		{name: "leading project hyphen", licenses: []string{"projects/-project/global/licenses/license"}},
		{name: "trailing project hyphen", licenses: []string{"projects/project-/global/licenses/license"}},
		{name: "64 character project", licenses: []string{"projects/" + strings.Repeat("p", 64) + "/global/licenses/license"}},
		{name: "64 character license", licenses: []string{"projects/p/global/licenses/" + strings.Repeat("l", 64)}},
		{name: "digit license", licenses: []string{"projects/p/global/licenses/1license"}},
		{name: "trailing license hyphen", licenses: []string{relative + "-"}},
		{name: "empty domain label", licenses: []string{"projects/example..com:project/global/licenses/license"}},
		{name: "numeric final domain label", licenses: []string{"projects/example.123:project/global/licenses/license"}},
		{name: "uppercase domain", licenses: []string{"projects/Example.com:project/global/licenses/license"}},
		{name: "64 character domain label", licenses: []string{"projects/" + strings.Repeat("a", 64) + ".com:p/global/licenses/license"}},
		{name: "empty project", licenses: []string{"projects//global/licenses/license"}},
		{name: "empty license", licenses: []string{"projects/p/global/licenses/"}},
		{name: "extra path", licenses: []string{relative + "/extra"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			errs := validateGCPDisks([]*machinev1.GCPDisk{{Licenses: testCase.licenses}}, field.NewPath("providerSpec", "disks"))
			if (len(errs) == 0) != testCase.valid {
				t.Fatalf("valid = %t, errors = %v", testCase.valid, errs)
			}
		})
	}
}
