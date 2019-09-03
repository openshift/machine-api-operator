package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

func TestGenerateRandomPassword(t *testing.T) {
	pwd := generateRandomPassword()
	if pwd == "" {
		t.Errorf("Expected a valid string but got null")
	}
}

func newOperatorWithBaremetalConfig() *OperatorConfig {
	return &OperatorConfig{
		targetNamespace,
		Controllers{
			"docker.io/openshift/origin-aws-machine-controllers:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
		},
		BaremetalControllers{
			"quay.io/openshift/origin-baremetal-operator:v4.2.0",
			"quay.io/openshift/origin-ironic:v4.2.0",
			"quay.io/openshift/origin-ironic-inspector:v4.2.0",
			"quay.io/openshift/origin-ironic-ipa-downloader:v4.2.0",
			"quay.io/openshift/origin-ironic-rhcos-downloader:v4.2.0",
			"quay.io/openshift/origin-ironic-static-ip-manager:v4.2.0",
		},
	}
}

//Testing the case where the password does already exist
func TestCreateMariadbPasswordSecret(t *testing.T) {
	kubeClient := fakekube.NewSimpleClientset(nil...)
	operatorConfig := newOperatorWithBaremetalConfig()
	client := kubeClient.CoreV1()

	// First create a mariadb password secret
	if err := createMariadbPasswordSecret(kubeClient.CoreV1(), operatorConfig); err != nil {
		t.Fatalf("Failed to create first Mariadb password. %s ", err)
	}
	// Read and get Mariadb password from Secret just created.
	oldMaridbPassword, err := client.Secrets(operatorConfig.TargetNamespace).Get(baremetalSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failure getting the first Mariadb password that just got created.")
	}
	oldPassword, ok := oldMaridbPassword.StringData[baremetalSecretKey]
	if !ok || oldPassword == "" {
		t.Fatal("Failure reading first Mariadb password from Secret.")
	}

	// The pasword definitely exists. Try creating again.
	if err := createMariadbPasswordSecret(kubeClient.CoreV1(), operatorConfig); err != nil {
		t.Fatal("Failure creating second Mariadb password.")
	}
	newMaridbPassword, err := client.Secrets(operatorConfig.TargetNamespace).Get(baremetalSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failure getting the second Mariadb password.")
	}
	newPassword, ok := newMaridbPassword.StringData[baremetalSecretKey]
	if !ok || newPassword == "" {
		t.Fatal("Failure reading second Mariadb password from Secret.")
	}
	if oldPassword != newPassword {
		t.Fatalf("Both passwords do not match.")
	} else {
		t.Logf("First Mariadb password is being preserved over re-creation as expected.")
	}
}
