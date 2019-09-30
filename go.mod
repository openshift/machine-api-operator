module github.com/openshift/machine-api-operator

go 1.12

require (
	cloud.google.com/go v0.36.0 // indirect
	github.com/blang/semver v3.5.1+incompatible
	github.com/ghodss/yaml v1.0.0
	github.com/gobuffalo/envy v1.6.15 // indirect
	github.com/golang/glog v0.0.0-20160126235308-23def4e6c14b
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/markbates/inflect v1.0.4 // indirect
	github.com/onsi/ginkgo v1.8.0
	github.com/onsi/gomega v1.5.0
	github.com/openshift/api v3.9.1-0.20190517100836-d5b34b957e91+incompatible
	github.com/openshift/client-go v0.0.0-20190617165122-8892c0adc000
	github.com/openshift/cluster-api v0.0.0-20190805113604-f8de78af80fc
	github.com/openshift/cluster-api-actuator-pkg v0.0.0-20190904193718-8250b456dec7
	github.com/openshift/cluster-autoscaler-operator v0.0.1-0.20190521201101-62768a6ba480
	github.com/openshift/cluster-version-operator v3.11.1-0.20190629164025-08cac1c02538+incompatible
	github.com/operator-framework/operator-sdk v0.5.1-0.20190301204940-c2efe6f74e7b
	github.com/prometheus/client_golang v1.0.0
	github.com/rogpeppe/go-internal v1.3.0 // indirect
	github.com/spf13/cobra v0.0.3
	github.com/stretchr/testify v1.3.0
	gonum.org/v1/gonum v0.0.0-20190915125329-975d99cd20a9 // indirect
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
	k8s.io/code-generator v0.0.0-00010101000000-000000000000
	k8s.io/klog v0.4.0
	k8s.io/utils v0.0.0-20190607212802-c55fbcfc754a
	sigs.k8s.io/controller-runtime v0.2.0-beta.2
	sigs.k8s.io/controller-tools v0.1.10
)

replace gopkg.in/fsnotify.v1 v1.4.7 => github.com/fsnotify/fsnotify v1.4.7

replace k8s.io/apiextensions-apiserver v0.0.0-20190315093550-53c4693659ed => github.com/openshift/kubernetes-apiextensions-apiserver v0.0.0-20190315093550-53c4693659ed

replace k8s.io/apimachinery v0.0.0-20190313205120-d7deff9243b1 => github.com/openshift/kubernetes-apimachinery v0.0.0-20190313205120-d7deff9243b1

replace k8s.io/client-go v11.0.0+incompatible => github.com/openshift/kubernetes-client-go v11.0.0+incompatible

replace k8s.io/kube-aggregator v0.0.0-20190314000639-da8327669ac5 => github.com/openshift/kubernetes-kube-aggregator v0.0.0-20190314000639-da8327669ac5

replace github.com/prometheus/client_golang => github.com/prometheus/client_golang v0.9.2

replace k8s.io/code-generator => k8s.io/code-generator v0.0.0-20190311093542-50b561225d70
