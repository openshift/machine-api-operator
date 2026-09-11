package main

import (
	"testing"
	"time"
)

// Resync must stay well above the node-drain window so periodic
// reconciliation does not saturate the shared vCenter. Transient
// states are covered by the 30s requeue-until-running, not by resync.
func TestSyncPeriodFloor(t *testing.T) {
	if syncPeriod < 10*time.Minute {
		t.Fatalf("syncPeriod %s is below the 10m floor; do not lower it without re-analyzing vCenter API load", syncPeriod)
	}
}
