package main

import (
	"flag"
	"testing"
	"time"
)

// Resync must stay well above the node-drain window so periodic
// reconciliation does not saturate the shared vCenter. Transient
// states are covered by the 30s requeue-until-running, not by resync.
func TestSyncPeriodFloor(t *testing.T) {
	if syncPeriod != 30*time.Minute {
		t.Fatalf("syncPeriod %s is not 30m; expected 30m to reduce vCenter API load", syncPeriod)
	}
}

func TestMaxConcurrentReconcilesDefault(t *testing.T) {
	// The flag is registered in main(); register it in a test flagset
	// by calling the helper that wires flags (extracted below).
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	maxConcurrent := registerControllerFlags(fs) // see Step 3
	if *maxConcurrent != 10 {
		t.Errorf("default max-concurrent-reconciles = %d, want 10", *maxConcurrent)
	}
}

func TestMaxConcurrentReconcilesCustom(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	maxConcurrent := registerControllerFlags(fs)
	if err := fs.Parse([]string{"--max-concurrent-reconciles=5"}); err != nil {
		t.Fatalf("unexpected error parsing flags: %v", err)
	}
	if *maxConcurrent != 5 {
		t.Errorf("expected max-concurrent-reconciles = 5, got %d", *maxConcurrent)
	}
}
