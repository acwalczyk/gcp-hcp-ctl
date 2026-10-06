package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
)

func TestUpdateUpgradePolicyExclusions(t *testing.T) {
	for _, tc := range []struct {
		name        string
		flags       []string
		wantPresent bool
		wantCount   int
	}{
		{name: "preserve omitted exclusions"},
		{name: "replace exclusions", flags: []string{"--exclusion=holiday,2026-12-20T00:00:00Z,2027-01-03T00:00:00Z"}, wantPresent: true, wantCount: 1},
		{name: "clear exclusions", flags: []string{"--clear-exclusions"}, wantPresent: true},
		{name: "false clear preserves exclusions", flags: []string{"--clear-exclusions=false"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPatch || r.URL.Path != "/apis/gcp.managed.openshift.io/v1/namespaces/my-project/controlplaneupgradepolicies/upgrade-poc" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if got := r.Header.Get("Content-Type"); got != "application/merge-patch+json" {
					t.Errorf("Content-Type = %q", got)
				}
				var patch struct {
					Spec map[string]json.RawMessage `json:"spec"`
				}
				if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
					t.Errorf("decoding patch: %v", err)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				raw, present := patch.Spec["maintenanceExclusions"]
				if present != tc.wantPresent {
					t.Errorf("exclusions present = %v, want %v", present, tc.wantPresent)
				}
				if present {
					var exclusions []gcpv1.ControlPlaneMaintenanceExclusion
					if err := json.Unmarshal(raw, &exclusions); err != nil {
						t.Errorf("decoding exclusions: %v", err)
					}
					if len(exclusions) != tc.wantCount {
						t.Errorf("exclusions count = %d, want %d", len(exclusions), tc.wantCount)
					}
					if tc.wantCount == 0 && string(raw) != "[]" {
						t.Errorf("clear patch = %s, want []", raw)
					}
					if tc.wantCount == 1 && len(exclusions) == 1 && exclusions[0].Name != "holiday" {
						t.Errorf("exclusion name = %q", exclusions[0].Name)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"apiVersion":"gcp.managed.openshift.io/v1","kind":"ControlPlaneUpgradePolicy","metadata":{"name":"upgrade-poc"}}`))
			}))
			t.Cleanup(server.Close)
			client, err := platformapi.NewClientForTest(server.URL, "my-project")
			if err != nil {
				t.Fatal(err)
			}
			args := append([]string{"update-upgrade-policy", "upgrade-poc", "--start=2026-10-10T02:00:00Z", "--duration-minutes=240", "--day=saturday"}, tc.flags...)
			if err, _, _ := executeClusterTestCommand(t, client, context.Background(), args...); err != nil {
				t.Fatalf("updating policy: %v", err)
			}
			if requests != 1 {
				t.Errorf("requests = %d, want 1", requests)
			}
		})
	}
}

func TestUpdateUpgradePolicyRejectsConflictingExclusionFlags(t *testing.T) {
	err, _, _ := executeClusterTestCommand(t, nil, context.Background(),
		"update-upgrade-policy", "upgrade-poc", "--start=2026-10-10T02:00:00Z", "--duration-minutes=240", "--day=saturday",
		"--clear-exclusions", "--exclusion=holiday,2026-12-20T00:00:00Z,2027-01-03T00:00:00Z")
	if err == nil || !strings.Contains(err.Error(), "none of the others can be") {
		t.Fatalf("error = %v, want mutually exclusive flag error", err)
	}
}

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
