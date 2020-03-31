module github.com/openshift/machine-api-operator

go 1.13

require (
	github.com/blang/semver v3.5.1+incompatible
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/golang/groupcache v0.0.0-20191002201903-404acd9df4cc // indirect
	github.com/google/uuid v1.1.1
	github.com/hashicorp/golang-lru v0.5.3 // indirect
	github.com/imdario/mergo v0.3.8 // indirect
	github.com/openshift/api v0.0.0-20200323095748-e7041f8762a3
	github.com/openshift/client-go v0.0.0-20200320150128-a906f3d8e723
	github.com/openshift/library-go v0.0.0-20200324092245-db2a8546af81
	github.com/operator-framework/operator-sdk v0.5.1-0.20190301204940-c2efe6f74e7b
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.1.0
	github.com/prometheus/common v0.7.0 // indirect
	github.com/prometheus/procfs v0.0.5 // indirect
	github.com/spf13/cobra v0.0.5
	github.com/stretchr/testify v1.4.0
	github.com/vmware/govmomi v0.22.2
	golang.org/x/net v0.0.0-20200202094626-16171245cfb2
	golang.org/x/time v0.0.0-20190921001708-c4c64cad1fd0 // indirect
	google.golang.org/appengine v1.6.4 // indirect
	gopkg.in/gcfg.v1 v1.2.3
	gopkg.in/warnings.v0 v0.1.2 // indirect
	k8s.io/api v0.18.0-rc.1
	k8s.io/apimachinery v0.18.0-rc.1
	k8s.io/client-go v0.18.0-rc.1
	k8s.io/code-generator v0.18.0-rc.1
	k8s.io/klog v1.0.0
	k8s.io/kube-aggregator v0.18.0-rc.1 // indirect
	k8s.io/kubectl v0.18.0-rc.1
	k8s.io/utils v0.0.0-20200229041039-0a110f9eb7ab
	sigs.k8s.io/controller-runtime v0.3.1-0.20191016212439-2df793d02076
	sigs.k8s.io/controller-tools v0.2.8
	sigs.k8s.io/yaml v1.2.0
)

replace sigs.k8s.io/controller-runtime => github.com/munnerz/controller-runtime v0.1.8-0.20200318092001-e22ac1073450
