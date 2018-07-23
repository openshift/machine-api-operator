package components

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
)

const migrationPollInterval = 1 * time.Second

// RunJobMigration runs the job type migration.
func RunJobMigration(client opclient.Interface, migration *batchv1.Job) error {
	namespace, name := migration.GetNamespace(), migration.GetName()

	j, err := client.KubernetesInterface().BatchV1().Jobs(namespace).Get(name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to get job %s/%s: %v", namespace, name, err)
	}

	if err == nil {
		glog.Infof("Found previous job, deleting before recreating", j.GetNamespace(), j.GetName())
		if err := client.KubernetesInterface().BatchV1().Jobs(namespace).Delete(name, &metav1.DeleteOptions{}); err != nil {
			return fmt.Errorf("failed to delete job %s/%s: %v", namespace, name, err)
		}
	}

	if _, err := client.KubernetesInterface().BatchV1().Jobs(namespace).Create(migration); err != nil {
		return fmt.Errorf("failed to create job %s/%s: %v", namespace, name, err)
	}

	// Wait for pod state.
	if err := wait.PollInfinite(migrationPollInterval, func() (bool, error) {
		j, err := client.KubernetesInterface().BatchV1().Jobs(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if j.Status.Succeeded > 0 {
			return true, nil
		}

		// Since we have filled in "activeDeadlineSeconds",
		// the Job will 'Active == 0' iff it exceeds the deadline.
		// Failed jobs will be recreated in the next run.
		if j.Status.Active == 0 && j.Status.Failed > 0 {
			reason := "DeadlineExceeded"
			message := "Job was active longer than specified deadline"
			if len(j.Status.Conditions) > 0 {
				reason, message = j.Status.Conditions[0].Reason, j.Status.Conditions[0].Message
			}
			return false, fmt.Errorf("deadline exceeded, reason: %q, message: %q", reason, message)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("migration job %s/%s didn't succeed: %v", namespace, name, err)
	}
	return nil
}
