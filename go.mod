module github.com/openshift/machine-api-operator

go 1.13

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/evanphx/json-patch v4.9.0+incompatible
	github.com/go-logr/logr v0.2.1 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/gofuzz v1.1.0
	github.com/google/uuid v1.1.1
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	github.com/openshift/api v0.0.0-20200901182017-7ac89ba6b971
	github.com/openshift/client-go v0.0.0-20200827190008-3062137373b5
	github.com/openshift/cluster-api-provider-gcp v0.0.1-0.20200901173901-9056dbc8c9b9
	github.com/openshift/library-go v0.0.0-20200909173121-1d055d971916
	github.com/operator-framework/operator-sdk v0.5.1-0.20190301204940-c2efe6f74e7b
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/cobra v1.0.0
	github.com/stretchr/testify v1.5.1
	github.com/vmware/govmomi v0.22.2
	golang.org/x/crypto v0.0.0-20200622213623-75b288015ac9
	golang.org/x/net v0.0.0-20200707034311-ab3426394381
	gopkg.in/gcfg.v1 v1.2.3
	k8s.io/api v0.19.0
	k8s.io/apimachinery v0.19.0
	k8s.io/client-go v0.19.0
	k8s.io/code-generator v0.19.0
	k8s.io/klog v1.0.0
	k8s.io/kubectl v0.19.0
	k8s.io/utils v0.0.0-20200729134348-d5654de09c73
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000
	sigs.k8s.io/cluster-api-provider-azure v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-runtime v0.6.2
	sigs.k8s.io/controller-tools v0.3.0
	sigs.k8s.io/yaml v1.2.0
)

replace sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20200907134647-9d3a7be55678

replace sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20200908192026-727639324ebc
