package session

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/vmware/govmomi/vim25/soap"
)

type fakeSOAPRoundTripper struct {
	calls int
	req   soap.HasFault
	err   error
}

func (f *fakeSOAPRoundTripper) RoundTrip(_ context.Context, req, _ soap.HasFault) error {
	f.calls++
	f.req = req
	return f.err
}

type metricSOAPRequestBody struct{}

func (*metricSOAPRequestBody) Fault() *soap.Fault { return nil }

type unexpectedSOAPRequest struct{}

func (unexpectedSOAPRequest) Fault() *soap.Fault { return nil }

type fakeHTTPRoundTripper struct {
	calls    int
	response *http.Response
	err      error
}

func (f *fakeHTTPRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	f.calls++
	return f.response, f.err
}

func newTestHistogram(t *testing.T) (*prometheus.Registry, *prometheus.HistogramVec) {
	t.Helper()
	registry := prometheus.NewRegistry()
	histogram := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "test_vsphere_request_duration_seconds",
		Help:    "Test vSphere request duration.",
		Buckets: []float64{0.1, 1},
	}, []string{"client", "operation", "status"})
	registry.MustRegister(histogram)
	return registry, histogram
}

func histogramSampleCount(t *testing.T, registry *prometheus.Registry, labels map[string]string) uint64 {
	t.Helper()
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			observed := make(map[string]string, len(metric.GetLabel()))
			for _, label := range metric.GetLabel() {
				observed[label.GetName()] = label.GetValue()
			}
			matches := len(observed) == len(labels)
			for key, value := range labels {
				if observed[key] != value {
					matches = false
				}
			}
			if matches && metric.GetHistogram() != nil {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0
}

func TestMetricRoundTripper(t *testing.T) {
	registry, histogram := newTestHistogram(t)
	inner := &fakeSOAPRoundTripper{}
	transport := &metricRoundTripper{roundTripper: inner, histogram: histogram}

	if err := transport.RoundTrip(context.Background(), &metricSOAPRequestBody{}, &metricSOAPRequestBody{}); err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	if inner.calls != 1 {
		t.Fatalf("inner RoundTrip() calls = %d, want 1", inner.calls)
	}
	if inner.req == nil {
		t.Fatal("inner RoundTrip() did not receive request")
	}
	if got := histogramSampleCount(t, registry, map[string]string{
		"client": "soap", "operation": "metricSOAPRequest", "status": "success",
	}); got != 1 {
		t.Fatalf("success sample count = %d, want 1", got)
	}

	expectedErr := errors.New("soap request failed")
	inner.err = expectedErr
	if err := transport.RoundTrip(context.Background(), &metricSOAPRequestBody{}, &metricSOAPRequestBody{}); !errors.Is(err, expectedErr) {
		t.Fatalf("RoundTrip() error = %v, want %v", err, expectedErr)
	}
	if got := histogramSampleCount(t, registry, map[string]string{
		"client": "soap", "operation": "metricSOAPRequest", "status": "error",
	}); got != 1 {
		t.Fatalf("error sample count = %d, want 1", got)
	}
}

func TestMetricRoundTripperUnknownRequestDoesNotPanic(t *testing.T) {
	registry, histogram := newTestHistogram(t)
	inner := &fakeSOAPRoundTripper{}
	transport := &metricRoundTripper{roundTripper: inner, histogram: histogram}

	if err := transport.RoundTrip(context.Background(), nil, &metricSOAPRequestBody{}); err != nil {
		t.Fatalf("RoundTrip(nil) error = %v", err)
	}
	if err := transport.RoundTrip(context.Background(), unexpectedSOAPRequest{}, &metricSOAPRequestBody{}); err != nil {
		t.Fatalf("RoundTrip(unexpected) error = %v", err)
	}
	if inner.calls != 2 {
		t.Fatalf("inner RoundTrip() calls = %d, want 2", inner.calls)
	}
	if got := histogramSampleCount(t, registry, map[string]string{
		"client": "soap", "operation": "unknown", "status": "success",
	}); got != 2 {
		t.Fatalf("unknown request sample count = %d, want 2", got)
	}
}

func TestMetricHTTPTransport(t *testing.T) {
	tests := []struct {
		name       string
		requestURL string
		response   *http.Response
		err        error
		status     string
	}{
		{
			name:       "success",
			requestURL: "https://vcenter.example/rest/com/vmware/cis/tagging/tag?foo=bar",
			response:   &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(nil)},
			status:     "success",
		},
		{
			name:       "http error",
			requestURL: "https://vcenter.example/rest/com/vmware/cis/tagging/tag/id/42",
			response:   &http.Response{StatusCode: http.StatusInternalServerError, Body: io.NopCloser(nil)},
			status:     "error",
		},
		{
			name:       "transport error",
			requestURL: "https://vcenter.example/rest/com/vmware/cis/tagging/tag",
			err:        errors.New("transport failed"),
			status:     "error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registry, histogram := newTestHistogram(t)
			inner := &fakeHTTPRoundTripper{response: test.response, err: test.err}
			transport := &metricHTTPTransport{roundTripper: inner, histogram: histogram}
			req, err := http.NewRequest(http.MethodGet, test.requestURL, nil)
			if err != nil {
				t.Fatal(err)
			}

			response, gotErr := transport.RoundTrip(req)
			if response != test.response {
				t.Fatalf("response = %p, want %p", response, test.response)
			}
			if !errors.Is(gotErr, test.err) {
				t.Fatalf("error = %v, want %v", gotErr, test.err)
			}
			if inner.calls != 1 {
				t.Fatalf("inner RoundTrip() calls = %d, want 1", inner.calls)
			}
			operation := "GET /rest/com/vmware/cis/tagging/tag"
			if test.name == "http error" {
				operation += "/id/{id}"
			}
			if got := histogramSampleCount(t, registry, map[string]string{
				"client": "rest", "operation": operation, "status": test.status,
			}); got != 1 {
				t.Fatalf("sample count = %d, want 1", got)
			}
		})
	}
}

func TestNormalizeRESTPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{
			path: "/rest/com/vmware/cis/tagging/tag/id/urn:vmomi:InventoryServiceTag:abc",
			want: "/rest/com/vmware/cis/tagging/tag/id/{id}",
		},
		{
			path: "/rest/com/vmware/cis/tagging/tag/id:urn:vmomi:InventoryServiceTag:abc",
			want: "/rest/com/vmware/cis/tagging/tag/{id}",
		},
		{
			path: "/rest/com/vmware/cis/tagging/tag/id/550e8400-e29b-41d4-a716-446655440000",
			want: "/rest/com/vmware/cis/tagging/tag/id/{id}",
		},
		{
			path: "/rest/com/vmware/cis/tagging/tag/id/42",
			want: "/rest/com/vmware/cis/tagging/tag/id/{id}",
		},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := normalizeRESTPath(test.path); got != test.want {
				t.Fatalf("normalizeRESTPath() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRESTOperationUsesURLPath(t *testing.T) {
	req := &http.Request{Method: http.MethodPost, URL: &url.URL{Path: "/rest/com/vmware/cis/tagging/tag", RawQuery: "foo=bar"}}
	if got, want := restOperation(req), "POST /rest/com/vmware/cis/tagging/tag"; got != want {
		t.Fatalf("restOperation() = %q, want %q", got, want)
	}
}
