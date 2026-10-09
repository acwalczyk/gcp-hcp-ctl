package regions

import (
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/discovery"
)

func TestFlattenRegions(t *testing.T) {
	manifest := &discovery.Manifest{
		SchemaVersion: "v1",
		Environment:   "integration",
		Regions: map[string]discovery.RegionInfo{
			"us-east1":    {OIDCIssuer: "https://oidc-east", PlatformAPIEndpoint: "https://api-east"},
			"us-central1": {OIDCIssuer: "https://oidc-central", PlatformAPIEndpoint: "https://api-central"},
		},
	}

	flattened := flattenRegions(manifest)

	if len(flattened) != 2 {
		t.Fatalf("expected 2 regions, got %d", len(flattened))
	}
	// Sorted by region name for deterministic output.
	if flattened[0].Region != "us-central1" || flattened[1].Region != "us-east1" {
		t.Errorf("regions not sorted: %+v", flattened)
	}
	if flattened[0].PlatformAPIEndpoint != "https://api-central" {
		t.Errorf("unexpected api endpoint: %q", flattened[0].PlatformAPIEndpoint)
	}
	if flattened[0].OIDCIssuer != "https://oidc-central" {
		t.Errorf("unexpected oidc issuer: %q", flattened[0].OIDCIssuer)
	}
}

func TestFlattenRegionsEmpty(t *testing.T) {
	flattened := flattenRegions(&discovery.Manifest{SchemaVersion: "v1"})
	if len(flattened) != 0 {
		t.Errorf("expected empty slice, got %d entries", len(flattened))
	}
}
