//go:build tools
// +build tools

package tools

import (
	_ "github.com/ahmetb/gen-crd-api-reference-docs"
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"
)
