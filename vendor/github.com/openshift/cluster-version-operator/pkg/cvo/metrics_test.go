package cvo

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_operatorMetrics_Collect(t *testing.T) {
	tests := []struct {
		name  string
		optr  *Operator
		wants func(*testing.T, []prometheus.Metric)
	}{
		{
			name: "collects current version",
			optr: &Operator{
				releaseVersion: "0.0.2",
				releaseImage:   "test/image:1",
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 1 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "0.0.2", "payload": "test/image:1"})
			},
		},
		{
			name: "collects cluster operator status failure",
			optr: &Operator{
				clusterOperatorLister: &coLister{
					Items: []*configv1.ClusterOperator{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test",
							},
							Status: configv1.ClusterOperatorStatus{
								Version: "10.1.5-1",
								Conditions: []configv1.ClusterOperatorStatusCondition{
									{Type: configv1.OperatorAvailable, Status: configv1.ConditionTrue},
									{Type: configv1.OperatorFailing, Status: configv1.ConditionTrue},
								},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 4 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "", "payload": ""})
				expectMetric(t, metrics[1], 0, map[string]string{"name": "test", "version": "10.1.5-1", "namespace": ""})
				expectMetric(t, metrics[2], 1, map[string]string{"name": "test", "condition": "Available", "namespace": ""})
				expectMetric(t, metrics[3], 1, map[string]string{"name": "test", "condition": "Failing", "namespace": ""})
			},
		},
		{
			name: "collects cluster operator status custom",
			optr: &Operator{
				clusterOperatorLister: &coLister{
					Items: []*configv1.ClusterOperator{
						{
							ObjectMeta: metav1.ObjectMeta{
								Namespace: "default",
								Name:      "test",
							},
							Status: configv1.ClusterOperatorStatus{
								Conditions: []configv1.ClusterOperatorStatusCondition{
									{Type: configv1.OperatorAvailable, Status: configv1.ConditionTrue},
									{Type: configv1.ClusterStatusConditionType("Custom"), Status: configv1.ConditionFalse},
									{Type: configv1.ClusterStatusConditionType("Unknown"), Status: configv1.ConditionUnknown},
								},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 4 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "", "payload": ""})
				expectMetric(t, metrics[1], 1, map[string]string{"name": "test", "version": "", "namespace": "default"})
				expectMetric(t, metrics[2], 1, map[string]string{"name": "test", "condition": "Available", "namespace": "default"})
				expectMetric(t, metrics[3], 0, map[string]string{"name": "test", "condition": "Custom", "namespace": "default"})
			},
		},
		{
			name: "collects available updates",
			optr: &Operator{
				name: "test",
				cvLister: &cvLister{
					Items: []*configv1.ClusterVersion{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test",
							},
							Status: configv1.ClusterVersionStatus{
								AvailableUpdates: []configv1.Update{
									{Version: "1.0.1"},
									{Version: "1.0.2"},
								},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 2 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "", "payload": ""})
				expectMetric(t, metrics[1], 2, map[string]string{"upstream": "<default>", "channel": ""})
			},
		},
		{
			name: "collects available updates and reports 0 when updates fetched",
			optr: &Operator{
				name: "test",
				cvLister: &cvLister{
					Items: []*configv1.ClusterVersion{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test",
							},
							Status: configv1.ClusterVersionStatus{
								Conditions: []configv1.ClusterOperatorStatusCondition{
									{Type: configv1.RetrievedUpdates, Status: configv1.ConditionTrue},
								},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 2 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "", "payload": ""})
				expectMetric(t, metrics[1], 0, map[string]string{"upstream": "<default>", "channel": ""})
			},
		},
		{
			name: "collects update",
			optr: &Operator{
				releaseVersion: "0.0.2",
				releaseImage:   "test/image:1",
				name:           "test",
				cvLister: &cvLister{
					Items: []*configv1.ClusterVersion{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test",
							},
							Spec: configv1.ClusterVersionSpec{
								DesiredUpdate: &configv1.Update{Version: "1.0.0", Payload: "test/image:2"},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 2 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "0.0.2", "payload": "test/image:1"})
				expectMetric(t, metrics[1], 1, map[string]string{"type": "update", "version": "1.0.0", "payload": "test/image:2"})
			},
		},
		{
			name: "collects failing update",
			optr: &Operator{
				releaseVersion: "0.0.2",
				releaseImage:   "test/image:1",
				name:           "test",
				cvLister: &cvLister{
					Items: []*configv1.ClusterVersion{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test",
							},
							Spec: configv1.ClusterVersionSpec{
								DesiredUpdate: &configv1.Update{Version: "1.0.0", Payload: "test/image:2"},
							},
							Status: configv1.ClusterVersionStatus{
								Conditions: []configv1.ClusterOperatorStatusCondition{
									{Type: configv1.OperatorFailing, Status: configv1.ConditionTrue},
								},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 4 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "0.0.2", "payload": "test/image:1"})
				expectMetric(t, metrics[1], 1, map[string]string{"type": "update", "version": "1.0.0", "payload": "test/image:2"})
				expectMetric(t, metrics[2], 1, map[string]string{"type": "failure", "version": "1.0.0", "payload": "test/image:2"})
				expectMetric(t, metrics[3], 1, map[string]string{"type": "failure", "version": "0.0.2", "payload": "test/image:1"})
			},
		},
		{
			name: "collects failing payload",
			optr: &Operator{
				releaseVersion: "0.0.2",
				releaseImage:   "test/image:1",
				name:           "test",
				cvLister: &cvLister{
					Items: []*configv1.ClusterVersion{
						{
							ObjectMeta: metav1.ObjectMeta{
								Name: "test",
							},
							Status: configv1.ClusterVersionStatus{
								Conditions: []configv1.ClusterOperatorStatusCondition{
									{Type: configv1.OperatorFailing, Status: configv1.ConditionTrue},
								},
							},
						},
					},
				},
			},
			wants: func(t *testing.T, metrics []prometheus.Metric) {
				if len(metrics) != 2 {
					t.Fatalf("Unexpected metrics %#v", metrics)
				}
				expectMetric(t, metrics[0], 1, map[string]string{"type": "current", "version": "0.0.2", "payload": "test/image:1"})
				expectMetric(t, metrics[1], 1, map[string]string{"type": "failure", "version": "0.0.2", "payload": "test/image:1"})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.optr.cvLister == nil {
				tt.optr.cvLister = &cvLister{}
			}
			if tt.optr.clusterOperatorLister == nil {
				tt.optr.clusterOperatorLister = &coLister{}
			}
			m := newOperatorMetrics(tt.optr)
			ch := make(chan prometheus.Metric)
			go func() {
				m.Collect(ch)
				close(ch)
			}()
			var collected []prometheus.Metric
			for sample := range ch {
				collected = append(collected, sample)
			}
			tt.wants(t, collected)
		})
	}
}

func expectMetric(t *testing.T, metric prometheus.Metric, value float64, labels map[string]string) {
	t.Helper()
	var d dto.Metric
	if err := metric.Write(&d); err != nil {
		t.Fatalf("unable to write metrics: %v", err)
	}
	if d.Gauge != nil {
		if value != *d.Gauge.Value {
			t.Fatalf("incorrect value: %f", *d.Gauge.Value)
		}
	}
	for _, label := range d.Label {
		if labels[*label.Name] != *label.Value {
			t.Fatalf("unexpected labels: %s", d.Label)
		}
		delete(labels, *label.Name)
	}
	if len(labels) > 0 {
		t.Fatalf("missing labels: %v", labels)
	}
}
