package main

import (
	"math/rand"
	"os"
	"time"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

const (
	minResyncPeriod = 10 * time.Minute
)

func resyncPeriod() func() time.Duration {
	return func() time.Duration {
		factor := rand.Float64() + 1
		return time.Duration(float64(minResyncPeriod.Nanoseconds()) * factor)
	}
}

// CreateResourceLock returns an interface for the resource lock.
func CreateResourceLock(cb *ClientBuilder, componentNamespace, componentName string) resourcelock.Interface {
	recorder := record.
		NewBroadcaster().
		NewRecorder(scheme.Scheme, v1.EventSource{Component: componentName})

	id, err := os.Hostname()
	if err != nil {
		klog.Fatalf("Error creating lock: %v", err)
	}

	// add a uniquifier so that two processes on the same host don't accidentally both become active
	id = id + "_" + string(uuid.NewUUID())

	// Set up Multilock for leader election. This Multilock is here for the
	// transitionary period from configmaps to leases see:
	// https://github.com/kubernetes-sigs/controller-runtime/pull/1144#discussion_r480173688.
	// and
	// https://github.com/kubernetes-sigs/controller-runtime/blob/196828e54e4210497438671b2b449522c004db5c/pkg/manager/manager.go#L144-L175.
	ml, err := resourcelock.New(resourcelock.ConfigMapsLeasesResourceLock,
		componentNamespace,
		componentName,
		cb.KubeClientOrDie("leader-election").CoreV1(),
		cb.KubeClientOrDie("leader-election").CoordinationV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: recorder,
		},
	)
	if err != nil {
		klog.Fatalf("Error creating lock: %v", err)
	}

	return ml
}
