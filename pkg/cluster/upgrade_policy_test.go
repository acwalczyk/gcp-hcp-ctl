package cluster

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildUpgradePolicy(t *testing.T) {
	opts := upgradePolicyOptions{
		start:           "2026-10-03T02:00:00Z",
		durationMinutes: 240,
		frequency:       "weekly",
		days:            []string{"saturday"},
		exclusions:      []string{"holiday-freeze,2026-12-20T00:00:00Z,2027-01-03T00:00:00Z"},
	}

	policy, err := opts.build("upgrade-poc")
	if err != nil {
		t.Fatalf("building policy: %v", err)
	}

	if policy.Name != "upgrade-poc" {
		t.Errorf("expected policy name %q, got %q", "upgrade-poc", policy.Name)
	}
	if policy.Spec.ClusterID != "upgrade-poc" {
		t.Errorf("expected cluster ID %q, got %q", "upgrade-poc", policy.Spec.ClusterID)
	}
	if policy.Spec.MaintenanceWindow.DurationMinutes != 240 {
		t.Errorf("expected duration 240, got %d", policy.Spec.MaintenanceWindow.DurationMinutes)
	}
	if len(policy.Spec.MaintenanceExclusions) != 1 {
		t.Fatalf("expected one exclusion, got %d", len(policy.Spec.MaintenanceExclusions))
	}
	if policy.Spec.MaintenanceExclusions[0].Name != "holiday-freeze" {
		t.Errorf("expected exclusion name %q, got %q", "holiday-freeze", policy.Spec.MaintenanceExclusions[0].Name)
	}
}

func TestBuildUpgradePolicyRejectsInvalidExclusion(t *testing.T) {
	opts := upgradePolicyOptions{
		start:           "2026-10-03T02:00:00Z",
		durationMinutes: 240,
		frequency:       "weekly",
		days:            []string{"saturday"},
		exclusions:      []string{"not-a-valid-exclusion"},
	}

	if _, err := opts.build("upgrade-poc"); err == nil {
		t.Fatal("expected invalid exclusion to fail")
	}
}

func TestPrintUpgradePolicyYAML(t *testing.T) {
	policy, err := (&upgradePolicyOptions{
		start:           "2026-10-03T02:00:00Z",
		durationMinutes: 240,
		frequency:       "weekly",
		days:            []string{"saturday"},
	}).build("upgrade-poc")
	if err != nil {
		t.Fatalf("building policy: %v", err)
	}
	policy.TypeMeta.APIVersion = ""
	policy.TypeMeta.Kind = ""

	var output bytes.Buffer
	if err := printUpgradePolicy(&output, policy, "yaml"); err != nil {
		t.Fatalf("printing policy: %v", err)
	}

	got := output.String()
	for _, want := range []string{
		"apiVersion: gcp.managed.openshift.io/v1",
		"kind: ControlPlaneUpgradePolicy",
		"clusterID: upgrade-poc",
		"durationMinutes: 240",
		"daysOfWeek:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"typemeta:", "objectmeta:", "clusterid:"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("output contains Go field name %q:\n%s", unwanted, got)
		}
	}
}
