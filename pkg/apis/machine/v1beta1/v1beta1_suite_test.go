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

package v1beta1

import (
	"context"
	"crypto/tls"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	fuzz "github.com/google/gofuzz"
	osconfigv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeserializer "k8s.io/apimachinery/pkg/runtime/serializer"
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
			filepath.Join("..", "..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1"),
		},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			MutatingWebhooks:   []client.Object{NewMutatingWebhookConfiguration()},
			ValidatingWebhooks: []client.Object{NewValidatingWebhookConfiguration()},
		},
	}

	err := SchemeBuilder.AddToScheme(scheme.Scheme)
	if err != nil {
		log.Fatal(err)
	}

	err = osconfigv1.AddToScheme(scheme.Scheme)
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
	testEnv.Stop()
	os.Exit(code)
}

func machineFuzzerFuncs(codecs runtimeserializer.CodecFactory) []interface{} {
	return []interface{}{
		// Fuzzer for pointer to metav1.Time
		func(j **metav1.Time, c fuzz.Continue) {
			if c.RandBool() {
				t := &time.Time{}
				c.Fuzz(t)
				*j = &metav1.Time{Time: *t}
			} else {
				*j = nil
			}
		},
		// Fuzzer for MachineSpec to ensure empty embedded maps are nil
		func(j *MachineSpec, c fuzz.Continue) {
			c.FuzzNoCustom(j)

			// Fuzz ObjectMeta using custom fuzzer
			c.Fuzz(&j.ObjectMeta)

			// Ensure embedded maps are nil if they have zero length
			if len(j.ObjectMeta.Labels) == 0 {
				j.ObjectMeta.Labels = nil
			}
			if len(j.ObjectMeta.Annotations) == 0 {
				j.ObjectMeta.Annotations = nil
			}

			// Ensure slices are nil if they are empty
			if len(j.Taints) == 0 {
				j.Taints = nil
			}
		},
		// Fuzzer for MachineStatus to ensure empty embedded maps are nil
		func(j *MachineStatus, c fuzz.Continue) {
			c.FuzzNoCustom(j)

			// Fuzz LastUpdated using custom fuzzer
			c.Fuzz(&j.LastUpdated)
			c.Fuzz(&j.LastOperation)

			// Ensure slices are nil if they are empty
			if len(j.Addresses) == 0 {
				j.Addresses = nil
			}
		},
		// Fuzzer for MachineSetSpec to ensure value restrictions are honoured
		func(j *MachineSetSpec, c fuzz.Continue) {
			c.FuzzNoCustom(j)

			// Fuzz Selector using custom fuzzer
			c.Fuzz(&j.Selector)
			if len(j.Selector.MatchLabels) == 0 {
				j.Selector.MatchLabels = nil
			}
			if len(j.Selector.MatchExpressions) == 0 {
				j.Selector.MatchExpressions = nil
			}

			// Fuzz Template using custom fuzzers
			c.Fuzz(&j.Template)

			// Ensure replicas is greater than zero
			replicas := c.Rand.Int31()
			j.Replicas = &replicas

			// Set DeletionPolicy to a valid value
			validDeletionPolicy := []string{
				string(RandomMachineSetDeletePolicy),
				string(NewestMachineSetDeletePolicy),
				string(OldestMachineSetDeletePolicy),
			}
			j.DeletePolicy = validDeletionPolicy[c.Rand.Intn(len(validDeletionPolicy))]
		},
		// Fuzzer for MachineSetStatus to ensure value restrictions are honoured
		func(j *MachineSetStatus, c fuzz.Continue) {
			c.FuzzNoCustom(j)

			// Ensure replicas is greater than zero
			j.Replicas = c.Rand.Int31()
		},
		// Fuzzer for ObjectMeta to ensure empty maps are nil
		func(j *ObjectMeta, c fuzz.Continue) {
			c.FuzzNoCustom(j)

			if len(j.Labels) == 0 {
				j.Labels = nil
			} else {
				delete(j.Labels, "")
			}
			if len(j.Annotations) == 0 {
				j.Annotations = nil
			} else {
				delete(j.Annotations, "")
			}
			if len(j.OwnerReferences) == 0 {
				j.OwnerReferences = nil
			}
		},
		// Fuzzer for MachineTemplateSpec to ensure empty embedded maps are nil
		func(j *MachineTemplateSpec, c fuzz.Continue) {
			c.FuzzNoCustom(j)

			// Fuzz the ObjectMeta
			c.Fuzz(&j.ObjectMeta)

			// Ensure embedded maps are nil if they have zero length
			if len(j.ObjectMeta.Labels) == 0 {
				j.ObjectMeta.Labels = nil
			}
			if len(j.ObjectMeta.Annotations) == 0 {
				j.ObjectMeta.Annotations = nil
			}

			// Fuzz the Spec
			c.Fuzz(&j.Spec)
		},
	}
}
