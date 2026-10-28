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

	// cpoFirewallMinimumVersion is the first OCP GA release whose
	// control-plane-operator owns the worker firewall rule. GCP-1221 shipped in
	// openshift/hypershift#9640 and was backported to release-5.0 in
	// openshift/hypershift#10096, so every 5.0.0 GA payload and later carries it.
	//
	// Pre-GA candidates cannot be ordered against that backport by version
	// number: for example 5.0.0-ec.6 was cut before it. shouldCreateFirewallRule
	// therefore keeps creating the rule for every prerelease.
	cpoFirewallMinimumVersion = "v5.0.0"
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
// CPO owns the rule from cpoFirewallMinimumVersion (5.0.0 GA) onwards. Earlier
// GA releases (4.x) and every prerelease fall back to CLI creation: a
// prerelease may have been cut before the CPO firewall code landed, and its
// version number cannot distinguish the two cases. Creating a rule CPO also
// wants is harmless, because CPO adopts a marked rule it finds, whereas not
// creating it where CPO has no firewall code leaves the cluster without worker
// traffic rules. An unknown version is treated the same way.
func shouldCreateFirewallRule(version string, noFirewallRule bool) bool {
	if noFirewallRule {
		return false
	}
	v := normalizeVersion(version)
	if !semver.IsValid(v) || semver.Prerelease(v) != "" {
		return true
	}
	return semver.Compare(v, cpoFirewallMinimumVersion) < 0
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
