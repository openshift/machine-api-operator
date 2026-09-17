package session

import (
	"context"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	gofault "github.com/vmware/govmomi/fault"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
)

const unknownOperation = "unknown"

var uuidPathSegment = regexp.MustCompile(`^[[:xdigit:]]{8}-[[:xdigit:]]{4}-[1-5][[:xdigit:]]{3}-[89abAB][[:xdigit:]]{3}-[[:xdigit:]]{12}$`)

type metricRoundTripper struct {
	roundTripper soap.RoundTripper
	histogram    *prometheus.HistogramVec
	// onSessionInvalid, when set, fires when a SOAP call reports the session
	// is no longer authenticated (vim.fault.NotAuthenticated) so the session
	// can be evicted from the cache.
	onSessionInvalid func()
}

func (t *metricRoundTripper) RoundTrip(ctx context.Context, req, res soap.HasFault) error {
	operation := soapOperation(req)
	start := time.Now()
	err := t.roundTripper.RoundTrip(ctx, req, res)
	status := "success"
	if err != nil {
		status = "error"
		if isSOAPAuthFailure(err) && t.onSessionInvalid != nil {
			t.onSessionInvalid()
		}
	}
	t.histogram.WithLabelValues("soap", operation, status).Observe(time.Since(start).Seconds())
	return err
}

// isSOAPAuthFailure reports whether a SOAP round-trip error indicates the
// session was invalidated (vim.fault.NotAuthenticated) and a re-login is
// required before the session can be reused.
func isSOAPAuthFailure(err error) bool {
	return gofault.Is(err, &types.NotAuthenticated{}) ||
		gofault.Is(err, &types.NotAuthenticatedFault{})
}

func soapOperation(req soap.HasFault) string {
	if req == nil {
		return unknownOperation
	}

	typ := reflect.TypeOf(req)
	value := reflect.ValueOf(req)
	if typ.Kind() != reflect.Ptr || value.IsNil() || typ.Elem().Kind() != reflect.Struct {
		return unknownOperation
	}

	name := typ.Elem().Name()
	if !strings.HasSuffix(name, "Body") || len(name) == len("Body") {
		return unknownOperation
	}
	return strings.TrimSuffix(name, "Body")
}

type metricHTTPTransport struct {
	roundTripper http.RoundTripper
	histogram    *prometheus.HistogramVec
	// onSessionInvalid, when set, fires when a REST call returns 401 so the
	// session can be evicted from the cache.
	onSessionInvalid func()
}

func (t *metricHTTPTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	transport := t.roundTripper
	if transport == nil {
		transport = http.DefaultTransport
	}
	response, err := transport.RoundTrip(req)
	status := "success"
	if err != nil || response == nil || response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		status = "error"
	}
	if response != nil && response.StatusCode == http.StatusUnauthorized && t.onSessionInvalid != nil {
		t.onSessionInvalid()
	}
	t.histogram.WithLabelValues("rest", restOperation(req), status).Observe(time.Since(start).Seconds())
	return response, err
}

func restOperation(req *http.Request) string {
	if req == nil || req.URL == nil {
		return "UNKNOWN " + unknownOperation
	}
	return req.Method + " " + normalizeRESTPath(req.URL.Path)
}

func normalizeRESTPath(path string) string {
	segments := strings.Split(path, "/")
	for i, segment := range segments {
		if isIDPathSegment(segment) {
			segments[i] = "{id}"
		}
	}
	return strings.Join(segments, "/")
}

func isIDPathSegment(segment string) bool {
	if segment == "" {
		return false
	}
	lower := strings.ToLower(segment)
	if strings.HasPrefix(lower, "urn:") || strings.HasPrefix(lower, "id:") || uuidPathSegment.MatchString(segment) {
		return true
	}
	_, err := strconv.ParseUint(segment, 10, 64)
	return err == nil
}
