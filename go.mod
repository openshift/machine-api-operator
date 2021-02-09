module github.com/openshift/machine-api-operator

go 1.13

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/google/gofuzz v1.1.0
	github.com/google/uuid v1.1.2
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/openshift/api v0.0.0-20201216151826-78a19e96f9eb
	github.com/openshift/client-go v0.0.0-20201214125552-e615e336eb49
	github.com/openshift/cluster-api-provider-gcp v0.0.1-0.20201201000827-1117a4fc438c
	github.com/openshift/library-go v0.0.0-20201215165635-4ee79b1caed5
	github.com/operator-framework/operator-sdk v0.5.1-0.20190301204940-c2efe6f74e7b
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	github.com/vmware/govmomi v0.22.2
	golang.org/x/net v0.0.0-20201110031124-69a78807bb2b
	gopkg.in/gcfg.v1 v1.2.3
	k8s.io/api v0.20.0
	k8s.io/apimachinery v0.20.0
	k8s.io/client-go v0.20.0
	k8s.io/code-generator v0.20.0
	k8s.io/klog/v2 v2.4.0
	k8s.io/kubectl v0.20.0
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000
	sigs.k8s.io/cluster-api-provider-azure v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-runtime v0.7.0
	sigs.k8s.io/controller-tools v0.3.0
	sigs.k8s.io/yaml v1.2.0
)

replace sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201125052318-b85a18cbf338

replace sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.0.0-20210209143830-3442c7a36c1e
