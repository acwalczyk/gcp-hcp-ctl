package network

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"google.golang.org/api/compute/v1"
	"google.golang.org/api/googleapi"

	gcpnetworking "github.com/openshift-online/gcp-hcp-ctl/pkg/gcp/networking"
)

// Manager orchestrates network infrastructure lifecycle using the GCP Compute client.
type Manager struct {
	projectID string
	infraID   string
	region    string

	client *gcpnetworking.Client
	logger logr.Logger
}

func NewManager(ctx context.Context, projectID, infraID, region string, logger logr.Logger) (*Manager, error) {
	if projectID == "" {
		return nil, fmt.Errorf("projectID is required")
	}
	if infraID == "" {
		return nil, fmt.Errorf("infraID is required")
	}
	if region == "" {
		return nil, fmt.Errorf("region is required")
	}

	client, err := gcpnetworking.NewClient(ctx, projectID, region)
	if err != nil {
		return nil, fmt.Errorf("failed to create networking client: %w", err)
	}

	return &Manager{
		projectID: projectID,
		infraID:   infraID,
		region:    region,
		client:    client,
		logger:    logger,
	}, nil
}

// ============================================================================
// Create Methods
// ============================================================================

func (m *Manager) CreateNetwork(ctx context.Context) (*compute.Network, error) {
	networkName := m.formatNetworkName()
	m.logger.Info("Creating VPC network", "name", networkName)

	network := &compute.Network{
		Name:                  networkName,
		AutoCreateSubnetworks: false,
		Description:           fmt.Sprintf("HyperShift VPC for cluster %s", m.infraID),
		ForceSendFields:       []string{"AutoCreateSubnetworks"},
	}

	if err := m.client.InsertNetwork(ctx, network); err != nil {
		if isAlreadyExistsError(err) {
			m.logger.Info("VPC network already exists", "name", networkName)
			return m.client.GetNetwork(ctx, networkName)
		}
		return nil, fmt.Errorf("failed to create VPC network: %w", err)
	}

	m.logger.Info("Created VPC network", "name", networkName)
	return m.client.GetNetwork(ctx, networkName)
}

func (m *Manager) CreateFirewallRule(ctx context.Context, networkSelfLink string) (*compute.Firewall, error) {
	firewallName := m.formatLegacyFirewallName()
	m.logger.Info("Creating firewall rule", "name", firewallName)

	firewall := &compute.Firewall{
		Name:        firewallName,
		Network:     networkSelfLink,
		Description: fmt.Sprintf("Allow kubelet API access for HyperShift cluster %s", m.infraID),
		Allowed: []*compute.FirewallAllowed{
			{
				IPProtocol: "tcp",
				Ports:      []string{"10250"},
			},
		},
		Direction: "INGRESS",
		// Allow traffic from the 10.0.0.0/8 range which covers:
		// - Worker node subnet (e.g., 10.0.0.0/24)
		// - Pod CIDR (e.g., 10.132.0.0/14)
		// This enables kube-apiserver to reach kubelets via konnectivity and
		// allows pods to scrape kubelet metrics.
		SourceRanges: []string{"10.0.0.0/8"},
	}

	if err := m.client.InsertFirewall(ctx, firewall); err != nil {
		if isAlreadyExistsError(err) {
			m.logger.Info("Firewall rule already exists", "name", firewallName)
			return m.client.GetFirewall(ctx, firewallName)
		}
		return nil, fmt.Errorf("failed to create firewall rule: %w", err)
	}

	m.logger.Info("Created firewall rule", "name", firewallName)
	return m.client.GetFirewall(ctx, firewallName)
}

func (m *Manager) CreateSubnet(ctx context.Context, networkSelfLink, cidr string) (*compute.Subnetwork, error) {
	subnetName := m.formatSubnetName()
	m.logger.Info("Creating subnet", "name", subnetName, "cidr", cidr)

	subnet := &compute.Subnetwork{
		Name:                  subnetName,
		IpCidrRange:           cidr,
		Network:               networkSelfLink,
		Region:                m.region,
		PrivateIpGoogleAccess: true,
		Description:           fmt.Sprintf("HyperShift subnet for cluster %s", m.infraID),
	}

	if err := m.client.InsertSubnet(ctx, subnet); err != nil {
		if isAlreadyExistsError(err) {
			m.logger.Info("Subnet already exists", "name", subnetName)
			return m.client.GetSubnet(ctx, subnetName)
		}
		return nil, fmt.Errorf("failed to create subnet: %w", err)
	}

	m.logger.Info("Created subnet", "name", subnetName, "cidr", cidr)
	return m.client.GetSubnet(ctx, subnetName)
}

func (m *Manager) CreateRouter(ctx context.Context, networkSelfLink string) (*compute.Router, error) {
	routerName := m.formatRouterName()
	m.logger.Info("Creating Cloud Router", "name", routerName)

	router := &compute.Router{
		Name:        routerName,
		Network:     networkSelfLink,
		Description: fmt.Sprintf("HyperShift Cloud Router for cluster %s", m.infraID),
	}

	if err := m.client.InsertRouter(ctx, router); err != nil {
		if isAlreadyExistsError(err) {
			m.logger.Info("Cloud Router already exists", "name", routerName)
			return m.client.GetRouter(ctx, routerName)
		}
		return nil, fmt.Errorf("failed to create Cloud Router: %w", err)
	}

	m.logger.Info("Created Cloud Router", "name", routerName)
	return m.client.GetRouter(ctx, routerName)
}

func (m *Manager) CreateNAT(ctx context.Context, routerName, subnetSelfLink string) (string, error) {
	natName := m.formatNATName()
	m.logger.Info("Creating Cloud NAT", "name", natName, "router", routerName)

	router, err := m.client.GetRouter(ctx, routerName)
	if err != nil {
		return "", fmt.Errorf("failed to get router for NAT configuration: %w", err)
	}

	for _, nat := range router.Nats {
		if nat.Name == natName {
			m.logger.Info("Cloud NAT already exists", "name", natName)
			return natName, nil
		}
	}

	nat := &compute.RouterNat{
		Name:                          natName,
		NatIpAllocateOption:           "AUTO_ONLY",
		SourceSubnetworkIpRangesToNat: "LIST_OF_SUBNETWORKS",
		Subnetworks: []*compute.RouterNatSubnetworkToNat{
			{
				Name:                subnetSelfLink,
				SourceIpRangesToNat: []string{"ALL_IP_RANGES"},
			},
		},
	}
	router.Nats = append(router.Nats, nat)

	if err := m.client.PatchRouter(ctx, routerName, router); err != nil {
		return "", fmt.Errorf("failed to create Cloud NAT: %w", err)
	}

	m.logger.Info("Created Cloud NAT", "name", natName)
	return natName, nil
}

// ============================================================================
// Destroy Methods
// ============================================================================

func (m *Manager) DeleteNAT(ctx context.Context) error {
	routerName := m.formatRouterName()
	natName := m.formatNATName()
	m.logger.Info("Deleting Cloud NAT", "name", natName, "router", routerName)

	router, err := m.client.GetRouter(ctx, routerName)
	if err != nil {
		if isNotFoundError(err) {
			m.logger.Info("Cloud Router not found, skipping NAT deletion", "router", routerName)
			return nil
		}
		return fmt.Errorf("failed to get router for NAT deletion: %w", err)
	}

	var updatedNats []*compute.RouterNat
	found := false
	for _, nat := range router.Nats {
		if nat.Name == natName {
			found = true
			continue
		}
		updatedNats = append(updatedNats, nat)
	}

	if !found {
		m.logger.Info("Cloud NAT not found, skipping", "name", natName)
		return nil
	}

	router.Nats = updatedNats
	router.ForceSendFields = []string{"Nats"}

	if err := m.client.PatchRouter(ctx, routerName, router); err != nil {
		if isNotFoundError(err) {
			m.logger.Info("Cloud Router deleted during NAT removal, skipping", "router", routerName)
			return nil
		}
		return fmt.Errorf("failed to delete Cloud NAT: %w", err)
	}

	m.logger.Info("Deleted Cloud NAT", "name", natName)
	return nil
}

func (m *Manager) DeleteRouter(ctx context.Context) error {
	routerName := m.formatRouterName()
	m.logger.Info("Deleting Cloud Router", "name", routerName)

	if err := m.client.DeleteRouter(ctx, routerName); err != nil {
		if isNotFoundError(err) {
			m.logger.Info("Cloud Router not found, skipping", "name", routerName)
			return nil
		}
		return fmt.Errorf("failed to delete Cloud Router: %w", err)
	}

	m.logger.Info("Deleted Cloud Router", "name", routerName)
	return nil
}

func (m *Manager) DeleteSubnet(ctx context.Context) error {
	subnetName := m.formatSubnetName()
	m.logger.Info("Deleting subnet", "name", subnetName)

	if err := m.client.DeleteSubnet(ctx, subnetName); err != nil {
		if isNotFoundError(err) {
			m.logger.Info("Subnet not found, skipping", "name", subnetName)
			return nil
		}
		return fmt.Errorf("failed to delete subnet: %w", err)
	}

	m.logger.Info("Deleted subnet", "name", subnetName)
	return nil
}

// DeleteFirewallRule deletes the firewall rules a cluster may own: the managed
// worker rule (<infra-id>-internal-cluster) and the legacy kubelet rule
// (<infra-id>-allow-kubelet) created by older CLI versions. Both must be gone
// before the VPC can be deleted. Not-found is treated as success so repeated
// runs are safe.
func (m *Manager) DeleteFirewallRule(ctx context.Context) error {
	for _, name := range m.firewallRuleNames() {
		if err := m.deleteFirewallRule(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) deleteFirewallRule(ctx context.Context, firewallName string) error {
	return deleteFirewallRuleTolerant(ctx, firewallName, m.client.DeleteFirewall, m.logger)
}

// deleteFirewallRuleTolerant deletes one firewall rule, treating NotFound as
// success so cleanup is idempotent.
func deleteFirewallRuleTolerant(ctx context.Context, firewallName string, deleteFn func(context.Context, string) error, logger logr.Logger) error {
	logger.Info("Deleting firewall rule", "name", firewallName)

	if err := deleteFn(ctx, firewallName); err != nil {
		if isNotFoundError(err) {
			logger.Info("Firewall rule not found, skipping", "name", firewallName)
			return nil
		}
		return fmt.Errorf("failed to delete firewall rule %s: %w", firewallName, err)
	}

	logger.Info("Deleted firewall rule", "name", firewallName)
	return nil
}

func (m *Manager) DeleteNetwork(ctx context.Context) error {
	networkName := m.formatNetworkName()
	m.logger.Info("Deleting VPC network", "name", networkName)

	if err := m.client.DeleteNetwork(ctx, networkName); err != nil {
		if isNotFoundError(err) {
			m.logger.Info("VPC network not found, skipping", "name", networkName)
			return nil
		}
		return fmt.Errorf("failed to delete VPC network: %w", err)
	}

	m.logger.Info("Deleted VPC network", "name", networkName)
	return nil
}

// ============================================================================
// Formatting Helpers
// ============================================================================

func (m *Manager) formatNetworkName() string {
	return fmt.Sprintf("%s-network", m.infraID)
}

func (m *Manager) formatSubnetName() string {
	return fmt.Sprintf("%s-subnet", m.infraID)
}

func (m *Manager) formatRouterName() string {
	return fmt.Sprintf("%s-router", m.infraID)
}

func (m *Manager) formatNATName() string {
	return fmt.Sprintf("%s-nat", m.infraID)
}

// formatFirewallName returns the name of the managed worker firewall rule that
// the control-plane-operator reconciles (GCP-1221).
func (m *Manager) formatFirewallName() string {
	return fmt.Sprintf("%s-internal-cluster", m.infraID)
}

// formatLegacyFirewallName returns the name of the kubelet firewall rule older
// CLI versions created at setup time. It is only ever deleted now.
func (m *Manager) formatLegacyFirewallName() string {
	return fmt.Sprintf("%s-allow-kubelet", m.infraID)
}

// firewallRuleNames returns every firewall rule a cluster may own, in deletion
// order. Both are removed before the VPC network, which GCP refuses to delete
// while a rule still references it.
func (m *Manager) firewallRuleNames() []string {
	return []string{m.formatFirewallName(), m.formatLegacyFirewallName()}
}

// ============================================================================
// Error Helpers
// ============================================================================

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 404
	}
	return false
}

func isAlreadyExistsError(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		return apiErr.Code == 409
	}
	return false
}
