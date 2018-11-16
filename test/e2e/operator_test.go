package e2e

import (
	"testing"
	"time"

	"context"
	"fmt"
	kappsapi "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	namespace = "openshift-cluster-api"
)

var (
	F *Framework
)

type Framework struct {
	Client client.Client
}

func newClient() error {
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		return err
	}
	if err != nil {
		return err
	}

	// TODO:(alberto) add schemes
	client, err := client.New(cfg, client.Options{})
	if err != nil {
		return err
	}
	F = &Framework{
		Client: client,
	}
	return nil
}

func TestMain(m *testing.M) {
	if err := newClient(); err != nil {
		fmt.Println("failed waiting for operator to start")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func TestOperatorAvailable(t *testing.T) {
	name := "machine-api-operator"
	key := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	err := wait.PollImmediate(1*time.Second, 1*time.Minute, func() (bool, error) {
		d := &kappsapi.Deployment{}
		if err := F.Client.Get(context.TODO(), key, d); err != nil {
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		t.Errorf("Expected: Operator available. Got: %v", err)
	}
}
