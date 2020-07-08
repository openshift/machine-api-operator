module github.com/openshift/machine-api-operator

go 1.13

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/fsnotify/fsnotify v1.4.7
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/google/gofuzz v1.1.0
	github.com/google/uuid v1.1.1
	github.com/metal3-io/baremetal-operator v0.0.0-20200707131856-231ba343d896 // indirect
	github.com/onsi/ginkgo v1.12.0
	github.com/onsi/gomega v1.9.0
	github.com/openshift/api v0.0.0-20200424083944-0422dc17083e
	github.com/openshift/client-go v0.0.0-20200326155132-2a6cd50aedd0
	github.com/openshift/cluster-api-provider-baremetal v0.0.0-20200623154250-657bc273a391 // indirect
	github.com/openshift/cluster-api-provider-gcp v0.0.1-0.20200701112720-3a7d727c9a10
	github.com/openshift/library-go v0.0.0-20200512120242-21a1ff978534
	github.com/operator-framework/operator-sdk v0.17.0
	github.com/prometheus/client_golang v1.5.1
	github.com/spf13/cobra v0.0.6
	github.com/stretchr/testify v1.4.0
	github.com/vmware/govmomi v0.22.2
	golang.org/x/net v0.0.0-20200501053045-e0ff5e5a1de5
	gopkg.in/fsnotify.v1 v1.4.7
	gopkg.in/gcfg.v1 v1.2.3
	k8s.io/api v0.18.2
	k8s.io/apimachinery v0.18.2
	k8s.io/client-go v12.0.0+incompatible
	k8s.io/code-generator v0.18.2
	k8s.io/klog v1.0.0
	k8s.io/kubectl v0.18.0-rc.1
	k8s.io/utils v0.0.0-20200414100711-2df71ebbae66
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000
	sigs.k8s.io/cluster-api-provider-azure v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-runtime v0.6.0
	sigs.k8s.io/controller-tools v0.3.0
	sigs.k8s.io/yaml v1.2.0
)

replace sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20200520125206-5e266b553d8e

replace sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20200529030741-17d4edc5142f
