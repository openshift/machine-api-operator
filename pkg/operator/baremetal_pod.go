package operator

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var baremetalConfigmap = "metal3-config"
var sharedVolume = "metal3-shared"

var volumes = []corev1.Volume{
	{
		Name: sharedVolume,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	},
}

var volumeMounts = []corev1.VolumeMount{
	{
		Name:      sharedVolume,
		MountPath: "/shared",
	},
}

func setEnvVar(name string, key string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: baremetalConfigmap,
				},
				Key: key,
			},
		},
	}
}

func setMariadbPassword() corev1.EnvVar {
	return corev1.EnvVar{
		Name: "MARIADB_PASSWORD",
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "mariadb-password",
				},
				Key: "password",
			},
		},
	}
}

func newMetal3Deployment(config *OperatorConfig) *appsv1.Deployment {
	replicas := int32(1)
	template := newMetal3PodTemplateSpec(config)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "metal3",
			Namespace: config.TargetNamespace,
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

func newMetal3PodTemplateSpec(config *OperatorConfig) *corev1.PodTemplateSpec {
	initContainers := newMetal3InitContainers(config)
	containers := newMetal3Containers(config)
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

func newMetal3InitContainers(config *OperatorConfig) []corev1.Container {
	initContainers := []corev1.Container{
		{
			Name:            "metal3-ipa-downloader",
			Image:           config.BaremetalControllers.IronicIpaDownloader,
			Command:         []string{"/usr/local/bin/get-resource.sh"},
			ImagePullPolicy: "Always",
			SecurityContext: &corev1.SecurityContext{
				Privileged: pointer.BoolPtr(true),
			},
			VolumeMounts: volumeMounts,
			Env: []corev1.EnvVar{
				setEnvVar("CACHEURL", "cache_url"),
			},
		},
	}
	initContainers = append(initContainers, createInitContainerMachineOsDownloader(config))
	initContainers = append(initContainers, createInitContainerStaticIpSet(config))
	return initContainers
}

func createInitContainerMachineOsDownloader(config *OperatorConfig) corev1.Container {
	initContainer := corev1.Container{
		Name:            "metal3-machine-os-downloader",
		Image:           config.BaremetalControllers.IronicMachineOsDownloader,
		Command:         []string{"/usr/local/bin/get-resource.sh"},
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setEnvVar("RHCOS_IMAGE_URL", "rhcos_image_url"),
			setEnvVar("CACHEURL", "cache_url"),
		},
	}
	return initContainer
}

func createInitContainerStaticIpSet(config *OperatorConfig) corev1.Container {
	initContainer := corev1.Container{
		Name:            "metal3-static-ip-set",
		Image:           config.BaremetalControllers.IronicStaticIpManager,
		Command:         []string{"/set-static-ip"},
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Env: []corev1.EnvVar{
			setEnvVar("PROVISIONING_IP", "provisioning_ip"),
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
		},
	}
	return initContainer
}

func newMetal3Containers(config *OperatorConfig) []corev1.Container {
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
			ImagePullPolicy: "Always",
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
				setEnvVar("DEPLOY_KERNEL_URL", "deploy_kernel_url"),
				setEnvVar("DEPLOY_RAMDISK_URL", "deploy_ramdisk_url"),
				setEnvVar("IRONIC_ENDPOINT", "ironic_endpoint"),
				setEnvVar("IRONIC_INSPECTOR_ENDPOINT", "ironic_inspector_endpoint"),
			},
		},
	}
	containers = append(containers, createContainerMetal3Dnsmasq(config))
	containers = append(containers, createContainerMetal3Mariadb(config))
	containers = append(containers, createContainerMetal3Httpd(config))
	containers = append(containers, createContainerMetal3IronicConductor(config))
	containers = append(containers, createContainerMetal3IronicApi(config))
	containers = append(containers, createContainerMetal3IronicInspector(config))
	containers = append(containers, createContainerMetal3StaticIpManager(config))
	return containers
}

func createContainerMetal3Dnsmasq(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-dnsmasq",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/rundnsmasq"},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setEnvVar("HTTP_PORT", "http_port"),
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
			setEnvVar("DHCP_RANGE", "dhcp_range"),
		},
	}
	return container
}

func createContainerMetal3Mariadb(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-mariadb",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runmariadb"},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setMariadbPassword(),
		},
	}
	return container
}

func createContainerMetal3Httpd(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-httpd",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runhttpd"},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setEnvVar("HTTP_PORT", "http_port"),
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
		},
	}
	return container
}

func createContainerMetal3IronicConductor(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-ironic-conductor",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runironic-conductor"},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setMariadbPassword(),
			setEnvVar("HTTP_PORT", "http_port"),
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
		},
	}
	return container
}

func createContainerMetal3IronicApi(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-ironic-api",
		Image:           config.BaremetalControllers.Ironic,
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Command:      []string{"/bin/runironic-api"},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setMariadbPassword(),
			setEnvVar("HTTP_PORT", "http_port"),
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
		},
	}
	return container
}

func createContainerMetal3IronicInspector(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-ironic-inspector",
		Image:           config.BaremetalControllers.IronicInspector,
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		VolumeMounts: volumeMounts,
		Env: []corev1.EnvVar{
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
		},
	}
	return container
}

func createContainerMetal3StaticIpManager(config *OperatorConfig) corev1.Container {

	container := corev1.Container{
		Name:            "metal3-static-ip-manager",
		Image:           config.BaremetalControllers.IronicStaticIpManager,
		Command:         []string{"/refresh-static-ip"},
		ImagePullPolicy: "Always",
		SecurityContext: &corev1.SecurityContext{
			Privileged: pointer.BoolPtr(true),
		},
		Env: []corev1.EnvVar{
			setEnvVar("PROVISIONING_IP", "provisioning_ip"),
			setEnvVar("PROVISIONING_INTERFACE", "provisioning_interface"),
		},
	}
	return container
}
