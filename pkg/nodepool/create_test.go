package nodepool

import (
	"testing"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
)

func TestCreateOptionsValidate(t *testing.T) {
	valid := func() *createOptions {
		return &createOptions{
			clusterRef:   "my-cluster",
			replicas:     2,
			instanceType: "n2-standard-4",
			diskSize:     100,
			diskType:     "pd-balanced",
			version:      "4.22.0",
			channelGroup: "stable",
		}
	}

	t.Run("When all options are valid it should not error", func(t *testing.T) {
		if err := valid().validate(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("When version is empty it should error", func(t *testing.T) {
		o := valid()
		o.version = ""
		if err := o.validate(); err == nil {
			t.Error("expected error for empty version")
		}
	})

	t.Run("When channel group is invalid it should error", func(t *testing.T) {
		o := valid()
		o.channelGroup = "nightly"
		if err := o.validate(); err == nil {
			t.Error("expected error for invalid channel group")
		}
	})

	t.Run("When channel group is empty it should error", func(t *testing.T) {
		o := valid()
		o.channelGroup = ""
		if err := o.validate(); err == nil {
			t.Error("expected error for empty channel group")
		}
	})

	t.Run("When disk type is invalid it should error", func(t *testing.T) {
		o := valid()
		o.diskType = "pd-extreme"
		if err := o.validate(); err == nil {
			t.Error("expected error for invalid disk type")
		}
	})

	t.Run("When replicas is negative it should error", func(t *testing.T) {
		o := valid()
		o.replicas = -1
		if err := o.validate(); err == nil {
			t.Error("expected error for negative replicas")
		}
	})
}

func TestBuildNodePool(t *testing.T) {
	t.Run("When given valid options it should produce a correct NodePool object", func(t *testing.T) {
		o := &createOptions{
			clusterRef:   "my-cluster",
			replicas:     3,
			instanceType: "n2-standard-8",
			diskSize:     200,
			diskType:     "pd-ssd",
			zone:         "us-central1-a",
			version:      "4.22.0",
			channelGroup: "candidate",
		}

		np := o.buildNodePool("workers")

		if np.Name != "workers" {
			t.Errorf("expected name 'workers', got %q", np.Name)
		}
		if np.Kind != "NodePool" {
			t.Errorf("expected kind 'NodePool', got %q", np.Kind)
		}
		if np.APIVersion != gcpv1.GroupVersion.String() {
			t.Errorf("expected apiVersion %q, got %q", gcpv1.GroupVersion.String(), np.APIVersion)
		}
		if np.Spec.ClusterID != "my-cluster" {
			t.Errorf("expected clusterID 'my-cluster', got %q", np.Spec.ClusterID)
		}
		if np.Spec.NodeCount == nil || *np.Spec.NodeCount != 3 {
			t.Errorf("expected nodeCount 3, got %v", np.Spec.NodeCount)
		}
		if np.Spec.Platform.Type != "GCP" {
			t.Errorf("expected platform type 'GCP', got %q", np.Spec.Platform.Type)
		}
		if np.Spec.Platform.GCP == nil {
			t.Fatal("expected GCP platform to be non-nil")
		}
		if np.Spec.Platform.GCP.MachineType != "n2-standard-8" {
			t.Errorf("expected machineType 'n2-standard-8', got %q", np.Spec.Platform.GCP.MachineType)
		}
		if np.Spec.Platform.GCP.DiskSizeGB != 200 {
			t.Errorf("expected diskSizeGB 200, got %d", np.Spec.Platform.GCP.DiskSizeGB)
		}
		if np.Spec.Platform.GCP.DiskType != "pd-ssd" {
			t.Errorf("expected diskType 'pd-ssd', got %q", np.Spec.Platform.GCP.DiskType)
		}
		if np.Spec.Platform.GCP.Zone != "us-central1-a" {
			t.Errorf("expected zone 'us-central1-a', got %q", np.Spec.Platform.GCP.Zone)
		}
		if np.Spec.Release.Version != "4.22.0" {
			t.Errorf("expected version '4.22.0', got %q", np.Spec.Release.Version)
		}
		if np.Spec.Release.ChannelGroup != "candidate" {
			t.Errorf("expected channelGroup 'candidate', got %q", np.Spec.Release.ChannelGroup)
		}
	})

	t.Run("When zone is empty it should leave zone unset", func(t *testing.T) {
		o := &createOptions{
			clusterRef:   "my-cluster",
			replicas:     1,
			instanceType: "n2-standard-4",
			diskSize:     100,
			diskType:     "pd-balanced",
			version:      "4.22.0",
			channelGroup: "stable",
		}

		np := o.buildNodePool("workers")
		if np.Spec.Platform.GCP.Zone != "" {
			t.Errorf("expected empty zone, got %q", np.Spec.Platform.GCP.Zone)
		}
	})
}

func TestFilterNodePoolsByCluster(t *testing.T) {
	pools := []gcpv1.NodePool{
		{Spec: gcpv1.NodePoolSpec{ClusterID: "cluster-a"}},
		{Spec: gcpv1.NodePoolSpec{ClusterID: "cluster-b"}},
		{Spec: gcpv1.NodePoolSpec{ClusterID: "cluster-a"}},
	}

	t.Run("When cluster name is empty it should return all pools", func(t *testing.T) {
		got := filterNodePoolsByCluster(pools, "")
		if len(got) != 3 {
			t.Errorf("expected 3 pools, got %d", len(got))
		}
	})

	t.Run("When cluster name matches it should return only matching pools", func(t *testing.T) {
		got := filterNodePoolsByCluster(pools, "cluster-a")
		if len(got) != 2 {
			t.Errorf("expected 2 pools, got %d", len(got))
		}
		for _, np := range got {
			if np.Spec.ClusterID != "cluster-a" {
				t.Errorf("expected clusterID 'cluster-a', got %q", np.Spec.ClusterID)
			}
		}
	})

	t.Run("When cluster name does not match it should return no pools", func(t *testing.T) {
		got := filterNodePoolsByCluster(pools, "cluster-z")
		if len(got) != 0 {
			t.Errorf("expected 0 pools, got %d", len(got))
		}
	})
}
