package webhooks

import (
	"testing"

	. "github.com/onsi/gomega"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewVSphereFailureDomainMachineVAP(t *testing.T) {
	g := NewWithT(t)

	policy := NewVSphereFailureDomainMachineVAP()
	g.Expect(policy).NotTo(BeNil())
	g.Expect(policy.Name).To(Equal(VAPMachineFailureDomainName))

	spec := policy.Spec

	// ParamKind must reference Machine.
	g.Expect(spec.ParamKind).NotTo(BeNil())
	g.Expect(spec.ParamKind.APIVersion).To(Equal("machine.openshift.io/v1beta1"))
	g.Expect(spec.ParamKind.Kind).To(Equal("Machine"))

	// Must fire only on UPDATE of infrastructures.
	g.Expect(spec.MatchConstraints).NotTo(BeNil())
	g.Expect(spec.MatchConstraints.ResourceRules).To(HaveLen(1))
	rule := spec.MatchConstraints.ResourceRules[0]
	g.Expect(rule.APIGroups).To(ConsistOf("config.openshift.io"))
	g.Expect(rule.APIVersions).To(ConsistOf("v1"))
	g.Expect(rule.Resources).To(ConsistOf("infrastructures"))
	g.Expect(rule.Operations).To(ConsistOf(admissionregistrationv1.Update))

	// Must have the platform match condition.
	g.Expect(spec.MatchConditions).To(HaveLen(1))
	g.Expect(spec.MatchConditions[0].Name).To(Equal("is-vsphere-platform"))
	g.Expect(spec.MatchConditions[0].Expression).To(ContainSubstring(`"VSphere"`))

	// Must define the three CEL variables.
	varNames := make([]string, 0, len(spec.Variables))
	for _, v := range spec.Variables {
		varNames = append(varNames, v.Name)
	}
	g.Expect(varNames).To(ConsistOf("fds", "machineRegion", "machineZone"))

	// Must have exactly one validation rule.
	g.Expect(spec.Validations).To(HaveLen(1))
	validation := spec.Validations[0]
	g.Expect(validation.Expression).To(ContainSubstring("variables.machineRegion"))
	g.Expect(validation.Expression).To(ContainSubstring("variables.machineZone"))
	g.Expect(validation.Expression).To(ContainSubstring("variables.fds.exists"))
	g.Expect(validation.MessageExpression).To(ContainSubstring("params.metadata.name"))
	g.Expect(validation.Reason).NotTo(BeNil())
	g.Expect(*validation.Reason).To(Equal(metav1.StatusReasonInvalid))

	// Failure policy must be Fail.
	g.Expect(spec.FailurePolicy).NotTo(BeNil())
	g.Expect(*spec.FailurePolicy).To(Equal(admissionregistrationv1.Fail))
}

func TestNewVSphereFailureDomainMachineVAPBinding(t *testing.T) {
	g := NewWithT(t)

	binding := NewVSphereFailureDomainMachineVAPBinding()
	g.Expect(binding).NotTo(BeNil())
	g.Expect(binding.Name).To(Equal(VAPMachineFailureDomainName))

	spec := binding.Spec
	g.Expect(spec.PolicyName).To(Equal(VAPMachineFailureDomainName))

	// ParamRef must select all Machines in openshift-machine-api.
	g.Expect(spec.ParamRef).NotTo(BeNil())
	g.Expect(spec.ParamRef.Namespace).To(Equal(openMachineAPINamespace))
	g.Expect(spec.ParamRef.Selector).To(Equal(&metav1.LabelSelector{}))
	g.Expect(spec.ParamRef.ParameterNotFoundAction).NotTo(BeNil())
	g.Expect(*spec.ParamRef.ParameterNotFoundAction).To(Equal(admissionregistrationv1.AllowAction))

	// Enforcement must be Deny.
	g.Expect(spec.ValidationActions).To(ConsistOf(admissionregistrationv1.Deny))
}

func TestNewVSphereFailureDomainCPMSVAP(t *testing.T) {
	g := NewWithT(t)

	policy := NewVSphereFailureDomainCPMSVAP()
	g.Expect(policy).NotTo(BeNil())
	g.Expect(policy.Name).To(Equal(VAPCPMSFailureDomainName))

	spec := policy.Spec

	// ParamKind must reference ControlPlaneMachineSet.
	g.Expect(spec.ParamKind).NotTo(BeNil())
	g.Expect(spec.ParamKind.APIVersion).To(Equal("machine.openshift.io/v1"))
	g.Expect(spec.ParamKind.Kind).To(Equal("ControlPlaneMachineSet"))

	// Must fire only on UPDATE of infrastructures.
	g.Expect(spec.MatchConstraints).NotTo(BeNil())
	g.Expect(spec.MatchConstraints.ResourceRules).To(HaveLen(1))
	rule := spec.MatchConstraints.ResourceRules[0]
	g.Expect(rule.APIGroups).To(ConsistOf("config.openshift.io"))
	g.Expect(rule.Resources).To(ConsistOf("infrastructures"))
	g.Expect(rule.Operations).To(ConsistOf(admissionregistrationv1.Update))

	// Must have the platform match condition.
	g.Expect(spec.MatchConditions).To(HaveLen(1))
	g.Expect(spec.MatchConditions[0].Name).To(Equal("is-vsphere-platform"))

	// Must define the two CEL variables.
	varNames := make([]string, 0, len(spec.Variables))
	for _, v := range spec.Variables {
		varNames = append(varNames, v.Name)
	}
	g.Expect(varNames).To(ConsistOf("fds", "cpmsFDs"))

	// The cpmsFDs variable must reference the correct CPMS template field path.
	for _, v := range spec.Variables {
		if v.Name == "cpmsFDs" {
			g.Expect(v.Expression).To(ContainSubstring("machines_v1beta1_machine_openshift_io"))
			g.Expect(v.Expression).To(ContainSubstring("failureDomains"))
			g.Expect(v.Expression).To(ContainSubstring("vsphere"))
		}
	}

	// Must have exactly one validation rule.
	g.Expect(spec.Validations).To(HaveLen(1))
	validation := spec.Validations[0]
	g.Expect(validation.Expression).To(ContainSubstring("variables.cpmsFDs"))
	g.Expect(validation.Expression).To(ContainSubstring("variables.fds.exists"))
	g.Expect(validation.MessageExpression).To(ContainSubstring("params.metadata.name"))
	g.Expect(validation.Reason).NotTo(BeNil())
	g.Expect(*validation.Reason).To(Equal(metav1.StatusReasonInvalid))

	// Failure policy must be Fail.
	g.Expect(spec.FailurePolicy).NotTo(BeNil())
	g.Expect(*spec.FailurePolicy).To(Equal(admissionregistrationv1.Fail))
}

func TestNewVSphereFailureDomainCPMSVAPBinding(t *testing.T) {
	g := NewWithT(t)

	binding := NewVSphereFailureDomainCPMSVAPBinding()
	g.Expect(binding).NotTo(BeNil())
	g.Expect(binding.Name).To(Equal(VAPCPMSFailureDomainName))

	spec := binding.Spec
	g.Expect(spec.PolicyName).To(Equal(VAPCPMSFailureDomainName))

	// ParamRef must select all CPMSes in openshift-machine-api.
	g.Expect(spec.ParamRef).NotTo(BeNil())
	g.Expect(spec.ParamRef.Namespace).To(Equal(openMachineAPINamespace))
	g.Expect(spec.ParamRef.Selector).To(Equal(&metav1.LabelSelector{}))
	g.Expect(spec.ParamRef.ParameterNotFoundAction).NotTo(BeNil())
	g.Expect(*spec.ParamRef.ParameterNotFoundAction).To(Equal(admissionregistrationv1.AllowAction))

	// Enforcement must be Deny.
	g.Expect(spec.ValidationActions).To(ConsistOf(admissionregistrationv1.Deny))
}

func TestNewVSphereFailureDomainMachineSetVAP(t *testing.T) {
	g := NewWithT(t)

	policy := NewVSphereFailureDomainMachineSetVAP()
	g.Expect(policy).NotTo(BeNil())
	g.Expect(policy.Name).To(Equal(VAPMachineSetFailureDomainName))

	spec := policy.Spec

	// ParamKind must reference MachineSet.
	g.Expect(spec.ParamKind).NotTo(BeNil())
	g.Expect(spec.ParamKind.APIVersion).To(Equal("machine.openshift.io/v1beta1"))
	g.Expect(spec.ParamKind.Kind).To(Equal("MachineSet"))

	// Must fire only on UPDATE of infrastructures.
	g.Expect(spec.MatchConstraints).NotTo(BeNil())
	g.Expect(spec.MatchConstraints.ResourceRules).To(HaveLen(1))
	rule := spec.MatchConstraints.ResourceRules[0]
	g.Expect(rule.APIGroups).To(ConsistOf("config.openshift.io"))
	g.Expect(rule.APIVersions).To(ConsistOf("v1"))
	g.Expect(rule.Resources).To(ConsistOf("infrastructures"))
	g.Expect(rule.Operations).To(ConsistOf(admissionregistrationv1.Update))

	// Must have the platform match condition.
	g.Expect(spec.MatchConditions).To(HaveLen(1))
	g.Expect(spec.MatchConditions[0].Name).To(Equal("is-vsphere-platform"))
	g.Expect(spec.MatchConditions[0].Expression).To(ContainSubstring(`"VSphere"`))

	// Must define the three CEL variables.
	varNames := make([]string, 0, len(spec.Variables))
	for _, v := range spec.Variables {
		varNames = append(varNames, v.Name)
	}
	g.Expect(varNames).To(ConsistOf("fds", "msRegion", "msZone"))

	// The msRegion and msZone variables must read from the template labels path using optional chaining.
	for _, v := range spec.Variables {
		switch v.Name {
		case "msRegion":
			g.Expect(v.Expression).To(ContainSubstring("params.?spec.template.metadata.labels"))
			g.Expect(v.Expression).To(ContainSubstring(machineRegionLabel))
		case "msZone":
			g.Expect(v.Expression).To(ContainSubstring("params.?spec.template.metadata.labels"))
			g.Expect(v.Expression).To(ContainSubstring(machineZoneLabel))
		}
	}

	// Must have exactly one validation rule.
	g.Expect(spec.Validations).To(HaveLen(1))
	validation := spec.Validations[0]
	g.Expect(validation.Expression).To(ContainSubstring("variables.msRegion"))
	g.Expect(validation.Expression).To(ContainSubstring("variables.msZone"))
	g.Expect(validation.Expression).To(ContainSubstring("variables.fds.exists"))
	g.Expect(validation.MessageExpression).To(ContainSubstring("params.metadata.name"))
	g.Expect(validation.Reason).NotTo(BeNil())
	g.Expect(*validation.Reason).To(Equal(metav1.StatusReasonInvalid))

	// Failure policy must be Fail.
	g.Expect(spec.FailurePolicy).NotTo(BeNil())
	g.Expect(*spec.FailurePolicy).To(Equal(admissionregistrationv1.Fail))
}

func TestNewVSphereFailureDomainMachineSetVAPBinding(t *testing.T) {
	g := NewWithT(t)

	binding := NewVSphereFailureDomainMachineSetVAPBinding()
	g.Expect(binding).NotTo(BeNil())
	g.Expect(binding.Name).To(Equal(VAPMachineSetFailureDomainName))

	spec := binding.Spec
	g.Expect(spec.PolicyName).To(Equal(VAPMachineSetFailureDomainName))

	// ParamRef must select all MachineSets in openshift-machine-api.
	g.Expect(spec.ParamRef).NotTo(BeNil())
	g.Expect(spec.ParamRef.Namespace).To(Equal(openMachineAPINamespace))
	g.Expect(spec.ParamRef.Selector).To(Equal(&metav1.LabelSelector{}))
	g.Expect(spec.ParamRef.ParameterNotFoundAction).NotTo(BeNil())
	g.Expect(*spec.ParamRef.ParameterNotFoundAction).To(Equal(admissionregistrationv1.AllowAction))

	// Enforcement must be Deny.
	g.Expect(spec.ValidationActions).To(ConsistOf(admissionregistrationv1.Deny))
}

// TestVAPMatchConstraintsServerDefaultedFields ensures that all three VAPs explicitly set
// MatchPolicy, NamespaceSelector, and ObjectSelector on MatchConstraints. When these fields
// are nil, the API server defaults them on storage. On the next sync cycle, resourceapply
// sees a diff between the desired spec (nil) and the stored spec (defaulted), triggering a
// spurious UPDATE every cycle. See SPLAT-2854.
func TestVAPMatchConstraintsServerDefaultedFields(t *testing.T) {
	expectedMatchPolicy := admissionregistrationv1.Equivalent
	expectedSelector := &metav1.LabelSelector{}

	tests := []struct {
		name   string
		policy *admissionregistrationv1.ValidatingAdmissionPolicy
	}{
		{"Machine VAP", NewVSphereFailureDomainMachineVAP()},
		{"CPMS VAP", NewVSphereFailureDomainCPMSVAP()},
		{"MachineSet VAP", NewVSphereFailureDomainMachineSetVAP()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			mc := tt.policy.Spec.MatchConstraints
			g.Expect(mc).NotTo(BeNil(), "MatchConstraints must not be nil")

			g.Expect(mc.MatchPolicy).NotTo(BeNil(),
				"MatchPolicy must be explicitly set to avoid spurious updates from API server defaulting")
			g.Expect(*mc.MatchPolicy).To(Equal(expectedMatchPolicy))

			g.Expect(mc.NamespaceSelector).To(Equal(expectedSelector),
				"NamespaceSelector must be explicitly set to avoid spurious updates from API server defaulting")

			g.Expect(mc.ObjectSelector).To(Equal(expectedSelector),
				"ObjectSelector must be explicitly set to avoid spurious updates from API server defaulting")
		})
	}
}

// TestVAPNamesAreConsistent ensures the binding policy names match the policy names.
func TestVAPNamesAreConsistent(t *testing.T) {
	g := NewWithT(t)

	machineVAP := NewVSphereFailureDomainMachineVAP()
	machineBinding := NewVSphereFailureDomainMachineVAPBinding()
	g.Expect(machineBinding.Spec.PolicyName).To(Equal(machineVAP.Name),
		"Machine binding PolicyName must match Machine VAP name")

	cpmsVAP := NewVSphereFailureDomainCPMSVAP()
	cpmsBinding := NewVSphereFailureDomainCPMSVAPBinding()
	g.Expect(cpmsBinding.Spec.PolicyName).To(Equal(cpmsVAP.Name),
		"CPMS binding PolicyName must match CPMS VAP name")

	machineSetVAP := NewVSphereFailureDomainMachineSetVAP()
	machineSetBinding := NewVSphereFailureDomainMachineSetVAPBinding()
	g.Expect(machineSetBinding.Spec.PolicyName).To(Equal(machineSetVAP.Name),
		"MachineSet binding PolicyName must match MachineSet VAP name")
}
