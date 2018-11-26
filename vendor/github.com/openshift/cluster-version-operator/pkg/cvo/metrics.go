package cvo

import (
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-version-operator/lib/resourcemerge"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/labels"
)

var (
	metricPayload = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cluster_version_payload",
		Help: "Report the number of entries in the payload.",
	}, []string{"version", "type"})
	metricPayloadErrors = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "cluster_operator_payload_errors",
		Help: "Report the number of errors encountered applying the payload.",
	}, []string{"version"})
)

func (optr *Operator) registerMetrics() error {
	if err := prometheus.Register(metricPayload); err != nil {
		return err
	}
	if err := prometheus.Register(metricPayloadErrors); err != nil {
		return err
	}
	m := newOperatorMetrics(optr)
	return prometheus.Register(m)
}

type operatorMetrics struct {
	optr *Operator

	version                   *prometheus.GaugeVec
	availableUpdates          *prometheus.GaugeVec
	clusterOperatorConditions *prometheus.GaugeVec
	clusterOperatorUp         *prometheus.GaugeVec
}

func newOperatorMetrics(optr *Operator) *operatorMetrics {
	return &operatorMetrics{
		optr: optr,

		version: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cluster_version",
			Help: "Reports the version of the cluster.",
		}, []string{"type", "version", "payload"}),
		availableUpdates: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cluster_version_available_updates",
			Help: "Report the count of available versions for an upstream and channel.",
		}, []string{"upstream", "channel"}),
		clusterOperatorUp: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cluster_operator_up",
			Help: "Reports key highlights of the active cluster operators.",
		}, []string{"namespace", "name", "version"}),
		clusterOperatorConditions: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "cluster_operator_conditions",
			Help: "Report the conditions for active cluster operators. 0 is False and 1 is True.",
		}, []string{"namespace", "name", "condition"}),
	}
}

func (m *operatorMetrics) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.version.WithLabelValues("", "", "").Desc()
}

func (m *operatorMetrics) Collect(ch chan<- prometheus.Metric) {
	current := m.optr.currentVersion()
	g := m.version.WithLabelValues("current", current.Version, current.Payload)
	g.Set(1)
	ch <- g
	if cv, err := m.optr.cvLister.Get(m.optr.name); err == nil {
		// output cluster version
		failing := resourcemerge.IsOperatorStatusConditionTrue(cv.Status.Conditions, configv1.OperatorFailing)
		if update := cv.Spec.DesiredUpdate; update != nil && update.Payload != current.Payload {
			g := m.version.WithLabelValues("update", update.Version, update.Payload)
			g.Set(1)
			ch <- g
			if failing {
				g = m.version.WithLabelValues("failure", update.Version, update.Payload)
				g.Set(1)
				ch <- g
			}
		}
		if failing {
			g := m.version.WithLabelValues("failure", current.Version, current.Payload)
			g.Set(1)
			ch <- g
		}

		if len(cv.Spec.Upstream) > 0 || len(cv.Status.AvailableUpdates) > 0 || resourcemerge.IsOperatorStatusConditionTrue(cv.Status.Conditions, configv1.RetrievedUpdates) {
			upstream := "<default>"
			if len(cv.Spec.Upstream) > 0 {
				upstream = string(cv.Spec.Upstream)
			}
			g := m.availableUpdates.WithLabelValues(upstream, cv.Spec.Channel)
			g.Set(float64(len(cv.Status.AvailableUpdates)))
			ch <- g
		}
	}

	// output cluster operator version and condition info
	operators, _ := m.optr.clusterOperatorLister.List(labels.Everything())
	for _, op := range operators {
		g := m.clusterOperatorUp.WithLabelValues(op.Namespace, op.Name, op.Status.Version)
		failing := resourcemerge.IsOperatorStatusConditionTrue(op.Status.Conditions, configv1.OperatorFailing)
		available := resourcemerge.IsOperatorStatusConditionTrue(op.Status.Conditions, configv1.OperatorAvailable)
		if available && !failing {
			g.Set(1)
		} else {
			g.Set(0)
		}
		ch <- g
		for _, condition := range op.Status.Conditions {
			if condition.Status == configv1.ConditionUnknown {
				continue
			}
			g := m.clusterOperatorConditions.WithLabelValues(op.Namespace, op.Name, string(condition.Type))
			if condition.Status == configv1.ConditionTrue {
				g.Set(1)
			} else {
				g.Set(0)
			}
			ch <- g
		}
	}
}
