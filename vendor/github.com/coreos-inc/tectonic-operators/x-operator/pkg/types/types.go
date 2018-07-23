package types

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
)

// File defines a type that represents the raw manifest data.
type File struct {
	Path string
	Data []byte
}

// UpgradeDefinition defines the state for a version.
type UpgradeDefinition struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Version           string        `json:"version"`
	Items             []UpgradeSpec `json:"items"`
}

// UpgradeDefinitionPair is a pair of (current, target) upgrade definition.
// They corresponds to the on-disk manifests of the current and target version.
type UpgradeDefinitionPair struct {
	Current *UpgradeDefinition
	Target  *UpgradeDefinition
}

// UpgradeSpec is the object that stores various details for an object.
type UpgradeSpec struct {
	// Spec defines an object.
	Spec metav1.Object
	// UpgradeStrategy defines the strategy for upgrading the object.
	UpgradeStrategy constants.UpgradeStrategy
	// UpgradeBehaviour defines the behaviour for upgrading the object.
	UpgradeBehaviour constants.UpgradeBehaviour
}

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

// Component is responsible for updating
// a single component in the cluster.
type Component interface {
	metav1.Object

	// GetKind returns the kind of the underlying object.
	GetKind() string

	// Definition returns the underlying object.
	Definition() metav1.Object

	// Get fetches the cluster state for the underlying object.
	Get() (Component, error)

	// Create is the function that creates the component if not exists.
	Create() error

	// Delete deletes the object.
	Delete(options *metav1.DeleteOptions) error

	// List returns all the objects matching a selector in a namespace.
	List(namespace string, sel labels.Selector) ([]Component, error)

	// Upgrade is the function used to upgrade this component if exists.
	Upgrade(old Component, strategy constants.UpgradeStrategy) error

	// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
	CloneAndSanitize() (metav1.Object, error)

	// CreateOrUpgrade creates or upgrades component.
	CreateOrUpgrade(old Component, strategy constants.UpgradeStrategy) error

	// UpgradeIfExists upgrades only if object exists.
	// When the object is not found, upgrade is skipped.
	UpgradeIfExists(old Component, strategy constants.UpgradeStrategy) error
}
