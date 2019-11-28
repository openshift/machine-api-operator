/*
Copyright 2015 The Kubernetes Authors.

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

package drain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/go-log/log/capture"

	appsv1 "k8s.io/api/apps/v1"
	batch "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/fake"
)

func boolptr(b bool) *bool { return &b }

func TestCordon(t *testing.T) {
	tests := []struct {
		description           string
		node                  string
		schedulable           bool
		cordon                bool
		expectedGetNodesError string
	}{
		{
			description: "uncordon for real",
			node:        "node",
			schedulable: false,
			cordon:      false,
		},
		{
			description: "uncordon does nothing",
			node:        "node",
			schedulable: true,
			cordon:      false,
		},
		{
			description: "cordon does nothing",
			node:        "node",
			schedulable: false,
			cordon:      true,
		},
		{
			description: "cordon for real",
			node:        "node",
			schedulable: true,
			cordon:      true,
		},
		{
			description:           "cordon missing node",
			node:                  "bar",
			schedulable:           true,
			cordon:                true,
			expectedGetNodesError: "^nodes \"bar\" not found$",
		},
		{
			description:           "uncordon missing node",
			node:                  "bar",
			cordon:                false,
			expectedGetNodesError: "^nodes \"bar\" not found$",
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			client := fake.NewSimpleClientset(&corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "node",
					CreationTimestamp: metav1.Time{Time: time.Now()},
				},
				Status: corev1.NodeStatus{},
				Spec: corev1.NodeSpec{
					Unschedulable: !test.schedulable,
				},
			}).CoreV1().Nodes()

			nodes, err := GetNodes(client, []string{test.node}, "")
			if err != nil {
				if len(test.expectedGetNodesError) == 0 {
					t.Fatal(err)
				} else if !regexp.MustCompile(test.expectedGetNodesError).MatchString(err.Error()) {
					t.Fatalf("%q does not match %q", err.Error(), test.expectedGetNodesError)
				}
				return
			} else if len(test.expectedGetNodesError) > 0 {
				t.Fatalf("expected error %q", test.expectedGetNodesError)
			}

			node := nodes[0]

			if test.cordon {
				err = Cordon(client, node, nil)
			} else {
				err = Uncordon(client, node, nil)
			}
			if err != nil {
				t.Fatal(err)
			}

			node, err = client.Get(node.Name, metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if node.Spec.Unschedulable != test.cordon {
				t.Errorf("node %q unschedulable %t (expected %t)", node.Name, node.Spec.Unschedulable, test.cordon)
			}
		})
	}
}

func TestDrain(t *testing.T) {
	labels := make(map[string]string)
	labels["my_key"] = "my_value"
	/* FIXME: these are not resource.Objects.  How to get them into NewSimpleClientset?
	policy := &metav1.APIGroupList{
		Groups: []metav1.APIGroup{
			{
				Name: "policy",
				PreferredVersion: metav1.GroupVersionForDiscovery{
					GroupVersion: "policy/v1beta1",
				},
			},
		},
	}

	resourceList := &metav1.APIResourceList{
		GroupVersion: "v1",
	}

	evictionResource := &metav1.APIResource{
		Name: EvictionSubresource,
		Kind: EvictionKind,
	}*/

	rc := &corev1.ReplicationController{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "rc",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
		},
		Spec: corev1.ReplicationControllerSpec{
			Selector: labels,
		},
	}

	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "node",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Status: corev1.NodeStatus{},
	}

	rcPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "ReplicationController",
					Name:               "rc",
					UID:                "123",
					BlockOwnerDeletion: boolptr(true),
					Controller:         boolptr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}

	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "ds",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
		},
	}

	dsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "appsv1/v1",
					Kind:               "DaemonSet",
					Name:               "ds",
					BlockOwnerDeletion: boolptr(true),
					Controller:         boolptr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}

	dsPodWithEmptyDir := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "apps/v1",
					Kind:               "DaemonSet",
					Name:               "ds",
					BlockOwnerDeletion: boolptr(true),
					Controller:         boolptr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
			Volumes: []corev1.Volume{
				{
					Name:         "scratch",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: ""}},
				},
			},
		},
	}

	orphanedDSpod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}

	job := &batch.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "job",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: batch.JobSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
		},
	}

	jobPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "Job",
					Name:               "job",
					BlockOwnerDeletion: boolptr(true),
					Controller:         boolptr(true),
				},
			},
		},
	}

	rs := &extensions.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "rs",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
		},
		Spec: extensions.ReplicaSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: labels},
		},
	}

	rsPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "v1",
					Kind:               "ReplicaSet",
					Name:               "rs",
					BlockOwnerDeletion: boolptr(true),
					Controller:         boolptr(true),
				},
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}

	nakedPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}

	podWithEmptyDir := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "bar",
			Namespace:         "default",
			CreationTimestamp: metav1.Time{Time: time.Now()},
			Labels:            labels,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
			Volumes: []corev1.Volume{
				{
					Name:         "scratch",
					VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: ""}},
				},
			},
		},
	}

	tests := []struct {
		description        string
		nodes              []string
		options            *DrainOptions
		objects            []runtime.Object
		logEntries         []string
		expectedDrainError string
	}{
		{
			description: "RC-managed pod (delete)",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				rc,
				node,
				rcPod,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				"pod \"bar\" removed (deleted)",
				"drained node \"node\"",
			},
		},
		/*		{
				description:  "RC-managed pod (evict)",
				nodes:        []string{"node"},
				options:      &DrainOptions{},
				objects:      []runtime.Object{
					policy,
					resourceList,
					evictionResource,
					rc,
					node,
					rcPod,
				},
				logEntries: []string{
					"cordoned node \"node\"",
					"pod \"bar\" removed (evicted)",
					"drained node \"node\"",
				},
			},*/
		{
			description: "DS-managed pod",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				rc,
				node,
				ds,
				dsPod,
			},
			expectedDrainError: "^DaemonSet-managed pods \\(use IgnoreDaemonsets to ignore\\): bar$",
		},
		{
			description: "orphaned DS-managed pod",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				node,
				orphanedDSpod,
			},
			expectedDrainError: "^pods not managed by ReplicationController, ReplicaSet, Job, DaemonSet or StatefulSet \\(use Force to override\\): bar$",
		},
		{
			description: "orphaned DS-managed pod with Force",
			nodes:       []string{"node"},
			options: &DrainOptions{
				Force: true,
			},
			objects: []runtime.Object{
				node,
				orphanedDSpod,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				fmt.Sprintf("%s: bar", kUnmanagedWarning),
				"pod \"bar\" removed (deleted)",
				"drained node \"node\"",
			},
		},
		{
			description: "DS-managed pod with IgnoreDaemonsets",
			nodes:       []string{"node"},
			options: &DrainOptions{
				IgnoreDaemonsets: true,
			},
			objects: []runtime.Object{
				rc,
				node,
				ds,
				dsPod,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				fmt.Sprintf("%s: bar", kDaemonsetWarning),
				"drained node \"node\"",
			},
		},
		{
			description: "DS-managed pod with emptyDir with IgnoreDaemonsets",
			nodes:       []string{"node"},
			options: &DrainOptions{
				IgnoreDaemonsets: true,
			},
			objects: []runtime.Object{
				rc,
				node,
				ds,
				dsPodWithEmptyDir,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				fmt.Sprintf("%s: bar", kDaemonsetWarning),
				"drained node \"node\"",
			},
		},
		{
			description: "Job-managed pod",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				rc,
				node,
				job,
				jobPod,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				"pod \"bar\" removed (deleted)",
				"drained node \"node\"",
			},
		},
		{
			description: "RS-managed pod",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				rc,
				node,
				rs,
				rsPod,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				"pod \"bar\" removed (deleted)",
				"drained node \"node\"",
			},
		},
		{
			description: "naked pod",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				node,
				nakedPod,
			},
			expectedDrainError: "^pods not managed by ReplicationController, ReplicaSet, Job, DaemonSet or StatefulSet \\(use Force to override\\): bar$",
		},
		{
			description: "naked pod with Force",
			nodes:       []string{"node"},
			options: &DrainOptions{
				Force: true,
			},
			objects: []runtime.Object{
				node,
				nakedPod,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				fmt.Sprintf("%s: bar", kUnmanagedWarning),
				"pod \"bar\" removed (deleted)",
				"drained node \"node\"",
			},
		},
		{
			description: "pod with EmptyDir",
			nodes:       []string{"node"},
			options: &DrainOptions{
				Force: true,
			},
			objects: []runtime.Object{
				node,
				podWithEmptyDir,
			},
			expectedDrainError: "^pods with local storage \\(use DeleteLocalData to override\\): bar$",
		},
		{
			description: "pod with EmptyDir and DeleteLocalData",
			nodes:       []string{"node"},
			options: &DrainOptions{
				DeleteLocalData: true,
				Force:           true,
			},
			objects: []runtime.Object{
				node,
				podWithEmptyDir,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				fmt.Sprintf("%s: bar; %s: bar", kLocalStorageWarning, kUnmanagedWarning),
				"pod \"bar\" removed (deleted)",
				"drained node \"node\"",
			},
		},
		{
			description: "empty node",
			nodes:       []string{"node"},
			options:     &DrainOptions{},
			objects: []runtime.Object{
				rc,
				node,
			},
			logEntries: []string{
				"cordoned node \"node\"",
				"drained node \"node\"",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			if len(test.logEntries) > 0 && len(test.expectedDrainError) > 0 {
				t.Error("logEntries set despite being masked by expectedDrainError")
			}

			client := fake.NewSimpleClientset(test.objects...)
			logger := capture.New()
			test.options.Logger = logger

			nodes, err := GetNodes(client.CoreV1().Nodes(), test.nodes, "")
			if err != nil {
				t.Fatal(err)
			}

			err = Drain(client, nodes, test.options)
			if err != nil {
				if len(test.expectedDrainError) == 0 {
					t.Fatal(err)
				} else if !regexp.MustCompile(test.expectedDrainError).MatchString(err.Error()) {
					t.Fatalf("%q does not match %q", err.Error(), test.expectedDrainError)
				}
				return
			} else if len(test.expectedDrainError) > 0 {
				t.Fatalf("expected error %q", test.expectedDrainError)
			}

			for i, expectedEntry := range test.logEntries {
				if i >= len(logger.Entries) {
					t.Errorf("missing expected entry %d: %q", i, expectedEntry)
					continue
				}
				actualEntry := logger.Entries[i]
				if actualEntry != expectedEntry {
					t.Errorf("unexpected entry %d: %q (expected %q)", i, actualEntry, expectedEntry)
				}
			}
			if len(logger.Entries) > len(test.logEntries) {
				t.Errorf("additional unexpected entries: %v", logger.Entries[len(test.logEntries):])
			}
		})
	}
}

func TestDeletePods(t *testing.T) {
	ifHasBeenCalled := map[string]bool{}
	tests := []struct {
		description       string
		interval          time.Duration
		timeout           time.Duration
		expectPendingPods bool
		expectError       bool
		expectedError     *error
		getPodFn          func(namespace, name string) (*corev1.Pod, error)
	}{
		{
			description:       "Wait for deleting to complete",
			interval:          100 * time.Millisecond,
			timeout:           10 * time.Second,
			expectPendingPods: false,
			expectError:       false,
			expectedError:     nil,
			getPodFn: func(namespace, name string) (*corev1.Pod, error) {
				oldPodMap, _ := createPods(false)
				newPodMap, _ := createPods(true)
				if oldPod, found := oldPodMap[name]; found {
					if _, ok := ifHasBeenCalled[name]; !ok {
						ifHasBeenCalled[name] = true
						return &oldPod, nil
					}
					if oldPod.ObjectMeta.Generation < 4 {
						newPod := newPodMap[name]
						return &newPod, nil
					}
					return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, name)

				}
				return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, name)
			},
		},
		{
			description:       "Deleting could timeout",
			interval:          200 * time.Millisecond,
			timeout:           3 * time.Second,
			expectPendingPods: true,
			expectError:       true,
			expectedError:     &wait.ErrWaitTimeout,
			getPodFn: func(namespace, name string) (*corev1.Pod, error) {
				oldPodMap, _ := createPods(false)
				if oldPod, found := oldPodMap[name]; found {
					return &oldPod, nil
				}
				return nil, fmt.Errorf("%q: not found", name)
			},
		},
		{
			description:       "Client error could be passed out",
			interval:          200 * time.Millisecond,
			timeout:           5 * time.Second,
			expectPendingPods: true,
			expectError:       true,
			expectedError:     nil,
			getPodFn: func(namespace, name string) (*corev1.Pod, error) {
				return nil, errors.New("This is a random error for testing")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			_, pods := createPods(false)
			pendingPods, err := waitForDelete(pods, test.interval, test.timeout, false, nil, test.getPodFn)

			if test.expectError {
				if err == nil {
					t.Fatalf("%s: unexpected non-error", test.description)
				} else if test.expectedError != nil {
					if *test.expectedError != err {
						t.Fatalf("%s: the error does not match expected error", test.description)
					}
				}
			}
			if !test.expectError && err != nil {
				t.Fatalf("%s: unexpected error", test.description)
			}
			if test.expectPendingPods && len(pendingPods) == 0 {
				t.Fatalf("%s: unexpected empty pods", test.description)
			}
			if !test.expectPendingPods && len(pendingPods) > 0 {
				t.Fatalf("%s: unexpected pending pods", test.description)
			}
		})
	}
}

func createPods(ifCreateNewPods bool) (map[string]corev1.Pod, []corev1.Pod) {
	podMap := make(map[string]corev1.Pod)
	podSlice := []corev1.Pod{}
	for i := 0; i < 8; i++ {
		var uid types.UID
		if ifCreateNewPods {
			uid = types.UID(i)
		} else {
			uid = types.UID(strconv.Itoa(i) + strconv.Itoa(i))
		}
		pod := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:       "pod" + strconv.Itoa(i),
				Namespace:  "default",
				UID:        uid,
				Generation: int64(i),
			},
		}
		podMap[pod.Name] = pod
		podSlice = append(podSlice, pod)
	}
	return podMap, podSlice
}

type MyReq struct {
	Request *http.Request
}

func defaultHeader() http.Header {
	header := http.Header{}
	header.Set("Content-Type", runtime.ContentTypeJSON)
	return header
}

func objBody(codec runtime.Codec, obj runtime.Object) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(runtime.EncodeOrDie(codec, obj))))
}

func stringBody(body string) io.ReadCloser {
	return ioutil.NopCloser(bytes.NewReader([]byte(body)))
}

func TestBuildDeleteOptions(t *testing.T) {
	tests := []struct {
		description    string
		podGracePeriod int64
		gracePeriod    int
		// -1 signals not set
		expected int64
	}{
		{
			description:    "library grace period seconds cannot exceed pod's",
			podGracePeriod: 30,
			gracePeriod:    600,
			expected:       30,
		},
		{
			description:    "library grace period negative doesn't set delete options",
			podGracePeriod: 30,
			gracePeriod:    -1,
			expected:       -1,
		},
		{
			description:    "library's grace period is honored if lower than pod's",
			podGracePeriod: 30,
			gracePeriod:    10,
			expected:       10,
		},
	}
	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			doptions := buildDeleteOptions(&corev1.Pod{ObjectMeta: metav1.ObjectMeta{DeletionGracePeriodSeconds: &test.podGracePeriod}}, test.gracePeriod)
			if doptions.GracePeriodSeconds != nil && *doptions.GracePeriodSeconds != test.expected {
				t.Fatalf("expected delete options grace period of %d, got %d instead", test.expected, *doptions.GracePeriodSeconds)
			}
			if doptions.GracePeriodSeconds != nil && test.expected == -1 {
				t.Fatalf("expected nil delete options grace period, got %d instead", *doptions.GracePeriodSeconds)
			}
		})
	}
}
