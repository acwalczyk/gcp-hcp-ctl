package network

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr"
	"google.golang.org/api/googleapi"
)

func TestManagerFormatMethods(t *testing.T) {
	m := &Manager{
		projectID: "test-project",
		infraID:   "test-infra",
		region:    "us-central1",
		logger:    logr.Discard(),
	}

	tests := []struct {
		name     string
		method   func() string
		expected string
	}{
		{
			name:     "When formatNetworkName is called it should return infraID-network",
			method:   m.formatNetworkName,
			expected: "test-infra-network",
		},
		{
			name:     "When formatSubnetName is called it should return infraID-subnet",
			method:   m.formatSubnetName,
			expected: "test-infra-subnet",
		},
		{
			name:     "When formatRouterName is called it should return infraID-router",
			method:   m.formatRouterName,
			expected: "test-infra-router",
		},
		{
			name:     "When formatNATName is called it should return infraID-nat",
			method:   m.formatNATName,
			expected: "test-infra-nat",
		},
		{
			name:     "When formatFirewallName is called it should return infraID-internal-cluster",
			method:   m.formatFirewallName,
			expected: "test-infra-internal-cluster",
		},
		{
			name:     "When formatLegacyFirewallName is called it should return infraID-allow-kubelet",
			method:   m.formatLegacyFirewallName,
			expected: "test-infra-allow-kubelet",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.method()
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestFirewallRuleNames(t *testing.T) {
	m := &Manager{infraID: "test-infra", logger: logr.Discard()}

	expected := []string{"test-infra-internal-cluster", "test-infra-allow-kubelet"}
	got := m.firewallRuleNames()

	if len(got) != len(expected) {
		t.Fatalf("expected %d firewall rule names, got %d: %v", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("expected firewall rule name %d to be %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestDeleteFirewallRuleTolerant(t *testing.T) {
	tests := []struct {
		name          string
		deleteErr     error
		expectedError string
	}{
		{
			name:      "When the rule exists it should delete it without error",
			deleteErr: nil,
		},
		{
			name:      "When the rule is already gone it should treat not-found as success",
			deleteErr: &googleapi.Error{Code: 404, Message: "Not found"},
		},
		{
			name:          "When deletion fails it should return an error",
			deleteErr:     &googleapi.Error{Code: 500, Message: "Internal server error"},
			expectedError: "failed to delete firewall rule test-infra-internal-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleteFn := func(context.Context, string) error { return tt.deleteErr }

			err := deleteFirewallRuleTolerant(context.Background(), "test-infra-internal-cluster", deleteFn, logr.Discard())

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectedError)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
				return
			}
			if err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}

// TestDestroyFirewallRulesIdempotent verifies that a destroy run deletes both
// rule names and succeeds when neither exists (already-clean cluster).
func TestDestroyFirewallRulesIdempotent(t *testing.T) {
	m := &Manager{infraID: "test-infra", logger: logr.Discard()}

	var deleted []string
	deleteFn := func(_ context.Context, name string) error {
		deleted = append(deleted, name)
		return &googleapi.Error{Code: 404, Message: "Not found"}
	}

	for _, name := range m.firewallRuleNames() {
		if err := deleteFirewallRuleTolerant(context.Background(), name, deleteFn, logr.Discard()); err != nil {
			t.Fatalf("expected repeated destroy to succeed, got %v", err)
		}
	}

	expected := []string{"test-infra-internal-cluster", "test-infra-allow-kubelet"}
	if len(deleted) != len(expected) {
		t.Fatalf("expected %d delete attempts, got %v", len(expected), deleted)
	}
	for i := range expected {
		if deleted[i] != expected[i] {
			t.Errorf("expected delete attempt %d to be %q, got %q", i, expected[i], deleted[i])
		}
	}
}

func TestIsNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "When error is nil it should return false",
			err:      nil,
			expected: false,
		},
		{
			name:     "When error is a 404 not found it should return true",
			err:      &googleapi.Error{Code: 404, Message: "Not found"},
			expected: true,
		},
		{
			name:     "When error is a 409 conflict it should return false",
			err:      &googleapi.Error{Code: 409, Message: "Already exists"},
			expected: false,
		},
		{
			name:     "When error is a 500 server error it should return false",
			err:      &googleapi.Error{Code: 500, Message: "Internal server error"},
			expected: false,
		},
		{
			name:     "When error is a non-googleapi error it should return false",
			err:      fmt.Errorf("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNotFoundError(tt.err)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestIsAlreadyExistsError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "When error is nil it should return false",
			err:      nil,
			expected: false,
		},
		{
			name:     "When error is a 409 conflict it should return true",
			err:      &googleapi.Error{Code: 409, Message: "Already exists"},
			expected: true,
		},
		{
			name:     "When error is a 404 not found it should return false",
			err:      &googleapi.Error{Code: 404, Message: "Not found"},
			expected: false,
		},
		{
			name:     "When error is a 403 forbidden it should return false",
			err:      &googleapi.Error{Code: 403, Message: "Forbidden"},
			expected: false,
		},
		{
			name:     "When error is a non-googleapi error it should return false",
			err:      fmt.Errorf("some other error"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAlreadyExistsError(tt.err)
			if got != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}
