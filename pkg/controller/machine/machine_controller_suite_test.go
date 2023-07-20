/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package machine

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	cfg *rest.Config
	ctx = context.Background()
)

func TestMain(m *testing.M) {
	t := &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "install")},
	}
	if err := machinev1.Install(scheme.Scheme); err != nil {
		log.Fatalf("cannot add scheme: %v", err)
	}

	var err error
	if cfg, err = t.Start(); err != nil {
		log.Fatal(err)
	}

	code := m.Run()
	if err = t.Stop(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}

// StartTestManager adds recFn
func StartTestManager(mgr manager.Manager, t *testing.T) (context.CancelFunc, chan error) {
	t.Helper()

	mgrCtx, cancel := context.WithCancel(ctx)
	errs := make(chan error, 1)

	go func() {
		errs <- mgr.Start(mgrCtx)
	}()

	return cancel, errs
}
