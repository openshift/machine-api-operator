package webhooks

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	// VAPMachineFailureDomainName is the name of the ValidatingAdmissionPolicy that guards
	// against removing a vSphere failure domain that is still referenced by a Machine.
	VAPMachineFailureDomainName = "vsphere-failure-domain-in-use-by-machine"

	// VAPCPMSFailureDomainName is the name of the ValidatingAdmissionPolicy that guards
	// against removing a vSphere failure domain that is still referenced by a ControlPlaneMachineSet.
	VAPCPMSFailureDomainName = "vsphere-failure-domain-in-use-by-cpms"

	// VAPMachineSetFailureDomainName is the name of the ValidatingAdmissionPolicy that guards
	// against removing a vSphere failure domain that is still referenced by a MachineSet (including
	// MachineSets with zero replicas that would have no running Machines to catch the check).
	VAPMachineSetFailureDomainName = "vsphere-failure-domain-in-use-by-machineset"

	// machineRegionLabel is the label used to identify the region a Machine belongs to.
	machineRegionLabel = "machine.openshift.io/region"

	// machineZoneLabel is the label used to identify the availability zone a Machine belongs to.
	machineZoneLabel = "machine.openshift.io/zone"

	// vspherePlatformType is the platform type string as used in infrastructure.status.platformStatus.type.
	vspherePlatformType = "VSphere"

	// openMachineAPINamespace is the namespace where Machines and CPMS live.
	openMachineAPINamespace = "openshift-machine-api"
)

var (
	// vapDenyAction is the enforcement action that denies the admission request.
	vapDenyAction = admissionregistrationv1.Deny

	// vapParamNotFoundAllow means: if no param object exists (e.g. no Machines yet), allow the infra update.
	vapParamNotFoundAllow = admissionregistrationv1.AllowAction
)

// NewVSphereFailureDomainMachineVAP returns a ValidatingAdmissionPolicy that prevents
// an infrastructure/cluster UPDATE from removing a vSphere failure domain that is still
// referenced by at least one Machine (identified via machine.openshift.io/region and
// machine.openshift.io/zone labels).
//
// The policy fires on every UPDATE of infrastructures.config.openshift.io. It is evaluated
// once per Machine that exists in the openshift-machine-api namespace (param binding).
// If any Machine's region+zone labels match a failure domain being removed, admission is denied.
func NewVSphereFailureDomainMachineVAP() *admissionregistrationv1.ValidatingAdmissionPolicy {
	failurePolicy := admissionregistrationv1.Fail

	return &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: VAPMachineFailureDomainName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			// The param object is a Machine (one evaluation per Machine).
			ParamKind: &admissionregistrationv1.ParamKind{
				APIVersion: "machine.openshift.io/v1beta1",
				Kind:       "Machine",
			},
			// Fire on UPDATE of the Infrastructure CR only.
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"config.openshift.io"},
								APIVersions: []string{"v1"},
								Resources:   []string{"infrastructures"},
							},
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Update,
							},
						},
					},
				},
			},
			// Only evaluate when the cluster is running on vSphere.
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       "is-vsphere-platform",
					Expression: `object.?status.platformStatus.type.orValue("") == "` + vspherePlatformType + `"`,
				},
			},
			// Reusable sub-expressions.
			Variables: []admissionregistrationv1.Variable{
				{
					// fds: the failure domains list from the incoming (updated) Infrastructure spec.
					Name:       "fds",
					Expression: `object.?spec.platformSpec.vsphere.failureDomains.orValue([])`,
				},
				{
					// machineRegion: the region label of the Machine param (empty string if absent).
					Name:       "machineRegion",
					Expression: `params.?metadata.labels["` + machineRegionLabel + `"].orValue("")`,
				},
				{
					// machineZone: the zone label of the Machine param (empty string if absent).
					Name:       "machineZone",
					Expression: `params.?metadata.labels["` + machineZoneLabel + `"].orValue("")`,
				},
			},
			// Core validation: the Machine's region+zone must still exist in the updated infra spec.
			Validations: []admissionregistrationv1.Validation{
				{
					// Pass when:
					//   - Machine has no region/zone label (not a failure-domain-managed Machine), OR
					//   - The failure domain is still present in the incoming spec.
					Expression: `variables.machineRegion == "" || variables.machineZone == "" ||
variables.fds.exists(fd,
  fd.region == variables.machineRegion && fd.zone == variables.machineZone
)`,
					MessageExpression: `"Infrastructure update would remove vSphere failure domain (region=" + variables.machineRegion + ", zone=" + variables.machineZone + ") that is still in use by Machine '" + params.metadata.name + "'"`,
					Reason:            ptr.To(metav1.StatusReasonInvalid),
				},
			},
			// Hard fail if the policy itself errors (e.g. param parse failure).
			FailurePolicy: &failurePolicy,
		},
	}
}

// NewVSphereFailureDomainMachineVAPBinding returns the ValidatingAdmissionPolicyBinding that
// connects the Machine VAP to all Machines in the openshift-machine-api namespace.
// The policy is evaluated once per Machine (param).
func NewVSphereFailureDomainMachineVAPBinding() *admissionregistrationv1.ValidatingAdmissionPolicyBinding {
	return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: VAPMachineFailureDomainName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			// Reference the policy defined above.
			PolicyName: VAPMachineFailureDomainName,
			// Param: iterate over all Machines in openshift-machine-api.
			ParamRef: &admissionregistrationv1.ParamRef{
				// Empty selector matches all Machines.
				Selector:                &metav1.LabelSelector{},
				Namespace:               openMachineAPINamespace,
				ParameterNotFoundAction: &vapParamNotFoundAllow,
			},
			// Deny if any validation fails.
			ValidationActions: []admissionregistrationv1.ValidationAction{
				vapDenyAction,
			},
		},
	}
}

// NewVSphereFailureDomainCPMSVAP returns a ValidatingAdmissionPolicy that prevents
// an infrastructure/cluster UPDATE from removing a vSphere failure domain that is still
// referenced by a ControlPlaneMachineSet (CPMS) by failure domain name.
//
// The CPMS references failure domains by the Name field of VSpherePlatformFailureDomainSpec.
// The policy fires on every UPDATE of infrastructures.config.openshift.io and is evaluated
// once per ControlPlaneMachineSet in the openshift-machine-api namespace.
func NewVSphereFailureDomainCPMSVAP() *admissionregistrationv1.ValidatingAdmissionPolicy {
	failurePolicy := admissionregistrationv1.Fail

	return &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: VAPCPMSFailureDomainName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			// The param object is a ControlPlaneMachineSet.
			ParamKind: &admissionregistrationv1.ParamKind{
				APIVersion: "machine.openshift.io/v1",
				Kind:       "ControlPlaneMachineSet",
			},
			// Fire on UPDATE of the Infrastructure CR only.
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"config.openshift.io"},
								APIVersions: []string{"v1"},
								Resources:   []string{"infrastructures"},
							},
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Update,
							},
						},
					},
				},
			},
			// Only evaluate when the cluster is running on vSphere.
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       "is-vsphere-platform",
					Expression: `object.?status.platformStatus.type.orValue("") == "` + vspherePlatformType + `"`,
				},
			},
			// Reusable sub-expressions.
			Variables: []admissionregistrationv1.Variable{
				{
					// fds: failure domains from the incoming (updated) Infrastructure spec.
					Name:       "fds",
					Expression: `object.?spec.platformSpec.vsphere.failureDomains.orValue([])`,
				},
				{
					// cpmsFDs: the list of vSphere failure domain names referenced by the CPMS param.
					// The CPMS template field path is:
					//   spec.template.machines_v1beta1_machine_openshift_io.failureDomains.vsphere[*].name
					Name: "cpmsFDs",
					Expression: `(has(params.spec.template.machines_v1beta1_machine_openshift_io) &&
 has(params.spec.template.machines_v1beta1_machine_openshift_io.failureDomains) &&
 has(params.spec.template.machines_v1beta1_machine_openshift_io.failureDomains.vsphere))
  ? params.spec.template.machines_v1beta1_machine_openshift_io.failureDomains.vsphere
  : []`,
				},
			},
			// Core validation: every CPMS failure domain name must still exist in the updated infra spec.
			Validations: []admissionregistrationv1.Validation{
				{
					// Pass when:
					//   - CPMS has no vSphere failure domains configured (empty list), OR
					//   - Every CPMS failure domain name is still present in the incoming infra spec.
					Expression: `variables.cpmsFDs.size() == 0 ||
variables.cpmsFDs.all(cpmsfd,
  variables.fds.exists(infrafd, infrafd.name == cpmsfd.name)
)`,
					MessageExpression: `"Infrastructure update would remove vSphere failure domain(s) still referenced by ControlPlaneMachineSet '" + params.metadata.name + "': [" + variables.cpmsFDs.filter(cpmsfd, !variables.fds.exists(infrafd, infrafd.name == cpmsfd.name)).map(cpmsfd, cpmsfd.name).join(", ") + "]"`,
					Reason:            ptr.To(metav1.StatusReasonInvalid),
				},
			},
			// Hard fail if the policy itself errors.
			FailurePolicy: &failurePolicy,
		},
	}
}

// NewVSphereFailureDomainCPMSVAPBinding returns the ValidatingAdmissionPolicyBinding that
// connects the CPMS VAP to all ControlPlaneMachineSets in the openshift-machine-api namespace.
func NewVSphereFailureDomainCPMSVAPBinding() *admissionregistrationv1.ValidatingAdmissionPolicyBinding {
	return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: VAPCPMSFailureDomainName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			// Reference the CPMS policy.
			PolicyName: VAPCPMSFailureDomainName,
			// Param: iterate over all ControlPlaneMachineSets in openshift-machine-api.
			ParamRef: &admissionregistrationv1.ParamRef{
				Selector:                &metav1.LabelSelector{},
				Namespace:               openMachineAPINamespace,
				ParameterNotFoundAction: &vapParamNotFoundAllow,
			},
			// Deny if any validation fails.
			ValidationActions: []admissionregistrationv1.ValidationAction{
				vapDenyAction,
			},
		},
	}
}

// NewVSphereFailureDomainMachineSetVAP returns a ValidatingAdmissionPolicy that prevents an
// Infrastructure update from removing a vSphere failure domain that is still referenced by at
// least one MachineSet (identified via machine.openshift.io/region and
// machine.openshift.io/zone labels on the MachineSet template). This covers MachineSets with
// zero replicas, which would otherwise have no running Machines for the Machine VAP to check.
func NewVSphereFailureDomainMachineSetVAP() *admissionregistrationv1.ValidatingAdmissionPolicy {
	failurePolicy := admissionregistrationv1.Fail
	return &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name: VAPMachineSetFailureDomainName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicySpec{
			// Param: each evaluation receives one MachineSet as a param object.
			ParamKind: &admissionregistrationv1.ParamKind{
				APIVersion: "machine.openshift.io/v1beta1",
				Kind:       "MachineSet",
			},
			// Trigger: UPDATE of the Infrastructure CR only.
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"config.openshift.io"},
								APIVersions: []string{"v1"},
								Resources:   []string{"infrastructures"},
							},
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Update,
							},
						},
					},
				},
			},
			// Only evaluate when the cluster is running on vSphere.
			MatchConditions: []admissionregistrationv1.MatchCondition{
				{
					Name:       "is-vsphere-platform",
					Expression: `object.?status.platformStatus.type.orValue("") == "` + vspherePlatformType + `"`,
				},
			},
			// Reusable sub-expressions.
			Variables: []admissionregistrationv1.Variable{
				{
					// fds: the failure domains list from the incoming (updated) Infrastructure spec.
					Name:       "fds",
					Expression: `object.?spec.platformSpec.vsphere.failureDomains.orValue([])`,
				},
				{
					// msRegion: the region label of the MachineSet template (empty string if absent).
					Name:       "msRegion",
					Expression: `params.?spec.template.metadata.labels["` + machineRegionLabel + `"].orValue("")`,
				},
				{
					// msZone: the zone label of the MachineSet template (empty string if absent).
					Name:       "msZone",
					Expression: `params.?spec.template.metadata.labels["` + machineZoneLabel + `"].orValue("")`,
				},
			},
			// Core validation: the MachineSet template's region+zone must still exist in the updated infra spec.
			Validations: []admissionregistrationv1.Validation{
				{
					// MachineSet has no region/zone label in its template (not a failure-domain-managed
					// MachineSet), OR the failure domain is still present in the incoming spec.
					Expression:        `variables.msRegion == "" || variables.msZone == "" || variables.fds.exists(fd, fd.region == variables.msRegion && fd.zone == variables.msZone)`,
					MessageExpression: `"Infrastructure update would remove vSphere failure domain (region=" + variables.msRegion + ", zone=" + variables.msZone + ") that is still in use by MachineSet '" + params.metadata.name + "'"`,
					Reason:            ptr.To(metav1.StatusReasonInvalid),
				},
			},
			// Hard fail if the VAP itself cannot be evaluated.
			FailurePolicy: &failurePolicy,
		},
	}
}

// NewVSphereFailureDomainMachineSetVAPBinding returns a ValidatingAdmissionPolicyBinding that
// connects the MachineSet VAP to all MachineSets in the openshift-machine-api namespace.
func NewVSphereFailureDomainMachineSetVAPBinding() *admissionregistrationv1.ValidatingAdmissionPolicyBinding {
	return &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: VAPMachineSetFailureDomainName,
		},
		Spec: admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			// Reference the MachineSet policy.
			PolicyName: VAPMachineSetFailureDomainName,
			// Param: iterate over all MachineSets in openshift-machine-api.
			ParamRef: &admissionregistrationv1.ParamRef{
				Selector:                &metav1.LabelSelector{},
				Namespace:               openMachineAPINamespace,
				ParameterNotFoundAction: &vapParamNotFoundAllow,
			},
			// Deny if any validation fails.
			ValidationActions: []admissionregistrationv1.ValidationAction{
				vapDenyAction,
			},
		},
	}
}
