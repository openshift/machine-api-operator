package main

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ec2/ec2iface"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// AwsCredsSecretIDKey is secret key containing AWS KeyId
	AwsCredsSecretIDKey = "awsAccessKeyId"
	// AwsCredsSecretAccessKey is secret key containing AWS Secret Key
	AwsCredsSecretAccessKey = "awsSecretAccessKey"
)

// NewClient creates our client wrapper object for the actual AWS clients we use.
// For authentication the underlying clients will use either the cluster AWS credentials
// secret if defined (i.e. in the root cluster),
// otherwise the IAM profile of the master where the actuator will run. (target clusters)
func NewClient(kubeClient kubernetes.Interface, secretName, namespace, region string) (*AWSClient, error) {
	awsConfig := &aws.Config{Region: aws.String(region)}

	if secretName != "" {
		secret, err := kubeClient.CoreV1().Secrets(namespace).Get(secretName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		accessKeyID, ok := secret.Data[AwsCredsSecretIDKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				secretName, AwsCredsSecretIDKey)
		}
		secretAccessKey, ok := secret.Data[AwsCredsSecretAccessKey]
		if !ok {
			return nil, fmt.Errorf("AWS credentials secret %v did not contain key %v",
				secretName, AwsCredsSecretAccessKey)
		}

		awsConfig.Credentials = credentials.NewStaticCredentials(
			string(accessKeyID), string(secretAccessKey), "")
	}

	// Otherwise default to relying on the IAM role of the masters where the actuator is running:
	s, err := session.NewSession(awsConfig)
	if err != nil {
		return nil, err
	}

	return &AWSClient{
		ec2Client: ec2.New(s),
	}, nil
}

// AWSClient is the client to retrieve aws instances
type AWSClient struct {
	ec2Client ec2iface.EC2API
}

// GetInstances returns all instances that have a tag matching a clsuterid
func (client *AWSClient) getInstances(instanceStateFilter []*string, clusterID string) ([]*ec2.Instance, error) {
	requestFilters := []*ec2.Filter{
		{
			Name:   aws.String("tag:openshiftClusterID"),
			Values: []*string{aws.String(clusterID)},
		},
	}

	if instanceStateFilter != nil {
		requestFilters = append(requestFilters, &ec2.Filter{
			Name:   aws.String("instance-state-name"),
			Values: instanceStateFilter,
		})
	}

	// Query instances with our machine's name, and in running/pending state.
	request := &ec2.DescribeInstancesInput{
		Filters: requestFilters,
	}

	result, err := client.ec2Client.DescribeInstances(request)
	if err != nil {
		return []*ec2.Instance{}, err
	}

	instances := make([]*ec2.Instance, 0, len(result.Reservations))
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// GetRunningInstances returns instances that match InstanceStateNameRunning or InstanceStateNamePending
func (client *AWSClient) GetRunningInstances(clusterID string) ([]*ec2.Instance, error) {
	stateFilter := []*string{aws.String(ec2.InstanceStateNameRunning), aws.String(ec2.InstanceStateNamePending)}
	return client.getInstances(stateFilter, clusterID)
}
