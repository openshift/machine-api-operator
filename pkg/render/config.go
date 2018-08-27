package render

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	// Kind is the TypeMeta.Kind for the OperatorConfig.
	Kind = "MachineAPIOperatorConfig"

	// APIVersion is the TypeMeta.APIVersion for the OperatorConfig.
	APIVersion = "v1"
)

// OperatorConfig contains configuration for KAO managed add-ons
type OperatorConfig struct {
	metav1.TypeMeta `json:",inline"`

	AWSCredentialsSecret string `json:"awsCredentialsSecret"`

	ClusterConfig ClusterConfig `json:"clusterConfig"`
	MachineConfig MachineConfig `json:"machineConfig"`
}

type ClusterConfig struct {
	KeyPairName string `json:"keyPairName"`
	Region      string `json:"region"`
	VPCName     string `json:"vpcName,omitempty"`
}

type MachineConfig struct {
	AMI                string   `json:"ami"`
	AvailabilityZone   string   `json:"availabilityZone"`
	Subnet             string   `json:"subnet"`
	IAMInstanceProfile string   `json:"iamInstanceProfile"`
	SecurityGroups     []string `json:"securityGroups"`
}
