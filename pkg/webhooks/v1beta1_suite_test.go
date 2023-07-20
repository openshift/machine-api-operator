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

package webhooks

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	cfg                *rest.Config
	c                  client.Client
	ctx                = context.Background()
	testEnv            *envtest.Environment
	insecureHTTPClient = http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
)

func TestMain(m *testing.M) {
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "install"),
			filepath.Join("..", "..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1"),
		},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			MutatingWebhooks:   []*admissionregistrationv1.MutatingWebhookConfiguration{NewMachineMutatingWebhookConfiguration()},
			ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{NewMachineValidatingWebhookConfiguration()},
		},
	}

	err := machinev1.Install(scheme.Scheme)
	if err != nil {
		log.Fatal(err)
	}

	err = osconfigv1.Install(scheme.Scheme)
	if err != nil {
		log.Fatal(err)
	}

	if cfg, err = testEnv.Start(); err != nil {
		log.Fatal(err)
	}

	if c, err = client.New(cfg, client.Options{Scheme: scheme.Scheme}); err != nil {
		log.Fatal(err)
	}

	// Azure credentialsSecret is a secretRef defaulting to defaultSecretNamespace instead of a localObjectRef.
	// This is so the tests can assume this namespace exists.
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultSecretNamespace,
		},
	}
	if err = c.Create(ctx, namespace); err != nil {
		log.Fatal(err)
	}

	code := m.Run()
	if err = testEnv.Stop(); err != nil {
		log.Fatal(err)
	}
	os.Exit(code)
}
