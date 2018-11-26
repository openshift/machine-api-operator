package cvo

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-version-operator/lib"
	"github.com/openshift/cluster-version-operator/lib/resourcebuilder"
)

func TestHasRequeueOnErrorAnnotation(t *testing.T) {
	tests := []struct {
		annos map[string]string

		exp     bool
		experrs []string
	}{{
		annos:   nil,
		exp:     false,
		experrs: nil,
	}, {
		annos:   map[string]string{"dummy": "dummy"},
		exp:     false,
		experrs: nil,
	}, {
		annos:   map[string]string{RequeueOnErrorAnnotationKey: "NoMatch"},
		exp:     true,
		experrs: []string{"NoMatch"},
	}, {
		annos:   map[string]string{RequeueOnErrorAnnotationKey: "NoMatch,NotFound"},
		exp:     true,
		experrs: []string{"NoMatch", "NotFound"},
	}}
	for idx, test := range tests {
		t.Run(fmt.Sprintf("test#%d", idx), func(t *testing.T) {
			got, goterrs := hasRequeueOnErrorAnnotation(test.annos)
			if got != test.exp {
				t.Fatalf("expected %v got %v", test.exp, got)
			}
			if !reflect.DeepEqual(goterrs, test.experrs) {
				t.Fatalf("expected %v got %v", test.exp, got)
			}
		})
	}
}

func TestShouldRequeueOnErr(t *testing.T) {
	tests := []struct {
		err      error
		manifest string
		exp      bool
	}{{
		err: nil,
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap"
		}`,

		exp: false,
	}, {
		err: fmt.Errorf("random error"),
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap"
		}`,

		exp: false,
	}, {
		err: &meta.NoResourceMatchError{},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap"
		}`,

		exp: false,
	}, {
		err: &updateError{cause: &meta.NoResourceMatchError{}},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap"
		}`,

		exp: false,
	}, {
		err: &meta.NoResourceMatchError{},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"annotations": {
					"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
				}
			}
		}`,

		exp: true,
	}, {
		err: &updateError{cause: &meta.NoResourceMatchError{}},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"annotations": {
					"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
				}
			}
		}`,

		exp: true,
	}, {
		err: &meta.NoResourceMatchError{},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"annotations": {
					"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NotFound"
				}
			}
		}`,

		exp: false,
	}, {
		err: &updateError{cause: &meta.NoResourceMatchError{}},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"annotations": {
					"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NotFound"
				}
			}
		}`,

		exp: false,
	}, {
		err: apierrors.NewInternalError(fmt.Errorf("dummy")),
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"annotations": {
					"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
				}
			}
		}`,

		exp: false,
	}, {
		err: &updateError{cause: apierrors.NewInternalError(fmt.Errorf("dummy"))},
		manifest: `{
			"apiVersion": "v1",
			"kind": "ConfigMap",
			"metadata": {
				"annotations": {
					"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
				}
			}
		}`,

		exp: false,
	}}
	for idx, test := range tests {
		t.Run(fmt.Sprintf("test#%d", idx), func(t *testing.T) {
			var manifest lib.Manifest
			if err := json.Unmarshal([]byte(test.manifest), &manifest); err != nil {
				t.Fatal(err)
			}
			if got := shouldRequeueOnErr(test.err, &manifest); got != test.exp {
				t.Fatalf("expected %v got %v", test.exp, got)
			}
		})
	}
}

func TestSyncUpdatePayload(t *testing.T) {
	tests := []struct {
		manifests []string
		reactors  map[action]error

		check func(*testing.T, []action)
	}{{
		manifests: []string{
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestA",
				"metadata": {
					"namespace": "default",
					"name": "testa"
				}
			}`,
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestB",
				"metadata": {
					"namespace": "default",
					"name": "testb"
				}
			}`,
		},
		reactors: map[action]error{},
		check: func(t *testing.T, actions []action) {
			if len(actions) != 2 {
				spew.Dump(actions)
				t.Fatal("expected only 2 actions")
			}

			if got, exp := actions[0], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
			if got, exp := actions[1], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestB"}, "default", "testb"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
		},
	}, {
		manifests: []string{
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestA",
				"metadata": {
					"namespace": "default",
					"name": "testa"
				}
			}`,
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestB",
				"metadata": {
					"namespace": "default",
					"name": "testb"
				}
			}`,
		},
		reactors: map[action]error{
			action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}: &meta.NoResourceMatchError{},
		},
		check: func(t *testing.T, actions []action) {
			if len(actions) != 3 {
				spew.Dump(actions)
				t.Fatal("expected only 3 actions")
			}

			if got, exp := actions[0], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
		},
	}, {
		manifests: []string{
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestA",
				"metadata": {
					"namespace": "default",
					"name": "testa",
					"annotations": {
						"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
					}
				}
			}`,
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestB",
				"metadata": {
					"namespace": "default",
					"name": "testb"
				}
			}`,
		},
		reactors: map[action]error{
			action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}: &meta.NoResourceMatchError{},
		},
		check: func(t *testing.T, actions []action) {
			if len(actions) != 7 {
				spew.Dump(actions)
				t.Fatal("expected only 7 actions")
			}

			if got, exp := actions[0], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
			if got, exp := actions[3], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestB"}, "default", "testb"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
			if got, exp := actions[4], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
		},
	}, {
		manifests: []string{
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestA",
				"metadata": {
					"namespace": "default",
					"name": "testa",
					"annotations": {
						"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
					}
				}
			}`,
			`{
				"apiVersion": "test.cvo.io/v1",
				"kind": "TestB",
				"metadata": {
					"namespace": "default",
					"name": "testb",
					"annotations": {
						"v1.cluster-version-operator.operators.openshift.io/requeue-on-error": "NoMatch"
					}
				}
			}`,
		},
		reactors: map[action]error{
			action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}: &meta.NoResourceMatchError{},
			action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestB"}, "default", "testb"}: &meta.NoResourceMatchError{},
		},
		check: func(t *testing.T, actions []action) {
			if len(actions) != 9 {
				spew.Dump(actions)
				t.Fatal("expected only 12 actions")
			}

			if got, exp := actions[0], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
			if got, exp := actions[3], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestB"}, "default", "testb"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
			if got, exp := actions[6], (action{schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, "default", "testa"}); !reflect.DeepEqual(got, exp) {
				t.Fatalf("expected: %s got: %s", spew.Sdump(exp), spew.Sdump(got))
			}
		},
	}}
	for idx, test := range tests {
		t.Run(fmt.Sprintf("test#%d", idx), func(t *testing.T) {
			var manifests []lib.Manifest
			for _, s := range test.manifests {
				m := lib.Manifest{}
				if err := json.Unmarshal([]byte(s), &m); err != nil {
					t.Fatal(err)
				}
				manifests = append(manifests, m)
			}

			up := &updatePayload{releaseImage: "test", releaseVersion: "v0.0.0", manifests: manifests}
			op := &Operator{}
			op.syncBackoff = wait.Backoff{Steps: 3}
			config := &configv1.ClusterVersion{}
			r := &recorder{}
			testMapper := resourcebuilder.NewResourceMapper()
			testMapper.RegisterGVK(schema.GroupVersionKind{"test.cvo.io", "v1", "TestA"}, newTestBuilder(r, test.reactors))
			testMapper.RegisterGVK(schema.GroupVersionKind{"test.cvo.io", "v1", "TestB"}, newTestBuilder(r, test.reactors))
			testMapper.AddToMap(resourcebuilder.Mapper)

			op.syncUpdatePayload(config, up)
			test.check(t, r.actions)
		})
	}
}

type testBuilder struct {
	*recorder
	reactors map[action]error

	m *lib.Manifest
}

func (t *testBuilder) WithModifier(_ resourcebuilder.MetaV1ObjectModifierFunc) resourcebuilder.Interface {
	return t
}

func (t *testBuilder) Do() error {
	a := t.recorder.Invoke(t.m.GVK, t.m.Object().GetNamespace(), t.m.Object().GetName())
	return t.reactors[a]
}

func newTestBuilder(r *recorder, rts map[action]error) resourcebuilder.NewInteraceFunc {
	return func(_ *rest.Config, m lib.Manifest) resourcebuilder.Interface {
		return &testBuilder{recorder: r, reactors: rts, m: &m}
	}
}

type recorder struct {
	actions []action
}

func (r *recorder) Invoke(gvk schema.GroupVersionKind, namespace, name string) action {
	action := action{GVK: gvk, Namespace: namespace, Name: name}
	r.actions = append(r.actions, action)
	return action
}

type action struct {
	GVK       schema.GroupVersionKind
	Namespace string
	Name      string
}
