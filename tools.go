//go:build tools
// +build tools

package tools

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/onsi/ginkgo/v2/ginkgo"
	_ "github.com/openshift/api/config/v1/zz_generated.crd-manifests"
	_ "github.com/openshift/api/machine/v1beta1/zz_generated.crd-manifests"
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
)
