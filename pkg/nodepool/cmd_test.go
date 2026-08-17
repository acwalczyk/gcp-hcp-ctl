package nodepool

import (
	"strings"
	"testing"
	"time"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodePoolStatus(t *testing.T) {
	t.Run("When there are no conditions it should return Pending", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{},
		}
		if got := nodePoolStatus(np); got != "Pending" {
			t.Errorf("expected 'Pending', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable and NodePoolHealthy are True it should return Ready", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionTrue},
					{Type: "NodePoolHealthy", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := nodePoolStatus(np); got != "Ready" {
			t.Errorf("expected 'Ready', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is False it should return Progressing", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionFalse, Reason: "NodePoolNotAvailable"},
				},
			},
		}
		if got := nodePoolStatus(np); got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is True and NodePoolHealthy is False it should return Degraded", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionTrue},
					{Type: "NodePoolHealthy", Status: metav1.ConditionFalse, Reason: "NodePoolNotHealthy"},
				},
			},
		}
		if got := nodePoolStatus(np); got != "Degraded" {
			t.Errorf("expected 'Degraded', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is True and NodePoolHealthy is Unknown it should return Progressing", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionTrue},
					{Type: "NodePoolHealthy", Status: metav1.ConditionUnknown, Reason: "NodePoolHealthCheckPending"},
				},
			},
		}
		if got := nodePoolStatus(np); got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})

	t.Run("When deleted_time is set it should return Deleting", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &now,
			},
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionTrue},
					{Type: "NodePoolHealthy", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := nodePoolStatus(np); got != "Deleting" {
			t.Errorf("expected 'Deleting', got %q", got)
		}
	})

	t.Run("When conditions exist but no NodePoolAvailable it should return Progressing", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "SomeOtherCondition", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := nodePoolStatus(np); got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})
}

func TestNodePoolStatusDetail(t *testing.T) {
	t.Run("When Ready it should return just Ready without parenthetical", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionTrue},
					{Type: "NodePoolHealthy", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := nodePoolStatusDetail(np); got != "Ready" {
			t.Errorf("expected 'Ready', got %q", got)
		}
	})

	t.Run("When Pending it should return just Pending without parenthetical", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Status: gcpv1.NodePoolStatus{},
		}
		if got := nodePoolStatusDetail(np); got != "Pending" {
			t.Errorf("expected 'Pending', got %q", got)
		}
	})

	t.Run("When Deleting it should return just Deleting without parenthetical", func(t *testing.T) {
		now := metav1.NewTime(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &now,
			},
			Status: gcpv1.NodePoolStatus{},
		}
		if got := nodePoolStatusDetail(np); got != "Deleting" {
			t.Errorf("expected 'Deleting', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is False it should return Progressing with the available condition message", func(t *testing.T) {
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionFalse, Reason: "NodePoolNotAvailable", Message: "Some nodes not available", ObservedGeneration: 1},
				},
			},
		}
		got := nodePoolStatusDetail(np)
		if got != "Progressing (Some nodes not available)" {
			t.Errorf("expected 'Progressing (Some nodes not available)', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is False with generation mismatch it should show generation detail", func(t *testing.T) {
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Generation: 3},
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionFalse, Reason: "NodePoolNotAvailable", ObservedGeneration: 2},
				},
			},
		}
		got := nodePoolStatusDetail(np)
		if got != "Progressing (controller reconciling generation 3)" {
			t.Errorf("expected 'Progressing (controller reconciling generation 3)', got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is False with long message it should truncate", func(t *testing.T) {
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionFalse, Reason: "NodePoolNotAvailable", Message: strings.Repeat("a", 80), ObservedGeneration: 1},
				},
			},
		}
		got := nodePoolStatusDetail(np)
		if !strings.Contains(got, "...") {
			t.Errorf("expected truncation ellipsis, got %q", got)
		}
		if !strings.HasPrefix(got, "Progressing (") {
			t.Errorf("expected 'Progressing (' prefix, got %q", got)
		}
	})

	t.Run("When NodePoolAvailable is False with no message it should fall back to reason", func(t *testing.T) {
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionFalse, Reason: "NodePoolNotAvailable", ObservedGeneration: 1},
				},
			},
		}
		got := nodePoolStatusDetail(np)
		if got != "Progressing (NodePoolNotAvailable)" {
			t.Errorf("expected 'Progressing (NodePoolNotAvailable)', got %q", got)
		}
	})

	t.Run("When Available is True but Healthy is False it should return Degraded with the health condition message", func(t *testing.T) {
		np := &gcpv1.NodePool{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.NodePoolStatus{
				Conditions: []metav1.Condition{
					{Type: "NodePoolAvailable", Status: metav1.ConditionTrue, Reason: "NodePoolAvailable"},
					{Type: "NodePoolHealthy", Status: metav1.ConditionFalse, Reason: "NodePoolNotHealthy", Message: "2 of 3 nodes not ready", ObservedGeneration: 1},
				},
			},
		}
		got := nodePoolStatusDetail(np)
		if got != "Degraded (2 of 3 nodes not ready)" {
			t.Errorf("expected 'Degraded (2 of 3 nodes not ready)', got %q", got)
		}
	})
}

func TestFindCondition(t *testing.T) {
	t.Run("When condition exists it should return it", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "Available", Status: metav1.ConditionTrue},
			{Type: "Reconciled", Status: metav1.ConditionFalse},
		}
		got := meta.FindStatusCondition(conditions, "Reconciled")
		if got == nil {
			t.Fatal("expected non-nil condition")
		}
		if got.Status != metav1.ConditionFalse {
			t.Errorf("expected status False, got %q", got.Status)
		}
	})

	t.Run("When condition does not exist it should return nil", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "Available", Status: metav1.ConditionTrue},
		}
		if got := meta.FindStatusCondition(conditions, "Reconciled"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("When conditions slice is empty it should return nil", func(t *testing.T) {
		if got := meta.FindStatusCondition(nil, "Reconciled"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}

func TestReleaseVersion(t *testing.T) {
	t.Run("When release version is set it should return it", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{
				Release: gcpv1.ReleaseSpec{Version: "4.22.0"},
			},
		}
		if got := releaseVersion(np); got != "4.22.0" {
			t.Errorf("expected '4.22.0', got %q", got)
		}
	})

	t.Run("When version is empty it should return <none>", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{},
		}
		if got := releaseVersion(np); got != "<none>" {
			t.Errorf("expected '<none>', got %q", got)
		}
	})
}

func TestNodeCount(t *testing.T) {
	t.Run("When nodeCount is set it should return the count", func(t *testing.T) {
		r := int32(3)
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{NodeCount: &r},
		}
		if got := nodeCount(np); got != "3" {
			t.Errorf("expected '3', got %q", got)
		}
	})

	t.Run("When nodeCount is zero it should return 0", func(t *testing.T) {
		r := int32(0)
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{NodeCount: &r},
		}
		if got := nodeCount(np); got != "0" {
			t.Errorf("expected '0', got %q", got)
		}
	})

	t.Run("When nodeCount is nil it should return dash", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{},
		}
		if got := nodeCount(np); got != "-" {
			t.Errorf("expected '-', got %q", got)
		}
	})
}

func TestNpConditionSummary(t *testing.T) {
	t.Run("When observed generation is behind it should show generation detail", func(t *testing.T) {
		cond := &metav1.Condition{
			Status:             metav1.ConditionFalse,
			Reason:             "NotReconciled",
			ObservedGeneration: 2,
		}
		got := conditionSummary(cond, 3)
		if got != "controller reconciling generation 3" {
			t.Errorf("expected 'controller reconciling generation 3', got %q", got)
		}
	})

	t.Run("When observed generation is zero it should not show generation detail", func(t *testing.T) {
		cond := &metav1.Condition{
			Status:             metav1.ConditionFalse,
			Reason:             "NotReconciled",
			Message:            "waiting for adapters",
			ObservedGeneration: 0,
		}
		got := conditionSummary(cond, 3)
		if got != "waiting for adapters" {
			t.Errorf("expected 'waiting for adapters', got %q", got)
		}
	})

	t.Run("When message is present it should return the message", func(t *testing.T) {
		cond := &metav1.Condition{
			Status:             metav1.ConditionFalse,
			Reason:             "SomeReason",
			Message:            "detailed message",
			ObservedGeneration: 1,
		}
		got := conditionSummary(cond, 1)
		if got != "detailed message" {
			t.Errorf("expected 'detailed message', got %q", got)
		}
	})

	t.Run("When message exceeds 60 chars it should truncate", func(t *testing.T) {
		cond := &metav1.Condition{
			Status:             metav1.ConditionFalse,
			Reason:             "SomeReason",
			Message:            strings.Repeat("a", 80),
			ObservedGeneration: 1,
		}
		got := conditionSummary(cond, 1)
		if len(got) != 63 {
			t.Errorf("expected length 63 (60 + '...'), got %d", len(got))
		}
		if !strings.HasSuffix(got, "...") {
			t.Error("expected truncation suffix '...'")
		}
	})

	t.Run("When no message it should fall back to reason", func(t *testing.T) {
		cond := &metav1.Condition{
			Status:             metav1.ConditionFalse,
			Reason:             "AdaptersNotReady",
			ObservedGeneration: 1,
		}
		got := conditionSummary(cond, 1)
		if got != "AdaptersNotReady" {
			t.Errorf("expected 'AdaptersNotReady', got %q", got)
		}
	})

	t.Run("When both reason and message are empty it should return empty string", func(t *testing.T) {
		cond := &metav1.Condition{
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 1,
		}
		got := conditionSummary(cond, 1)
		if got != "" {
			t.Errorf("expected empty string, got %q", got)
		}
	})
}

func TestMachineType(t *testing.T) {
	t.Run("When GCP platform is set it should return machine type", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{
				Platform: gcpv1.NodePoolPlatformSpec{
					GCP: &gcpv1.GCPNodePoolPlatform{MachineType: "n2-standard-4"},
				},
			},
		}
		if got := machineType(np); got != "n2-standard-4" {
			t.Errorf("expected 'n2-standard-4', got %q", got)
		}
	})

	t.Run("When GCP platform is nil it should return dash", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{
				Platform: gcpv1.NodePoolPlatformSpec{},
			},
		}
		if got := machineType(np); got != "-" {
			t.Errorf("expected '-', got %q", got)
		}
	})

	t.Run("When GCP platform is set but MachineType is empty it should return dash", func(t *testing.T) {
		np := &gcpv1.NodePool{
			Spec: gcpv1.NodePoolSpec{
				Platform: gcpv1.NodePoolPlatformSpec{
					GCP: &gcpv1.GCPNodePoolPlatform{MachineType: ""},
				},
			},
		}
		if got := machineType(np); got != "-" {
			t.Errorf("expected '-', got %q", got)
		}
	})
}
