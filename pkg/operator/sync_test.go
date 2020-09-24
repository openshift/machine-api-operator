package operator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/diff"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clientTesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/cache"
)

func TestWaitForDeploymentRollout(t *testing.T) {
	testCases := []struct {
		name       string
		deployment *appsv1.Deployment
		expected   error
	}{
		{
			name: "Deployment is available for more than deploymentMinimumAvailabilityTime min",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-deploymentMinimumAvailabilityTime - 1*time.Second)},
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "Deployment is available for less than deploymentMinimumAvailabilityTime min",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Second)},
						},
					},
				},
			},
			expected: fmt.Errorf("deployment test has been available for less than 3 min"),
		},
		{
			name: "Deployment has unavailable replicas",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 1,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Second)},
						},
					},
				},
			},
			expected: fmt.Errorf("deployment test is not ready. status: (replicas: 1, updated: 1, ready: 1, unavailable: 1)"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			optr := newFakeOperator([]runtime.Object{tc.deployment}, nil, make(<-chan struct{}))

			got := optr.waitForDeploymentRollout(tc.deployment, 1*time.Second, 3*time.Second)
			if tc.expected != nil {
				if tc.expected.Error() != got.Error() {
					t.Errorf("Got: %v, expected: %v", got, tc.expected)
				}
			} else if tc.expected != got {
				t.Errorf("Got: %v, expected: %v", got, tc.expected)
			}
		})
	}
}

type webhookTestCase struct {
	testCase             string
	shouldSync           bool
	serverOperationError string
	exisingWebhook       func() *unstructured.Unstructured
}

func TestSyncValidatingWebhooks(t *testing.T) {
	defaultConfiguration := mapiv1.NewValidatingWebhookConfiguration()

	testCases := []webhookTestCase{
		{
			testCase:   "It should create webhookConfiguration if it does not exsit",
			shouldSync: true,
		},
		{
			testCase: "It should not update webhookConfiguration if it already exist and is equal expected",
			exisingWebhook: func() *unstructured.Unstructured {
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfiguration.DeepCopy())
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: false,
		},
		{
			testCase: "It should not create webhookConfiguration if the server is down",
			exisingWebhook: func() *unstructured.Unstructured {
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfiguration.DeepCopy())
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			serverOperationError: "get",
			shouldSync:           false,
		},
		{
			testCase: "It shouldn't update webhookConfiguration if only caBundle field have changed",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].ClientConfig.CABundle = []byte("test")
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook.DeepCopy())
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: false,
		},
		{
			testCase: "It should update webhookConfiguration if some of their webhooks differ",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].Name = "test"
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It should not update webhookConfiguration if some of their webhooks differ, but the server is down",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].Name = "test"
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			serverOperationError: "update",
			shouldSync:           false,
		},
		{
			testCase: "It should update webhookConfiguration if its webhook list is missing an element",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks = webhook.Webhooks[:1]
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It should update webhookConfiguration if its some webhook has a change in a field populated by defaults",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				// Set a non default policy
				policy := admissionregistrationv1.Exact
				webhook.Webhooks[0].MatchPolicy = &policy
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It shoud update webhookConfiguration if some webhooks are removed from the list",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks = append(webhook.Webhooks, mapiv1.MachineValidatingWebhook())
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It shoud update webhookConfiguration if some slice subelement was extended with items",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].Rules[0].Operations = append(webhook.Webhooks[0].Rules[0].Operations, admissionregistrationv1.Connect)
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It should update webhookConfiguration if some slice subelement had a change in the order",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				rules := []admissionregistrationv1.OperationType{admissionregistrationv1.Connect}
				webhook.Webhooks[0].Rules[0].Operations = append(rules, webhook.Webhooks[0].Rules[0].Operations...)
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
	}

	stop := make(chan struct{})
	defer close(stop)
	optr := newFakeOperator(nil, nil, stop)
	optr.syncHandler = nil

	configuration, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfiguration.DeepCopy())
	testSyncWebhookConfiguration(t, optr,
		admissionregistrationv1.SchemeGroupVersion.WithResource("validatingwebhookconfigurations"),
		&unstructured.Unstructured{Object: configuration},
		optr.syncValidatingWebhook,
		stop,
		optr.validatingWebhookListerSynced, testCases)
}

func TestSyncMutatingWebhooks(t *testing.T) {
	defaultConfiguration := mapiv1.NewMutatingWebhookConfiguration()

	testCases := []webhookTestCase{
		{
			testCase:   "It should create webhookConfiguration if it does not exsit",
			shouldSync: true,
		},
		{
			testCase: "It should not update webhookConfiguration if it already exist and is equal expected",
			exisingWebhook: func() *unstructured.Unstructured {
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfiguration.DeepCopy())
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: false,
		},
		{
			testCase: "It should not create webhookConfiguration if the server is down",
			exisingWebhook: func() *unstructured.Unstructured {
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfiguration.DeepCopy())
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			serverOperationError: "get",
			shouldSync:           false,
		},
		{
			testCase: "It shouldn't update webhookConfiguration if only caBundle field have changed",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].ClientConfig.CABundle = []byte("test")
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook.DeepCopy())
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: false,
		},
		{
			testCase: "It should update webhookConfiguration if some of their webhooks differ",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].Name = "test"
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It should not update webhookConfiguration if some of their webhooks differ, but the server is down",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].Name = "test"
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			serverOperationError: "update",
			shouldSync:           false,
		},
		{
			testCase: "It should update webhookConfiguration if its webhook list is missing an element",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks = webhook.Webhooks[:1]
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It shoud update webhookConfiguration if some webhooks are removed from the list",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks = append(webhook.Webhooks, mapiv1.MachineMutatingWebhook())
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It shoud update webhookConfiguration if some slice subelement was extended with items",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				webhook.Webhooks[0].Rules[0].Operations = append(webhook.Webhooks[0].Rules[0].Operations, admissionregistrationv1.Connect)
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
		{
			testCase: "It should update webhookConfiguration if some slice subelement had a change in the order",
			exisingWebhook: func() *unstructured.Unstructured {
				webhook := defaultConfiguration.DeepCopy()
				rules := []admissionregistrationv1.OperationType{admissionregistrationv1.Connect}
				webhook.Webhooks[0].Rules[0].Operations = append(rules, webhook.Webhooks[0].Rules[0].Operations...)
				exisingWebhook, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(webhook)
				return &unstructured.Unstructured{Object: exisingWebhook}
			},
			shouldSync: true,
		},
	}

	stop := make(chan struct{})
	defer close(stop)
	optr := newFakeOperator(nil, nil, stop)
	optr.syncHandler = nil

	configuration, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(defaultConfiguration.DeepCopy())
	testSyncWebhookConfiguration(t, optr,
		admissionregistrationv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations"),
		&unstructured.Unstructured{Object: configuration},
		optr.syncMutatingWebhook,
		stop,
		optr.mutatingWebhookListerSynced, testCases)
}

func testSyncWebhookConfiguration(
	t *testing.T,
	optr *Operator,
	gvr schema.GroupVersionResource,
	defaultConfiguration *unstructured.Unstructured,
	sync func() error,
	stop chan struct{},
	waitForSync cache.InformerSynced,
	testCases []webhookTestCase,
) {
	expectedName := "test"
	expectedUnstructured := defaultConfiguration.DeepCopy()
	expectedUnstructured.SetName(expectedName)
	expectedMap, _ := runtime.DefaultUnstructuredConverter.ToUnstructured(expectedUnstructured)
	clientIn := fakedynamic.NewSimpleDynamicClient(scheme.Scheme, []runtime.Object{}...)
	optr.dynamicClient = clientIn

	serverError := errors.New("Server error")
	addReactor := func(operation string) {
		reactor := func(action clientTesting.Action) (bool, runtime.Object, error) {
			return true, &admissionregistrationv1.ValidatingWebhookConfiguration{}, serverError
		}
		clientIn.PrependReactor(operation, gvr.Resource, reactor)
	}

	removeReactor := func() {
		clientIn.ReactionChain = clientIn.ReactionChain[1:]
	}

	client := clientIn.Resource(gvr)
	expected, err := client.Create(context.Background(), &unstructured.Unstructured{Object: expectedMap}, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Unexpected error during creation of an expected webhook configuration: %q", err.Error())
	}
	defer func() {
		if err = client.Delete(context.Background(), expectedName, metav1.DeleteOptions{}); err != nil {
			t.Fatalf("Unexpected error during deletion of an expected webhook configuration: %q", err.Error())
		}
	}()
	expectedWebhooks, _, err := unstructured.NestedSlice(expected.Object, "webhooks")
	if err != nil {
		t.Fatalf("Unexpected error while fetching expected webhook list: %v", err)
	}
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			if !cache.WaitForCacheSync(stop, waitForSync) {
				t.Fatalf("Failed to sync caches")
			}

			if tc.exisingWebhook != nil {
				if _, err = client.Create(context.Background(), tc.exisingWebhook(), metav1.CreateOptions{}); err != nil {
					t.Fatalf("Unexpected error during creation of an exising webhook configuration: %q", err.Error())
				}
			}
			defer func() {
				if err = client.Delete(context.Background(), defaultConfiguration.GetName(), metav1.DeleteOptions{}); err != nil {
					t.Fatalf("Unexpected error during deletion of an exising webhook configuration: %q", err.Error())
				}
			}()

			if !cache.WaitForCacheSync(stop, waitForSync) {
				t.Fatalf("Failed to sync caches")
			}

			if tc.serverOperationError != "" {
				addReactor(tc.serverOperationError)
				if err := sync(); err != serverError {
					t.Fatalf("Unexpected error during webhook syncronization: %q, expected %q", err.Error(), serverError)
				}
				removeReactor()
			} else {
				if err := sync(); err != nil {
					t.Fatalf("Unexpected error during webhook syncronization: %q", err.Error())
				}
			}

			existing, err := client.Get(context.Background(), defaultConfiguration.GetName(), metav1.GetOptions{})
			if err != nil {
				t.Fatalf("Unexpected error while getting validating webhook: %q", err.Error())
			}

			existingWebhooks, _, err := unstructured.NestedSlice(existing.Object, "webhooks")
			if err != nil {
				t.Fatalf("Unexpected error reading updated webhooks list: %v", err)
			}
			if tc.shouldSync {
				if annotations, _, err := unstructured.NestedStringMap(existing.Object, "metadata", "annotations"); err != nil ||
					!equality.Semantic.DeepDerivative(expectedUnstructured.GetAnnotations(), annotations) {
					t.Errorf("Expected hook annotations match:\n%#v\n, got:\n%#v\n, error: %v", expectedUnstructured.GetAnnotations(), annotations, err)
				}
				if !equality.Semantic.DeepEqual(expectedWebhooks, existingWebhooks) {
					t.Errorf("Expected webhhoks match:\n%#v\n, got:\n%#v\n", expectedWebhooks, existingWebhooks)
				}
			} else {
				initialExistingWebhooks, _, err := unstructured.NestedSlice(tc.exisingWebhook().Object, "webhooks")
				if err != nil || !equality.Semantic.DeepEqual(initialExistingWebhooks, existingWebhooks) {
					t.Errorf("Expected webhhoks match initial configuration:\n%#v\n, got:\n%#v\n, error: %v", initialExistingWebhooks, existingWebhooks, err)
				}
			}
		})
	}
}

// func TestApplyUnstructured(t *testing.T) {
// 	testResource := admissionregistrationv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations")

// 	cases := []struct {
// 		name           string
// 		path           string
// 		existing       runtime.Object
// 		object         func() *unstructured.Unstructured
// 		expectModified bool
// 		serverError    string
// 		err            error
// 	}{
// 		{
// 			name: "Successfully create an object",
// 			path: "webhooks",
// 			object: func() *unstructured.Unstructured {
// 				obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(mapiv1.NewMutatingWebhookConfiguration())
// 				if err != nil {
// 					t.Fatalf("Failed to covnert resource to unstructured: %v", err)
// 				}
// 				return &unstructured.Unstructured{Object: obj}
// 			},
// 			expectModified: true,
// 		},
// 		{
// 			name: "Successfully update an object",
// 			path: "webhooks",
// 			object: func() *unstructured.Unstructured {
// 				resource := mapiv1.NewMutatingWebhookConfiguration()
// 				resource.Webhooks = append(resource.Webhooks, resource.Webhooks[0])
// 				obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
// 				if err != nil {
// 					t.Fatalf("Failed to covnert resource to unstructured: %v", err)
// 				}
// 				return &unstructured.Unstructured{Object: obj}
// 			},
// 			existing:       mapiv1.NewMutatingWebhookConfiguration(),
// 			expectModified: true,
// 		},
// 		{
// 			name:   "Fail to apply a nil object",
// 			object: func() *unstructured.Unstructured { return nil },
// 			err:    errors.New("Unexpected nil instead of an object"),
// 		},
// 		{
// 			name:   "Fail to apply an empty object",
// 			path:   "webhooks",
// 			object: func() *unstructured.Unstructured { return &unstructured.Unstructured{} },
// 			err:    errors.New("Object does not contain the specified path: webhooks"),
// 		},
// 		{
// 			name: "Fail to create an object in case of a server error on 'get' requests",
// 			path: "webhooks",
// 			object: func() *unstructured.Unstructured {
// 				obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(mapiv1.NewMutatingWebhookConfiguration())
// 				if err != nil {
// 					t.Fatalf("Failed to covnert resource to unstructured: %v", err)
// 				}
// 				return &unstructured.Unstructured{Object: obj}
// 			},
// 			serverError: "get",
// 			err:         errors.New("Custom server error"),
// 		},
// 		{
// 			name: "Fail to create an object in case of a server error on 'create' requests",
// 			path: "webhooks",
// 			object: func() *unstructured.Unstructured {
// 				obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(mapiv1.NewMutatingWebhookConfiguration())
// 				if err != nil {
// 					t.Fatalf("Failed to covnert resource to unstructured: %v", err)
// 				}
// 				return &unstructured.Unstructured{Object: obj}
// 			},
// 			serverError: "create",
// 			err:         errors.New("Custom server error"),
// 		},
// 		{
// 			name: "Fail to update an object in case of a server error on 'create' requests",
// 			path: "webhooks",
// 			object: func() *unstructured.Unstructured {
// 				resource := mapiv1.NewMutatingWebhookConfiguration()
// 				resource.Webhooks = append(resource.Webhooks, resource.Webhooks[0])
// 				obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(resource)
// 				if err != nil {
// 					t.Fatalf("Failed to covnert resource to unstructured: %v", err)
// 				}
// 				return &unstructured.Unstructured{Object: obj}
// 			},
// 			existing:    mapiv1.NewMutatingWebhookConfiguration(),
// 			serverError: "update",
// 			err:         errors.New("Custom server error"),
// 		},
// 	}

// 	for _, test := range cases {
// 		t.Run(test.name, func(t *testing.T) {
// 			var objects []runtime.Object
// 			if test.existing != nil {
// 				objects = append(objects, test.existing)
// 			}
// 			clientIn := fakedynamic.NewSimpleDynamicClient(scheme.Scheme, objects...)
// 			client := clientIn.Resource(testResource)
// 			if test.serverError != "" {
// 				reactor := func(action clientTesting.Action) (bool, runtime.Object, error) {
// 					return true, nil, test.err
// 				}
// 				clientIn.PrependReactor(test.serverError, "*", reactor)
// 			}

// 			applied, modified, err := applyUnstructured(client, test.path, eventstesting.NewTestingEventRecorder(t), test.object(), 0)
// 			if test.err != nil {
// 				if err == nil || test.err.Error() != err.Error() {
// 					t.Fatalf("Expected error to be equal %v, got %v", test.err, err)
// 				}
// 				return
// 			}

// 			if test.expectModified != modified {
// 				t.Errorf("Expected modified to be %v, got %v", test.expectModified, modified)
// 				return
// 			}

// 			expectedChange := test.err == nil
// 			if expectedChange && equality.Semantic.DeepEqual(applied, test.object) {
// 				t.Error("Expected object to be updated")
// 			} else if !expectedChange && !equality.Semantic.DeepEqual(applied, test.object) {
// 				t.Error("Expected object to not be updated")
// 			}
// 		})
// 	}
// }

func Test_ensureDependecyAnnotations(t *testing.T) {
	cases := []struct {
		name string

		input       *appsv1.Deployment
		inputHashes map[string]string

		expected *appsv1.Deployment
	}{{
		name:        "no previous hash tag",
		input:       &appsv1.Deployment{},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-1"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
	}, {
		name: "changed in on of the dependencies",
		input: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-2"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-2",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-2",
						},
					},
				},
			},
		},
	}, {
		name: "no change in dependencies",
		input: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-1"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
	}}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := test.input.DeepCopy()
			ensureDependecyAnnotations(test.inputHashes, input)
			if !equality.Semantic.DeepEqual(test.expected, input) {
				t.Fatalf("unexpected: %s", diff.ObjectDiff(test.expected, input))
			}
		})
	}
}
