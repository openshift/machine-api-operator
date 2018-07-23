// Package constants defines public API constants that are used by x-operator clients to control
// the behavior of the x-operator when processing manifests.
package constants

const (
	// XOperatorManagedByLabelKey is the key for label that must exist on all manifests
	// that this operator handles
	XOperatorManagedByLabelKey = "tectonic-operators.coreos.com/managed-by"

	// XOperatorUpgradeStrategyAnnotationKey is the key that defines UpgradeStrategy for any UpgradeSpec.
	XOperatorUpgradeStrategyAnnotationKey = "tectonic-operators.coreos.com/upgrade-strategy"

	// XOperatorUpgradeBehaviourAnnotationKey is the key that defines UpgradeBehaviour for any UpgradeSpec.
	XOperatorUpgradeBehaviourAnnotationKey = "tectonic-operators.coreos.com/upgrade-behaviour"
)

// UpgradeBehaviour defines the behaviour for the upgrade for each UpgradeSpec.
// Default is CreateOrUpgrade.
type UpgradeBehaviour string

const (
	// UpgradeBehaviourCreateOrUpgrade defines that each UpgradeSpec is either created or updated.
	UpgradeBehaviourCreateOrUpgrade = "CreateOrUpgrade"
	// UpgradeBehaviourUpgradeIfExists defines that each UpgradeSpec is only updated if it already exists in the cluster.
	UpgradeBehaviourUpgradeIfExists = "UpgradeIfExists"
)

// UpgradeStrategy defines the strategy for upgrade for each UpgradeSpec.
// Default is Patch.
type UpgradeStrategy string

const (
	// UpgradeStrategyReplace defines that upgrades perform replace.
	UpgradeStrategyReplace = "Replace"
	// UpgradeStrategyPatch defines that upgrades perform 3-way merge patch.
	UpgradeStrategyPatch = "Patch"
	// UpgradeStrategyDeleteAndRecreate defines that upgrades delete the old object and recreate.
	// Might be useful when a change would potentially orphan children.
	UpgradeStrategyDeleteAndRecreate = "DeleteAndRecreate"
)
