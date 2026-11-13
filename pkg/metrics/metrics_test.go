package metrics

import (
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	fakemachine "github.com/openshift/client-go/machine/clientset/versioned/fake"
	machineinformers "github.com/openshift/client-go/machine/informers/externalversions"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

const testNamespace = "openshift-machine-api"

var initializeMHCMetricsOnce sync.Once

func metricNames(t *testing.T, gatherer prometheus.Gatherer) map[string]struct{} {
	t.Helper()
	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("gathering metrics: %v", err)
	}
	names := make(map[string]struct{}, len(families))
	for _, family := range families {
		names[family.GetName()] = struct{}{}
	}
	return names
}

func assertMetricNamesPresent(t *testing.T, gatherer prometheus.Gatherer, want ...string) {
	t.Helper()
	names := metricNames(t, gatherer)
	var missing []string
	for _, name := range want {
		if _, ok := names[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("expected metrics missing from controller-runtime registry: %s", strings.Join(missing, ", "))
	}
}

func TestVsphereRequestDurationSecondsRegistered(t *testing.T) {
	VsphereRequestDurationSeconds.WithLabelValues("soap", "RetrieveProperties", "success").Observe(1)
	assertMetricNamesPresent(t, ctrlmetrics.Registry, "mapi_vsphere_request_duration_seconds")
}

func TestControllerRuntimeRegistryContainsExpectedMetrics(t *testing.T) {
	// Priming labeled metrics so they appear in Gather output.
	MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machine_items"}).Set(1)
	MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machineset_items"}).Set(1)
	MachinePhaseTransitionSeconds.With(prometheus.Labels{"phase": "Running"}).Observe(1)
	RegisterFailedInstanceCreate(&MachineLabels{Name: "m", Namespace: testNamespace, Reason: "test"})
	RegisterFailedInstanceUpdate(&MachineLabels{Name: "m", Namespace: testNamespace, Reason: "test"})
	RegisterFailedInstanceDelete(&MachineLabels{Name: "m", Namespace: testNamespace, Reason: "test"})

	assertMetricNamesPresent(t, ctrlmetrics.Registry,
		"mapi_mao_collector_up",
		"mapi_machine_phase_transition_seconds",
		"mapi_instance_create_failed",
		"mapi_instance_update_failed",
		"mapi_instance_delete_failed",
	)
}

func TestMachineHealthCheckMetricsRegistered(t *testing.T) {
	initializeMHCMetricsOnce.Do(func() {
		InitializeMachineHealthCheckMetrics()
	})

	ObserveMachineHealthCheckNodesCovered("mhc", testNamespace, 2)
	ObserveMachineHealthCheckRemediationSuccess("mhc", testNamespace)
	ObserveMachineHealthCheckShortCircuitEnabled("mhc", testNamespace)

	assertMetricNamesPresent(t, ctrlmetrics.Registry,
		"mapi_machinehealthcheck_nodes_covered",
		"mapi_machinehealthcheck_remediation_success_total",
		"mapi_machinehealthcheck_short_circuit",
	)
}

func TestMachineCollectorEmitsExpectedMetricsAndCollectorUp(t *testing.T) {
	g := NewWithT(t)

	creationTime := metav1.NewTime(time.Unix(1_700_000_000, 0))
	machine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-machine",
			Namespace:         testNamespace,
			CreationTimestamp: creationTime,
		},
		Spec: machinev1.MachineSpec{
			ProviderID: ptr.To("aws:///us-east-1a/i-123"),
		},
		Status: machinev1.MachineStatus{
			Phase: ptr.To("Running"),
			NodeRef: &corev1.ObjectReference{
				Name: "test-node",
			},
		},
	}
	machineSet := &machinev1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "test-machineset",
			Namespace:         testNamespace,
			CreationTimestamp: creationTime,
		},
		Status: machinev1.MachineSetStatus{
			Replicas:          3,
			ReadyReplicas:     2,
			AvailableReplicas: 1,
		},
	}

	collector := newTestMachineCollector(t, machine, machineSet)

	expected := `
# HELP mapi_machine_created_timestamp_seconds Timestamp of the mapi managed Machine creation time
# TYPE mapi_machine_created_timestamp_seconds gauge
mapi_machine_created_timestamp_seconds{api_version="",name="test-machine",namespace="openshift-machine-api",node="test-node",phase="Running",spec_provider_id="aws:///us-east-1a/i-123"} 1.7e+09
# HELP mapi_machine_items Count of machine objects currently at the apiserver
# TYPE mapi_machine_items gauge
mapi_machine_items 1
# HELP mapi_machine_set_status_replicas Information of the mapi managed Machineset's status for replicas
# TYPE mapi_machine_set_status_replicas gauge
mapi_machine_set_status_replicas{name="test-machineset",namespace="openshift-machine-api"} 3
# HELP mapi_machine_set_status_replicas_available Information of the mapi managed Machineset's status for available replicas
# TYPE mapi_machine_set_status_replicas_available gauge
mapi_machine_set_status_replicas_available{name="test-machineset",namespace="openshift-machine-api"} 1
# HELP mapi_machine_set_status_replicas_ready Information of the mapi managed Machineset's status for ready replicas
# TYPE mapi_machine_set_status_replicas_ready gauge
mapi_machine_set_status_replicas_ready{name="test-machineset",namespace="openshift-machine-api"} 2
# HELP mapi_machineset_created_timestamp_seconds Timestamp of the mapi managed Machineset creation time
# TYPE mapi_machineset_created_timestamp_seconds gauge
mapi_machineset_created_timestamp_seconds{api_version="",name="test-machineset",namespace="openshift-machine-api"} 1.7e+09
# HELP mapi_machineset_items Count of machinesets at the apiserver
# TYPE mapi_machineset_items gauge
mapi_machineset_items 1
`
	err := testutil.CollectAndCompare(collector, strings.NewReader(expected),
		"mapi_machine_items",
		"mapi_machineset_items",
		"mapi_machine_created_timestamp_seconds",
		"mapi_machineset_created_timestamp_seconds",
		"mapi_machine_set_status_replicas",
		"mapi_machine_set_status_replicas_available",
		"mapi_machine_set_status_replicas_ready",
	)
	g.Expect(err).ToNot(HaveOccurred())

	g.Expect(testutil.ToFloat64(MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machine_items"}))).To(Equal(float64(1)))
	g.Expect(testutil.ToFloat64(MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machineset_items"}))).To(Equal(float64(1)))
}

func TestMachineCollectorEmptyListsStillReportsCollectorUp(t *testing.T) {
	g := NewWithT(t)

	// Reset to 0 first so we prove Collect sets them back to 1 for empty-but-successful lists.
	MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machine_items"}).Set(0)
	MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machineset_items"}).Set(0)

	collector := newTestMachineCollector(t) // no objects
	ch := make(chan prometheus.Metric, 16)
	go func() {
		collector.Collect(ch)
		close(ch)
	}()
	for range ch {
	}

	g.Expect(testutil.ToFloat64(MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machine_items"}))).To(Equal(float64(1)))
	g.Expect(testutil.ToFloat64(MachineCollectorUp.With(prometheus.Labels{"kind": "mapi_machineset_items"}))).To(Equal(float64(1)))
}

func newTestMachineCollector(t *testing.T, objects ...runtime.Object) *MachineCollector {
	t.Helper()

	client := fakemachine.NewSimpleClientset() //nolint:staticcheck
	factory := machineinformers.NewSharedInformerFactoryWithOptions(client, 0, machineinformers.WithNamespace(testNamespace))
	machineInformer := factory.Machine().V1beta1().Machines()
	machineSetInformer := factory.Machine().V1beta1().MachineSets()

	for _, obj := range objects {
		switch o := obj.(type) {
		case *machinev1.Machine:
			if err := machineInformer.Informer().GetStore().Add(o); err != nil {
				t.Fatalf("adding machine to informer store: %v", err)
			}
		case *machinev1.MachineSet:
			if err := machineSetInformer.Informer().GetStore().Add(o); err != nil {
				t.Fatalf("adding machineset to informer store: %v", err)
			}
		default:
			t.Fatalf("unsupported object type %T", obj)
		}
	}

	return NewMachineCollector(machineInformer, machineSetInformer, testNamespace)
}
