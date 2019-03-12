package util

import (
	"io/ioutil"
	"os"
	"testing"
)

type expectedNamespace struct {
	namespace string
	error     bool
}

func createNamespaceFile(t *testing.T, content []byte) string {
	tmpfile, err := ioutil.TempFile("", "namespace")
	if err != nil {
		t.Error(err)
	}

	if _, err := tmpfile.Write(content); err != nil {
		t.Error(err)
	}
	if err := tmpfile.Close(); err != nil {
		t.Error(err)
	}
	return tmpfile.Name()
}

func TestGetNamespace(t *testing.T) {
	nsFile := createNamespaceFile(t, []byte("default"))
	defer os.Remove(nsFile)

	testsCases := []struct {
		name          string
		namespaceFile string
		expected      expectedNamespace
	}{
		{
			name:          "namespace file exists",
			namespaceFile: nsFile,
			expected: expectedNamespace{
				namespace: "default",
				error:     false,
			},
		},
		{
			name:          "namespace file does not exist",
			namespaceFile: "notexist",
			expected: expectedNamespace{
				namespace: "",
				error:     true,
			},
		},
	}

	for _, tc := range testsCases {
		ns, err := GetNamespace(tc.namespaceFile)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected %s error, got: %v", tc.name, errorExpectation, err)
		}

		if tc.expected.namespace != ns {
			t.Errorf("Test case: %s. Expected %s namespace, got: %s", tc.name, tc.expected.namespace, ns)
		}
	}
}
