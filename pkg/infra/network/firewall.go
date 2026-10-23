package network

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"
	"google.golang.org/api/compute/v1"
)

const (
	// managedByMarkerKey and infraIDMarkerKey are the JSON keys written into the
	// firewall description to establish control-plane-operator ownership. CPO
	// only adopts or deletes a rule whose description carries this marker.
	managedByMarkerKey = "hypershift.openshift.io/managed-by"
	infraIDMarkerKey   = "hypershift.openshift.io/infra-id"

	// managedByValue is the value written under managedByMarkerKey.
	managedByValue = "control-plane-operator"

	// firewallPriority is the priority of the managed worker firewall rule.
	firewallPriority = 1000

	// geneveOverlayPort is the UDP port used by the OVN-Kubernetes Geneve overlay.
	geneveOverlayPort = "6081"

	// DefaultNodePortRange is HyperShift's default serviceNodePortRange.
	DefaultNodePortRange = "30000-32767"

	// DefaultNetworkType is the network type assumed when none is supplied. It
	// matches the HyperShift default and adds the Geneve overlay UDP port.
	DefaultNetworkType = "OVNKubernetes"

	// Supported network types.
	NetworkTypeOVNKubernetes = "OVNKubernetes"
	NetworkTypeOpenShiftSDN  = "OpenShiftSDN"

	// cpoFirewallMinimumVersion is the first OCP release whose
	// control-plane-operator owns the worker firewall rule (GCP-1221,
	// openshift/hypershift#9640). Payloads before it have no CPO firewall code,
	// so this CLI must create the rule for them.
	cpoFirewallMinimumVersion = "v5.1.0"
)

// normalizeVersion prefixes a bare version with "v" so golang.org/x/mod/semver,
// which requires the prefix, can parse it.
func normalizeVersion(version string) string {
	if version == "" || strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}

// isValidVersion reports whether version is a valid semantic version. An empty
// version is valid: it means "unknown", which is handled by the gate.
func isValidVersion(version string) bool {
	return version == "" || semver.IsValid(normalizeVersion(version))
}

// shouldCreateFirewallRule decides whether this CLI creates the worker firewall
// rule for the requested cluster version.
//
// CPO owns the rule from cpoFirewallMinimumVersion onwards. Below it the CLI
// must create the rule, because that payload has no CPO firewall code. When the
// version is unknown the CLI creates the rule: CPO adopts a marked rule it
// finds, so creating it is safe in every case, whereas not creating it would
// leave the cluster without worker traffic rules.
//
// Prereleases compare below their release, so 5.0.0-ec.6 and 5.1.0-ec.1 both
// fall below 5.1.0. That is intended: those payloads predate GCP-1221.
func shouldCreateFirewallRule(version string, noFirewallRule bool) bool {
	if noFirewallRule {
		return false
	}
	if version == "" {
		return true
	}
	return semver.Compare(normalizeVersion(version), cpoFirewallMinimumVersion) < 0
}

// validateNodePortRange checks a "min-max" service node port range.
func validateNodePortRange(nodePortRange string) error {
	minStr, maxStr, ok := strings.Cut(nodePortRange, "-")
	if !ok {
		return fmt.Errorf("node-port-range must be in min-max form (e.g. %s), got %q", DefaultNodePortRange, nodePortRange)
	}
	minPort, err := strconv.Atoi(minStr)
	if err != nil {
		return fmt.Errorf("node-port-range minimum is not a number: %w", err)
	}
	maxPort, err := strconv.Atoi(maxStr)
	if err != nil {
		return fmt.Errorf("node-port-range maximum is not a number: %w", err)
	}
	if minPort < 1 || maxPort > 65535 || minPort > maxPort {
		return fmt.Errorf("node-port-range must satisfy 1 <= min <= max <= 65535, got %q", nodePortRange)
	}
	return nil
}

// validateNetworkType checks that the network type is one this package knows how
// to translate into firewall ports.
func validateNetworkType(networkType string) error {
	switch networkType {
	case NetworkTypeOVNKubernetes, NetworkTypeOpenShiftSDN:
		return nil
	default:
		return fmt.Errorf("network-type must be one of: %s, %s", NetworkTypeOVNKubernetes, NetworkTypeOpenShiftSDN)
	}
}

// ownershipMarker builds the JSON ownership marker CPO requires in the firewall
// description.
func ownershipMarker(infraID string) (string, error) {
	marker := map[string]string{
		managedByMarkerKey: managedByValue,
		infraIDMarkerKey:   infraID,
	}
	b, err := json.Marshal(marker)
	if err != nil {
		return "", fmt.Errorf("failed to marshal ownership marker: %w", err)
	}
	return string(b), nil
}

// hasOwnershipMarker reports whether the rule description carries a well-formed
// marker naming the control-plane-operator and the given infra ID.
func hasOwnershipMarker(firewall *compute.Firewall, infraID string) bool {
	if firewall.Description == "" {
		return false
	}
	var marker map[string]string
	if err := json.Unmarshal([]byte(firewall.Description), &marker); err != nil {
		return false
	}
	return marker[managedByMarkerKey] == managedByValue && marker[infraIDMarkerKey] == infraID
}

// desiredAllowed returns the worker traffic rules CPO expects. The node port
// range comes from the cluster's serviceNodePortRange so custom ranges are not
// blocked; UDP 6081 (Geneve) only applies to OVNKubernetes.
func desiredAllowed(networkType, nodePortRange string) []*compute.FirewallAllowed {
	udpPorts := []string{"9000-9999", nodePortRange}
	if networkType == NetworkTypeOVNKubernetes {
		udpPorts = append(udpPorts, geneveOverlayPort)
	}
	return []*compute.FirewallAllowed{
		{
			IPProtocol: "tcp",
			Ports:      []string{"10250", "9000-9999", nodePortRange},
		},
		{
			IPProtocol: "udp",
			Ports:      udpPorts,
		},
	}
}

// desiredFirewall builds the worker firewall rule CPO reconciles. It must match
// CPO's desired state exactly so CPO adopts it instead of reporting an
// ownership conflict. The description (ownership marker) is set by the caller
// on create only.
func desiredFirewall(infraID, networkSelfLink, networkType, nodePortRange string) *compute.Firewall {
	tag := fmt.Sprintf("%s-worker", infraID)
	return &compute.Firewall{
		Name:       fmt.Sprintf("%s-internal-cluster", infraID),
		Network:    networkSelfLink,
		Direction:  "INGRESS",
		Priority:   firewallPriority,
		Allowed:    desiredAllowed(networkType, nodePortRange),
		SourceTags: []string{tag},
		TargetTags: []string{tag},
		// Disabled is a meaningful false: send it explicitly so a previously
		// disabled rule is re-enabled.
		Disabled: false,
		// Every other (mutually exclusive) source/target selector must be sent
		// even when empty, so CPO's repairing patch of a hand-edited rule clears
		// CIDRs and service accounts instead of combining them with tags, which
		// GCP rejects. These fields are omitempty, so ForceSendFields is required.
		ForceSendFields: []string{
			"Disabled",
			"SourceRanges",
			"DestinationRanges",
			"SourceServiceAccounts",
			"TargetServiceAccounts",
		},
	}
}
