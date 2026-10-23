package network

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"google.golang.org/api/compute/v1"
)

func TestShouldCreateFirewallRule(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		noFirewallRule bool
		expected       bool
	}{
		{
			name:     "When version is 5.0.0-ec.6 it should create the rule because that payload predates GCP-1221",
			version:  "5.0.0-ec.6",
			expected: true,
		},
		{
			name:     "When version is a 5.1 prerelease it should create the rule because prereleases sort below 5.1.0",
			version:  "5.1.0-ec.1",
			expected: true,
		},
		{
			name:     "When version is 5.1.0 it should not create the rule because CPO owns it",
			version:  "5.1.0",
			expected: false,
		},
		{
			name:     "When version is newer than 5.1.0 it should not create the rule",
			version:  "5.2.0",
			expected: false,
		},
		{
			name:     "When version is unset it should create the rule as the safe default",
			version:  "",
			expected: true,
		},
		{
			name:     "When version is a 4.y release it should create the rule",
			version:  "4.22.0-rc.5",
			expected: true,
		},
		{
			name:     "When version is already v-prefixed it should be compared the same way",
			version:  "v5.1.0",
			expected: false,
		},
		{
			name:     "When version is a prerelease of a later patch it should not create the rule",
			version:  "5.1.1-ec.1",
			expected: false,
		},
		{
			name:     "When version carries build metadata it should compare equal to the release",
			version:  "5.1.0+build.1",
			expected: false,
		},
		{
			name:           "When the opt-out flag is set it should not create the rule for a pre-5.1 version",
			version:        "5.0.0-ec.6",
			noFirewallRule: true,
			expected:       false,
		},
		{
			name:           "When the opt-out flag is set it should not create the rule for an unknown version",
			version:        "",
			noFirewallRule: true,
			expected:       false,
		},
		{
			name:           "When the opt-out flag is set it should not create the rule for a 5.1+ version",
			version:        "5.2.0",
			noFirewallRule: true,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldCreateFirewallRule(tt.version, tt.noFirewallRule)
			if got != tt.expected {
				t.Errorf("expected shouldCreateFirewallRule(%q, %v) to be %v, got %v", tt.version, tt.noFirewallRule, tt.expected, got)
			}
		})
	}
}

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{name: "When version is empty it should be valid", version: "", expected: true},
		{name: "When version is a GA release it should be valid", version: "5.1.0", expected: true},
		{name: "When version is a prerelease it should be valid", version: "5.0.0-ec.6", expected: true},
		{name: "When version has a v prefix it should be valid", version: "v5.1.0", expected: true},
		{name: "When version is a shorthand minor release it should be valid", version: "5.1", expected: true},
		{name: "When version is not semver it should be invalid", version: "not-a-version", expected: false},
		{name: "When version is empty string with spaces it should be invalid", version: " ", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isValidVersion(tt.version); got != tt.expected {
				t.Errorf("expected isValidVersion(%q) to be %v, got %v", tt.version, tt.expected, got)
			}
		})
	}
}

func TestDesiredAllowed(t *testing.T) {
	tests := []struct {
		name          string
		networkType   string
		nodePortRange string
		expectedTCP   []string
		expectedUDP   []string
	}{
		{
			name:          "When the network type is OVNKubernetes it should add the Geneve overlay UDP port",
			networkType:   NetworkTypeOVNKubernetes,
			nodePortRange: DefaultNodePortRange,
			expectedTCP:   []string{"10250", "9000-9999", "30000-32767"},
			expectedUDP:   []string{"9000-9999", "30000-32767", "6081"},
		},
		{
			name:          "When the network type is OpenShiftSDN it should omit the Geneve overlay port",
			networkType:   NetworkTypeOpenShiftSDN,
			nodePortRange: DefaultNodePortRange,
			expectedTCP:   []string{"10250", "9000-9999", "30000-32767"},
			expectedUDP:   []string{"9000-9999", "30000-32767"},
		},
		{
			name:          "When a custom node port range is set it should be used in both protocols",
			networkType:   NetworkTypeOVNKubernetes,
			nodePortRange: "30000-31000",
			expectedTCP:   []string{"10250", "9000-9999", "30000-31000"},
			expectedUDP:   []string{"9000-9999", "30000-31000", "6081"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			allowed := desiredAllowed(tt.networkType, tt.nodePortRange)

			if len(allowed) != 2 {
				t.Fatalf("expected 2 allowed rules (tcp, udp), got %d", len(allowed))
			}
			if allowed[0].IPProtocol != "tcp" {
				t.Errorf("expected first rule protocol to be tcp, got %q", allowed[0].IPProtocol)
			}
			if !slices.Equal(allowed[0].Ports, tt.expectedTCP) {
				t.Errorf("expected tcp ports %v, got %v", tt.expectedTCP, allowed[0].Ports)
			}
			if allowed[1].IPProtocol != "udp" {
				t.Errorf("expected second rule protocol to be udp, got %q", allowed[1].IPProtocol)
			}
			if !slices.Equal(allowed[1].Ports, tt.expectedUDP) {
				t.Errorf("expected udp ports %v, got %v", tt.expectedUDP, allowed[1].Ports)
			}
		})
	}
}

func TestDesiredFirewall(t *testing.T) {
	const (
		infraID   = "test-infra"
		networkSL = "https://www.googleapis.com/compute/v1/projects/test-project/global/networks/test-infra-network"
	)

	firewall := desiredFirewall(infraID, networkSL, NetworkTypeOVNKubernetes, DefaultNodePortRange)

	if firewall.Name != "test-infra-internal-cluster" {
		t.Errorf("expected name %q, got %q", "test-infra-internal-cluster", firewall.Name)
	}
	if firewall.Network != networkSL {
		t.Errorf("expected network %q, got %q", networkSL, firewall.Network)
	}
	if firewall.Direction != "INGRESS" {
		t.Errorf("expected direction INGRESS, got %q", firewall.Direction)
	}
	if firewall.Priority != 1000 {
		t.Errorf("expected priority 1000, got %d", firewall.Priority)
	}
	if firewall.Disabled {
		t.Error("expected the rule to be enabled")
	}
	if !slices.Equal(firewall.SourceTags, []string{"test-infra-worker"}) {
		t.Errorf("expected source tags [test-infra-worker], got %v", firewall.SourceTags)
	}
	if !slices.Equal(firewall.TargetTags, []string{"test-infra-worker"}) {
		t.Errorf("expected target tags [test-infra-worker], got %v", firewall.TargetTags)
	}
	if firewall.Description != "" {
		t.Errorf("expected no description before the marker is applied, got %q", firewall.Description)
	}

	if len(firewall.SourceRanges) != 0 || len(firewall.DestinationRanges) != 0 {
		t.Errorf("expected no CIDR selectors, got source %v destination %v", firewall.SourceRanges, firewall.DestinationRanges)
	}
	if len(firewall.SourceServiceAccounts) != 0 || len(firewall.TargetServiceAccounts) != 0 {
		t.Errorf("expected no service account selectors, got source %v target %v", firewall.SourceServiceAccounts, firewall.TargetServiceAccounts)
	}

	expectedForceSend := []string{
		"Disabled",
		"SourceRanges",
		"DestinationRanges",
		"SourceServiceAccounts",
		"TargetServiceAccounts",
	}
	if !slices.Equal(firewall.ForceSendFields, expectedForceSend) {
		t.Errorf("expected ForceSendFields %v, got %v", expectedForceSend, firewall.ForceSendFields)
	}
}

func TestDesiredFirewallCustomNodePortRange(t *testing.T) {
	tests := []struct {
		name              string
		networkType       string
		expectedGeneveUDP bool
	}{
		{name: "When the network type is OVNKubernetes it should keep the Geneve port", networkType: NetworkTypeOVNKubernetes, expectedGeneveUDP: true},
		{name: "When the network type is OpenShiftSDN it should omit the Geneve port", networkType: NetworkTypeOpenShiftSDN, expectedGeneveUDP: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			firewall := desiredFirewall("test-infra", "networks/test-infra-network", tt.networkType, "30000-31000")

			if !slices.Contains(firewall.Allowed[0].Ports, "30000-31000") {
				t.Errorf("expected tcp ports %v to contain the custom node port range", firewall.Allowed[0].Ports)
			}
			if !slices.Contains(firewall.Allowed[1].Ports, "30000-31000") {
				t.Errorf("expected udp ports %v to contain the custom node port range", firewall.Allowed[1].Ports)
			}
			if got := slices.Contains(firewall.Allowed[1].Ports, geneveOverlayPort); got != tt.expectedGeneveUDP {
				t.Errorf("expected udp ports %v to contain %q: %v, got %v", firewall.Allowed[1].Ports, geneveOverlayPort, tt.expectedGeneveUDP, got)
			}
		})
	}
}

func TestOwnershipMarker(t *testing.T) {
	marker, err := ownershipMarker("test-infra")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	var parsed map[string]string
	if err := json.Unmarshal([]byte(marker), &parsed); err != nil {
		t.Fatalf("expected valid JSON marker, got %q: %v", marker, err)
	}

	expected := map[string]string{
		"hypershift.openshift.io/managed-by": "control-plane-operator",
		"hypershift.openshift.io/infra-id":   "test-infra",
	}
	if len(parsed) != len(expected) {
		t.Errorf("expected exactly %d marker keys, got %v", len(expected), parsed)
	}
	for key, want := range expected {
		if got := parsed[key]; got != want {
			t.Errorf("expected marker %q to be %q, got %q", key, want, got)
		}
	}
}

func TestHasOwnershipMarker(t *testing.T) {
	validMarker, err := ownershipMarker("test-infra")
	if err != nil {
		t.Fatalf("failed to build marker: %v", err)
	}
	otherInfraMarker, err := ownershipMarker("other-infra")
	if err != nil {
		t.Fatalf("failed to build marker: %v", err)
	}

	tests := []struct {
		name     string
		rule     *compute.Firewall
		expected bool
	}{
		{
			name:     "When the description carries a matching marker it should be owned",
			rule:     &compute.Firewall{Description: validMarker},
			expected: true,
		},
		{
			name:     "When the description carries a marker for another infra ID it should not be owned",
			rule:     &compute.Firewall{Description: otherInfraMarker},
			expected: false,
		},
		{
			name:     "When the description is empty it should not be owned",
			rule:     &compute.Firewall{Description: ""},
			expected: false,
		},
		{
			name:     "When the description is not JSON it should not be owned",
			rule:     &compute.Firewall{Description: "Allow kubelet API access"},
			expected: false,
		},
		{
			name:     "When the marker names another manager it should not be owned",
			rule:     &compute.Firewall{Description: `{"hypershift.openshift.io/managed-by":"someone-else","hypershift.openshift.io/infra-id":"test-infra"}`},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasOwnershipMarker(tt.rule, "test-infra"); got != tt.expected {
				t.Errorf("expected hasOwnershipMarker to be %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestValidateNodePortRange(t *testing.T) {
	tests := []struct {
		name          string
		nodePortRange string
		expectedError string
	}{
		{name: "When the range is the HyperShift default it should pass", nodePortRange: "30000-32767"},
		{name: "When the range is a single port it should pass", nodePortRange: "30080-30080"},
		{name: "When the range has no dash it should fail", nodePortRange: "30000", expectedError: "min-max form"},
		{name: "When the minimum is not a number it should fail", nodePortRange: "abc-32767", expectedError: "minimum is not a number"},
		{name: "When the maximum is not a number it should fail", nodePortRange: "30000-xyz", expectedError: "maximum is not a number"},
		{name: "When the minimum is above the maximum it should fail", nodePortRange: "32767-30000", expectedError: "1 <= min <= max <= 65535"},
		{name: "When a port is out of range it should fail", nodePortRange: "30000-70000", expectedError: "1 <= min <= max <= 65535"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNodePortRange(tt.nodePortRange)

			if tt.expectedError == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.expectedError)
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
			}
		})
	}
}

func TestValidateNetworkType(t *testing.T) {
	tests := []struct {
		name        string
		networkType string
		wantErr     bool
	}{
		{name: "When the network type is OVNKubernetes it should pass", networkType: NetworkTypeOVNKubernetes},
		{name: "When the network type is OpenShiftSDN it should pass", networkType: NetworkTypeOpenShiftSDN},
		{name: "When the network type is unknown it should fail", networkType: "Calico", wantErr: true},
		{name: "When the network type is empty it should fail", networkType: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNetworkType(tt.networkType)
			if tt.wantErr && err == nil {
				t.Errorf("expected an error for network type %q, got nil", tt.networkType)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error for network type %q, got %v", tt.networkType, err)
			}
		})
	}
}

func TestCreateOptionsApplyDefaults(t *testing.T) {
	opts := &CreateOptions{}
	opts.applyDefaults()

	if opts.VPCCidr != DefaultSubnetCIDR {
		t.Errorf("expected VPC CIDR %q, got %q", DefaultSubnetCIDR, opts.VPCCidr)
	}
	if opts.NetworkType != DefaultNetworkType {
		t.Errorf("expected network type %q, got %q", DefaultNetworkType, opts.NetworkType)
	}
	if opts.NodePortRange != DefaultNodePortRange {
		t.Errorf("expected node port range %q, got %q", DefaultNodePortRange, opts.NodePortRange)
	}
}
