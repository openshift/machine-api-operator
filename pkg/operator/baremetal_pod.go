package operator

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"

	"golang.org/x/crypto/bcrypt"

	"github.com/golang/glog"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/utils/pointer"
)

const (
	baremetalDeploymentName    = "metal3"
	baremetalSharedVolume      = "metal3-shared"
	baremetalSecretName        = "metal3-mariadb-password"
	baremetalSecretKey         = "password"
	ironicCredentialsVolume    = "metal3-ironic-basic-auth"
	inspectorCredentialsVolume = "metal3-inspector-basic-auth"
	ironicUsernameKey          = "username"
	ironicPasswordKey          = "password"
	ironicHtpasswdKey          = "htpasswd"
	ironicConfigKey            = "auth-config"
	ironicSecretName           = "metal3-ironic-password"
	ironicUsername             = "ironic-user"
	inspectorSecretName        = "metal3-ironic-inspector-password"
	inspectorUsername          = "inspector-user"
	metal3AuthRootDir          = "/auth"
	htpasswdEnvVar             = "HTTP_BASIC_HTPASSWD"
)

var volumes = []corev1.Volume{
	{
		Name: baremetalSharedVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	},
	{
		Name: ironicCredentialsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: ironicSecretName,
				Items: []corev1.KeyToPath{
					{Key: ironicUsernameKey, Path: ironicUsernameKey},
					{Key: ironicPasswordKey, Path: ironicPasswordKey},
					{Key: ironicConfigKey, Path: ironicConfigKey},
				},
			},
		},
	},
	{
		Name: inspectorCredentialsVolume,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: inspectorSecretName,
				Items: []corev1.KeyToPath{
					{Key: ironicUsernameKey, Path: ironicUsernameKey},
					{Key: ironicPasswordKey, Path: ironicPasswordKey},
					{Key: ironicConfigKey, Path: ironicConfigKey},
				},
			},
		},
	},
}

var sharedVolumeMount = corev1.VolumeMount{
	Name:      baremetalSharedVolume,
	MountPath: "/shared",
}

var ironicCredentialsMount = corev1.VolumeMount{
	Name:      ironicCredentialsVolume,
	MountPath: metal3AuthRootDir + "/ironic",
	ReadOnly:  true,
}

var inspectorCredentialsMount = corev1.VolumeMount{
	Name:      inspectorCredentialsVolume,
	MountPath: metal3AuthRootDir + "/ironic-inspector",
	ReadOnly:  true,
}

func buildEnvVar(name string, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.EnvVar {
	value := getMetal3DeploymentConfig(name, baremetalProvisioningConfig)
	if value != nil {
		return corev1.EnvVar{
			Name:  name,
			Value: *value,
		}
	} else {
		return corev1.EnvVar{
			Name: name,
		}
	}
}

func setMariadbPassword() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "MARIADB_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: baremetalSecretName,
				},
				Key: baremetalSecretKey,
			},
		},
	}
}

func setIronicHtpasswdHash(name string, secretName string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
				Key: ironicHtpasswdKey,
			},
		},
	}
}

func generateRandomPassword() (string, error) {
	chars := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789")
	length := 16
	buf := make([]rune, length)
	numChars := big.NewInt(int64(len(chars)))
	for i := range buf {
		c, err := rand.Int(rand.Reader, numChars)
		if err != nil {
			return "", err
		}
		buf[i] = chars[c.Uint64()]
	}
	return string(buf), nil
}

func createMariadbPasswordSecret(client coreclientv1.SecretsGetter, config *OperatorConfig) error {
	glog.V(3).Info("Checking if the MariaDB password secret already exists")
	_, err := client.Secrets(config.TargetNamespace).Get(context.Background(), baremetalSecretName, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Secret does not already exist. So, create one.
	password, err := generateRandomPassword()
	if err != nil {
		return err
	}
	_, err = client.Secrets(config.TargetNamespace).Create(
		context.Background(),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      baremetalSecretName,
				Namespace: config.TargetNamespace,
			},
			StringData: map[string]string{
				baremetalSecretKey: password,
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func createIronicPasswordSecret(client coreclientv1.SecretsGetter, config *OperatorConfig, name string, username string, configSection string) error {
	glog.V(3).Info(fmt.Sprintf("Checking if the %s password secret already exists", name))
	_, err := client.Secrets(config.TargetNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if !apierrors.IsNotFound(err) {
		return err
	}

	// Secret does not already exist. So, create one.
	password, err := generateRandomPassword()
	if err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 5) // Use same cost as htpasswd default
	if err != nil {
		return err
	}
	// Change hash version from $2a$ to $2y$, as generated by htpasswd.
	// These are equivalent for our purposes.
	hash[2] = 'y'

	_, err = client.Secrets(config.TargetNamespace).Create(
		context.Background(),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: config.TargetNamespace,
			},
			StringData: map[string]string{
				ironicUsernameKey: username,
				ironicPasswordKey: password,
				ironicHtpasswdKey: fmt.Sprintf("%s:%s", username, hash),
				ironicConfigKey: fmt.Sprintf(`[%s]
auth_type = http_basic
username = %s
password = %s
`,
					configSection, username, password),
			},
		},
		metav1.CreateOptions{},
	)
	return err
}

func createMetal3PasswordSecrets(client coreclientv1.SecretsGetter, config *OperatorConfig) error {
	if err := createMariadbPasswordSecret(client, config); err != nil {
		glog.Error("Failed to create Mariadb password.")
		return err
	}
	if err := createIronicPasswordSecret(client, config, ironicSecretName, ironicUsername, "ironic"); err != nil {
		glog.Error("Failed to create Ironic password.")
		return err
	}
	if err := createIronicPasswordSecret(client, config, inspectorSecretName, inspectorUsername, "inspector"); err != nil {
		glog.Error("Failed to create Ironic Inspector password.")
		return err
	}
	return nil
}

// Return false on error or if "baremetal.openshift.io/owned" annotation set
func checkMetal3DeploymentMAOOwned(client appsclientv1.DeploymentsGetter, config *OperatorConfig) (bool, error) {
	existing, err := client.Deployments(config.TargetNamespace).Get(context.Background(), baremetalDeploymentName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		return false, err
	}
	if _, exists := existing.ObjectMeta.Annotations[cboOwnedAnnotation]; exists {
		return false, nil
	}
	return true, nil
}

// Return true if the baremetal clusteroperator exists
func checkForBaremetalClusterOperator(osClient osclientset.Interface) (bool, error) {
	_, err := osClient.ConfigV1().ClusterOperators().Get(context.Background(), cboClusterOperatorName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func newMetal3Deployment(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) *appsv1.Deployment {
	replicas := int32(1)
	template := newMetal3PodTemplateSpec(config, baremetalProvisioningConfig)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      baremetalDeploymentName,
			Namespace: config.TargetNamespace,
			Annotations: map[string]string{
				maoOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "controller",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"api":     "clusterapi",
					"k8s-app": "controller",
				},
			},
			Template: *template,
		},
	}
}

func newMetal3PodTemplateSpec(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) *corev1.PodTemplateSpec {
	initContainers := newMetal3InitContainers(config, baremetalProvisioningConfig)
	containers := newMetal3Containers(config, baremetalProvisioningConfig)
	tolerations := []corev1.Toleration{
		{
			Key:    "node-role.kubernetes.io/master",
			Effect: corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
		{
			Key:               "node.kubernetes.io/not-ready",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: pointer.Int64Ptr(120),
		},
		{
			Key:               "node.kubernetes.io/unreachable",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: pointer.Int64Ptr(120),
		},
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "controller",
			},
		},
		Spec: corev1.PodSpec{
			Volumes:           volumes,
			InitContainers:    initContainers,
			Containers:        containers,
			HostNetwork:       true,
			PriorityClassName: "system-node-critical",
			NodeSelector:      map[string]string{"node-role.kubernetes.io/master": ""},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: pointer.BoolPtr(false),
			},
			ServiceAccountName: "machine-api-controllers",
			Tolerations:        tolerations,
		},
	}
}

func newMetal3InitContainers(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) []corev1.Container {
	initContainers := []corev1.Container{
		{
			Name:            "metal3-ipa-downloader",
			Image:           config.BaremetalControllers.IronicIpaDownloader,
			Command:         []string{"/usr/local/bin/get-resource.sh"},
			ImagePullPolicy: "IfNotPresent",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.BoolPtr(true),
			},
			VolumeMounts: []corev1.VolumeMount{sharedVolumeMount},
			Env:          []corev1.EnvVar{},
		},
	}
	initContainers = append(initContainers, createInitContainerMachineOsDownloader(config, baremetalProvisioningConfig))
	initContainers = append(initContainers, createInitContainerStaticIpSet(config, baremetalProvisioningConfig))
	return initContainers
}

func createInitContainerMachineOsDownloader(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {
	initContainer := corev1.Container{
		Name:            "metal3-machine-os-downloader",
		Image:           config.BaremetalControllers.IronicMachineOsDownloader,
		Command:         []string{"/usr/local/bin/get-resource.sh"},
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		VolumeMounts: []corev1.VolumeMount{sharedVolumeMount},
		Env: []corev1.EnvVar{
			buildEnvVar("RHCOS_IMAGE_URL", baremetalProvisioningConfig),
		},
	}
	return initContainer
}

func createInitContainerStaticIpSet(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {
	initContainer := corev1.Container{
		Name:            "metal3-static-ip-set",
		Image:           config.BaremetalControllers.IronicStaticIpManager,
		Command:         []string{"/set-static-ip"},
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Env: []corev1.EnvVar{
			buildEnvVar("PROVISIONING_CIDR", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
		},
	}
	return initContainer
}

func newMetal3Containers(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) []corev1.Container {
	//Starting off with the metal3-baremetal-operator container
	containers := []corev1.Container{
		{
			Name:  "metal3-baremetal-operator",
			Image: config.BaremetalControllers.BaremetalOperator,
			Ports: []corev1.ContainerPort{
				{
					Name:          "metrics",
					ContainerPort: 60000,
				},
			},
			Command:         []string{"/baremetal-operator"},
			ImagePullPolicy: "IfNotPresent",
			VolumeMounts: []corev1.VolumeMount{
				ironicCredentialsMount,
				inspectorCredentialsMount,
			},
			Env: []corev1.EnvVar{
				{
					Name: "WATCH_NAMESPACE",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.namespace",
						},
					},
				},
				{
					Name: "POD_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.name",
						},
					},
				},
				{
					Name:  "OPERATOR_NAME",
					Value: "baremetal-operator",
				},
				buildEnvVar("DEPLOY_KERNEL_URL", baremetalProvisioningConfig),
				buildEnvVar("DEPLOY_RAMDISK_URL", baremetalProvisioningConfig),
				buildEnvVar("IRONIC_ENDPOINT", baremetalProvisioningConfig),
				buildEnvVar("IRONIC_INSPECTOR_ENDPOINT", baremetalProvisioningConfig),
				{
					Name:  "METAL3_AUTH_ROOT_DIR",
					Value: metal3AuthRootDir,
				},
			},
		},
	}
	if baremetalProvisioningConfig.ProvisioningNetwork != provisioningNetworkDisabled {
		containers = append(containers, createContainerMetal3Dnsmasq(config, baremetalProvisioningConfig))
	}
	containers = append(containers, createContainerMetal3Mariadb(config))
	containers = append(containers, createContainerMetal3Httpd(config, baremetalProvisioningConfig))
	containers = append(containers, createContainerMetal3IronicConductor(config, baremetalProvisioningConfig))
	containers = append(containers, createContainerMetal3IronicApi(config, baremetalProvisioningConfig))
	containers = append(containers, createContainerMetal3IronicInspector(config, baremetalProvisioningConfig))
	containers = append(containers, createContainerMetal3StaticIpManager(config, baremetalProvisioningConfig))
	return containers
}

func createContainerMetal3Dnsmasq(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-dnsmasq",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/rundnsmasq"},
		VolumeMounts: []corev1.VolumeMount{sharedVolumeMount},
		Env: []corev1.EnvVar{
			buildEnvVar("HTTP_PORT", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
			buildEnvVar("DHCP_RANGE", baremetalProvisioningConfig),
		},
	}
	return container
}

func createContainerMetal3Mariadb(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-mariadb",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runmariadb"},
		VolumeMounts: []corev1.VolumeMount{sharedVolumeMount},
		Env: []corev1.EnvVar{
			setMariadbPassword(),
		},
	}
	return container
}

func createContainerMetal3Httpd(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-httpd",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runhttpd"},
		VolumeMounts: []corev1.VolumeMount{sharedVolumeMount},
		Env: []corev1.EnvVar{
			buildEnvVar("HTTP_PORT", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_IP", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
		},
	}
	return container
}

func createContainerMetal3IronicConductor(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-ironic-conductor",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command: []string{"/bin/runironic-conductor"},
		VolumeMounts: []corev1.VolumeMount{
			sharedVolumeMount,
			inspectorCredentialsMount,
		},
		Env: []corev1.EnvVar{
			setMariadbPassword(),
			buildEnvVar("HTTP_PORT", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_IP", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
		},
	}
	return container
}

func createContainerMetal3IronicApi(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-ironic-api",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runironic-api"},
		VolumeMounts: []corev1.VolumeMount{sharedVolumeMount},
		Env: []corev1.EnvVar{
			setMariadbPassword(),
			buildEnvVar("HTTP_PORT", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_IP", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
			setIronicHtpasswdHash(htpasswdEnvVar, ironicSecretName),
		},
	}
	return container
}

func createContainerMetal3IronicInspector(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-ironic-inspector",
		Image:           config.BaremetalControllers.IronicInspector,
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		VolumeMounts: []corev1.VolumeMount{
			sharedVolumeMount,
			ironicCredentialsMount,
		},
		Env: []corev1.EnvVar{
			buildEnvVar("PROVISIONING_IP", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
			setIronicHtpasswdHash(htpasswdEnvVar, inspectorSecretName),
		},
	}
	return container
}

func createContainerMetal3StaticIpManager(config *OperatorConfig, baremetalProvisioningConfig BaremetalProvisioningConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-static-ip-manager",
		Image:           config.BaremetalControllers.IronicStaticIpManager,
		Command:         []string{"/refresh-static-ip"},
		ImagePullPolicy: "IfNotPresent",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Env: []corev1.EnvVar{
			buildEnvVar("PROVISIONING_CIDR", baremetalProvisioningConfig),
			buildEnvVar("PROVISIONING_INTERFACE", baremetalProvisioningConfig),
		},
	}
	return container
}
