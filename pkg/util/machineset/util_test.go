package util

import (
	"maps"
	"testing"

	. "github.com/onsi/gomega"
)

func TestSettingAnnotations(t *testing.T) {
	tests := []struct {
		name                string
		value               string
		fn                  func(map[string]string, string) map[string]string
		expectedAnnotations map[string]string
		suppliedAnnotations map[string]string
	}{
		{
			name:  "set empty CpuKey annotation",
			value: "",
			fn:    SetCpuAnnotation,
			suppliedAnnotations: map[string]string{
				CpuKey: "",
			},
			expectedAnnotations: map[string]string{
				CpuKey: "",
			},
		},
		{
			name:  "replaces CpuKey annotation",
			value: "1",
			fn:    SetCpuAnnotation,
			suppliedAnnotations: map[string]string{
				CpuKey: "",
			},
			expectedAnnotations: map[string]string{
				CpuKey: "1",
			},
		},
		{
			name:                "adds CpuKey annotation",
			value:               "1",
			fn:                  SetCpuAnnotation,
			suppliedAnnotations: map[string]string{},
			expectedAnnotations: map[string]string{
				CpuKey: "1",
			},
		},
		{
			name:  "replaces MemoryKey annotation",
			value: "4Gi",
			fn:    SetMemoryAnnotation,
			suppliedAnnotations: map[string]string{
				MemoryKey: "",
			},
			expectedAnnotations: map[string]string{
				MemoryKey: "4Gi",
			},
		},
		{
			name:                "adds MemoryKey annotation",
			value:               "4Gi",
			fn:                  SetMemoryAnnotation,
			suppliedAnnotations: map[string]string{},
			expectedAnnotations: map[string]string{
				MemoryKey: "4Gi",
			},
		},
		{
			name:  "replaces GpuCount annotation",
			value: "1",
			fn:    SetGpuCountAnnotation,
			suppliedAnnotations: map[string]string{
				GpuCountKey: "",
			},
			expectedAnnotations: map[string]string{
				GpuCountKey: "1",
			},
		},
		{
			name:                "adds GpuCount annotation",
			value:               "1",
			fn:                  SetGpuCountAnnotation,
			suppliedAnnotations: map[string]string{},
			expectedAnnotations: map[string]string{
				GpuCountKey: "1",
			},
		},
		{
			name:  "replaces GpuType annotation",
			value: "nvidia.com/gpu",
			fn:    SetGpuTypeAnnotation,
			suppliedAnnotations: map[string]string{
				GpuTypeKey: "",
			},
			expectedAnnotations: map[string]string{
				GpuTypeKey: "nvidia.com/gpu",
			},
		},
		{
			name:                "adds GpuType annotation",
			value:               "nvidia.com/gpu",
			fn:                  SetGpuTypeAnnotation,
			suppliedAnnotations: map[string]string{},
			expectedAnnotations: map[string]string{
				GpuTypeKey: "nvidia.com/gpu",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			observed := tc.fn(tc.suppliedAnnotations, tc.value)
			if !maps.Equal(observed, tc.expectedAnnotations) {
				t.Errorf("SetAnnotations() returned %v, expected %v", observed, tc.expectedAnnotations)
			}
		})
	}
}

func TestParsingAnnotations(t *testing.T) {
	tests := []struct {
		name           string
		annotations    map[string]string
		key            string
		expectError    bool
		expectedReturn string
	}{
		{
			name: "Tests that key and value exists in annotation",
			annotations: map[string]string{
				CpuKey: "1",
			},
			key:            CpuKey,
			expectError:    false,
			expectedReturn: "1",
		},
		{
			name: "Tests that key and empty value exists in annotation",
			annotations: map[string]string{
				CpuKey: "",
			},
			key:            CpuKey,
			expectError:    false,
			expectedReturn: "",
		},
		{
			name:           "Tests that empty string is returned if key doesn't exist",
			annotations:    map[string]string{},
			key:            CpuKey,
			expectError:    true,
			expectedReturn: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			observed, err := ParseMachineSetAnnotationKey(tc.annotations, tc.key)

			if tc.expectError == true {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
			g.Expect(observed).To(Equal(tc.expectedReturn))
		})
	}
}

func TestHasScaleFromZero(t *testing.T) {
	tests := []struct {
		name                string
		suppliedAnnotations map[string]string
		expectedReturn      bool
	}{
		{
			name: "Tests that ScaleFromZero is enabled",
			suppliedAnnotations: map[string]string{
				CpuKey:    "1",
				MemoryKey: "4Gi",
			},
			expectedReturn: true,
		},
		{
			name: "Tests that ScaleFromZero fails with one required value missing",
			suppliedAnnotations: map[string]string{
				CpuKey:    "1",
				MemoryKey: "",
			},
			expectedReturn: false,
		},
		{
			name: "Tests that ScaleFromZero fails with both required values missing",
			suppliedAnnotations: map[string]string{
				CpuKey:    "",
				MemoryKey: "",
			},
			expectedReturn: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			observed := HasScaleFromZeroAnnotationsEnabled(tc.suppliedAnnotations)

			g.Expect(observed).To(Equal(tc.expectedReturn))
		})
	}
}
