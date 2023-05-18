package tools

import (
	_ "github.com/golangci/golangci-lint/cmd/golangci-lint"
	_ "github.com/onsi/ginkgo/v2/ginkgo"
	_ "sigs.k8s.io/cluster-api/exp/ipam/api/v1alpha1"
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
)
