package types

import "k8s.io/api/core/v1"

// SecretModifier is a modifier function to be used when atomically
// updating a Secret.
type SecretModifier func(*v1.Secret) error
