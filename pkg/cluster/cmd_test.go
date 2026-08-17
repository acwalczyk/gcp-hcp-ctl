package cluster

import (
	"testing"
	"time"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestClusterStatus(t *testing.T) {
	t.Run("When there are no conditions it should return Pending", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{},
		}
		if got := clusterStatus(c); got != "Pending" {
			t.Errorf("expected 'Pending', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is True it should return Ready", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := clusterStatus(c); got != "Ready" {
			t.Errorf("expected 'Ready', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is absent it should return Progressing", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "SomeOtherCondition", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := clusterStatus(c); got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is False it should return Progressing", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionFalse, Reason: "NotAvailable"},
				},
			},
		}
		if got := clusterStatus(c); got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})

	t.Run("When DeletionTimestamp is set it should return Deleting", func(t *testing.T) {
		now := metav1.NewTime(time.Now())
		c := &gcpv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := clusterStatus(c); got != "Deleting" {
			t.Errorf("expected 'Deleting', got %q", got)
		}
	})

	t.Run("When conditions exist but no HostedClusterAvailable it should return Progressing", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "SomeOtherCondition", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := clusterStatus(c); got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})
}

func TestClusterStatusDetail(t *testing.T) {
	t.Run("When Ready it should return just Ready without parenthetical", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionTrue},
				},
			},
		}
		if got := clusterStatusDetail(c); got != "Ready" {
			t.Errorf("expected 'Ready', got %q", got)
		}
	})

	t.Run("When Pending it should return just Pending without parenthetical", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Status: gcpv1.ClusterStatus{},
		}
		if got := clusterStatusDetail(c); got != "Pending" {
			t.Errorf("expected 'Pending', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is False with message it should return Progressing with detail", func(t *testing.T) {
		c := &gcpv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "Waiting for controllers", ObservedGeneration: 1},
				},
			},
		}
		got := clusterStatusDetail(c)
		if got != "Progressing (Waiting for controllers)" {
			t.Errorf("expected 'Progressing (Waiting for controllers)', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is False with reason but no message it should show reason", func(t *testing.T) {
		c := &gcpv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionFalse, Reason: "NotAvailable", ObservedGeneration: 1},
				},
			},
		}
		got := clusterStatusDetail(c)
		if got != "Progressing (NotAvailable)" {
			t.Errorf("expected 'Progressing (NotAvailable)', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is False with reason equal to type and no message it should omit the parenthetical", func(t *testing.T) {
		// Mirrors gecko's actual behavior: hc_controller.go hard-codes
		// Reason to the literal condition Type ("HostedClusterAvailable")
		// and leaves Message empty, which previously rendered as the
		// useless "Progressing (HostedClusterAvailable)".
		c := &gcpv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Generation: 1},
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionFalse, Reason: "HostedClusterAvailable", ObservedGeneration: 1},
				},
			},
		}
		got := clusterStatusDetail(c)
		if got != "Progressing" {
			t.Errorf("expected 'Progressing', got %q", got)
		}
	})

	t.Run("When HostedClusterAvailable is False and observed generation lags it should show controller reconciling detail", func(t *testing.T) {
		c := &gcpv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{Generation: 2},
			Status: gcpv1.ClusterStatus{
				Conditions: []metav1.Condition{
					{Type: "HostedClusterAvailable", Status: metav1.ConditionFalse, Reason: "NotAvailable", Message: "old message", ObservedGeneration: 1},
				},
			},
		}
		got := clusterStatusDetail(c)
		if got != "Progressing (controller reconciling generation 2)" {
			t.Errorf("expected 'Progressing (controller reconciling generation 2)', got %q", got)
		}
	})

	t.Run("When Deleting it should return just Deleting without parenthetical", func(t *testing.T) {
		now := metav1.NewTime(time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC))
		c := &gcpv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now},
			Status:     gcpv1.ClusterStatus{},
		}
		got := clusterStatusDetail(c)
		if got != "Deleting" {
			t.Errorf("expected 'Deleting', got %q", got)
		}
	})
}

func TestReleaseVersion(t *testing.T) {
	t.Run("When release version is set it should return it", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Spec: gcpv1.ClusterSpec{
				Release: gcpv1.ReleaseSpec{Version: "4.22.0"},
			},
		}
		if got := releaseVersion(c); got != "4.22.0" {
			t.Errorf("expected '4.22.0', got %q", got)
		}
	})

	t.Run("When version is empty it should return <none>", func(t *testing.T) {
		c := &gcpv1.Cluster{
			Spec: gcpv1.ClusterSpec{},
		}
		if got := releaseVersion(c); got != "<none>" {
			t.Errorf("expected '<none>', got %q", got)
		}
	})
}

func TestFindCondition(t *testing.T) {
	t.Run("When condition exists it should return a pointer to it", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
			{Type: "Available", Status: metav1.ConditionFalse},
		}
		got := meta.FindStatusCondition(conditions, "Available")
		if got == nil {
			t.Fatal("expected non-nil condition")
		}
		if got.Status != metav1.ConditionFalse {
			t.Errorf("expected False, got %q", got.Status)
		}
	})

	t.Run("When condition does not exist it should return nil", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		}
		if got := meta.FindStatusCondition(conditions, "Missing"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("When conditions list is empty it should return nil", func(t *testing.T) {
		if got := meta.FindStatusCondition(nil, "Ready"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
}
