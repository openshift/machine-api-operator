package versionhandler

import (
	"github.com/blang/semver"

	"github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
)

// UpgradeFromVersion will return a handler that will run if the CurrentVersion == version
// It's used to trigger a handler when it's leaving a specific version
func UpgradeFromVersion(v string, h HandlerFunc) HandlerFunc {
	return HandlerFunc(func(client client.Interface, version *types.AppVersion) error {
		ver, err := semver.Parse(v)
		if err != nil {
			return err
		}
		current, err := semver.Parse(version.Status.CurrentVersion)
		if err != nil {
			return err
		}

		if current.EQ(ver) {
			return h(client, version)
		}

		return nil
	})
}

// UpgradeBeforeVersion will return a handler that will run if the CurrentVersion < version <= TargetVersion
// It's used to trigger a handler before going into or past a specific version
func UpgradeBeforeVersion(v string, h HandlerFunc) HandlerFunc {
	return HandlerFunc(func(client client.Interface, version *types.AppVersion) error {
		ver, err := semver.Parse(v)
		if err != nil {
			return err
		}
		current, err := semver.Parse(version.Status.CurrentVersion)
		if err != nil {
			return err
		}
		target, err := semver.Parse(version.Status.TargetVersion)
		if err != nil {
			return err
		}

		if current.LT(ver) && target.GTE(ver) {
			return h(client, version)
		}

		return nil
	})
}
