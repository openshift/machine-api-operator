module github.com/openshift/machine-api-operator

go 1.13

require (
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/blang/semver v3.5.1+incompatible
	github.com/go-logr/logr v0.4.0
	github.com/gobuffalo/flect v0.2.2 // indirect
	github.com/google/gofuzz v1.1.0
	github.com/google/uuid v1.1.2
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/onsi/ginkgo v1.15.0
	github.com/onsi/gomega v1.10.5
	github.com/openshift/api v0.0.0-20210412212256-79bd8cfbbd59
	github.com/openshift/client-go v0.0.0-20210409155308-a8e62c60e930
	github.com/openshift/cluster-api-provider-gcp v0.0.1-0.20201201000827-1117a4fc438c
	github.com/openshift/library-go v0.0.0-20210408164723-7a65fdb398e2
	github.com/operator-framework/operator-sdk v0.5.1-0.20190301204940-c2efe6f74e7b
	github.com/prometheus/client_golang v1.9.0
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.6.1
	github.com/vmware/govmomi v0.22.2
	golang.org/x/net v0.0.0-20210224082022-3d97a244fca7
	gopkg.in/gcfg.v1 v1.2.3
	k8s.io/api v0.21.0
	k8s.io/apimachinery v0.21.0
	k8s.io/apiserver v0.21.0
	k8s.io/client-go v0.21.0
	k8s.io/code-generator v0.21.0
	k8s.io/klog/v2 v2.8.0
	k8s.io/kubectl v0.21.0
	k8s.io/utils v0.0.0-20210111153108-fddb29f9d009
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000
	sigs.k8s.io/cluster-api-provider-azure v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-runtime v0.9.0-beta.1.0.20210512131817-ce2f0c92d77e
	sigs.k8s.io/controller-tools v0.3.0
	sigs.k8s.io/structured-merge-diff/v4 v4.1.1 // indirect
	sigs.k8s.io/yaml v1.2.0
)

replace sigs.k8s.io/cluster-api-provider-aws => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20201125052318-b85a18cbf338

replace sigs.k8s.io/cluster-api-provider-azure => github.com/openshift/cluster-api-provider-azure v0.0.0-20210209143830-3442c7a36c1e
