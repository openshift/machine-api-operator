package metrics

import (
	"testing"

	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

func TestVsphereRequestDurationSecondsRegistered(t *testing.T) {
	VsphereRequestDurationSeconds.WithLabelValues("soap", "RetrieveProperties", "success").Observe(1)

	families, err := crmetrics.Registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}

	for _, family := range families {
		if family.GetName() == "mapi_vsphere_request_duration_seconds" {
			return
		}
	}

	t.Fatal("mapi_vsphere_request_duration_seconds was not registered")
}
