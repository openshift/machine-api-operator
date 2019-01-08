package version

import (
	"fmt"
	"strings"

	"github.com/blang/semver"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	// Raw is the string representation of the version. This will be replaced
	// with the calculated version at build time.
	Raw = "v0.0.0-was-not-built-properly"

	// Version is semver representation of the version.
	Version = semver.MustParse(strings.TrimLeft(Raw, "v"))

	// String is the human-friendly representation of the version.
	String = fmt.Sprintf("MachineAPIOperator %s", Raw)
)

func init() {
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "openshift_cluster_openshift_apiserver_operator_build_info",
			Help: "A metric with a constant '1' value labeled by major, minor, git commit & git version from which OpenShift Service Serving Cert Signer was built.",
		},
		[]string{"Version"},
	)
	buildInfo.WithLabelValues(String).Set(1)

	prometheus.MustRegister(buildInfo)
}
