package output

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestPrintResourceYAML(t *testing.T) {
	type resource struct {
		APIVersion string `json:"apiVersion"`
		ClusterID  string `json:"clusterID"`
	}

	var buf bytes.Buffer
	if err := PrintResourceYAML(&buf, resource{
		APIVersion: "gcp.managed.openshift.io/v1",
		ClusterID:  "example",
	}); err != nil {
		t.Fatalf("printing resource YAML: %v", err)
	}

	got := buf.String()
	for _, want := range []string{
		"apiVersion: gcp.managed.openshift.io/v1",
		"clusterID: example",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestPrintResultTextResourceTable(t *testing.T) {
	data := map[string]interface{}{
		"resource_type": "namespaces",
		"items": []interface{}{
			map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "test-namespace",
					"creationTimestamp": "2026-09-01T00:00:00Z",
				},
				"status": map[string]interface{}{"phase": "Active"},
			},
		},
	}

	var buf bytes.Buffer
	if err := PrintResult(&buf, FormatText, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"NAME", "STATUS", "AGE", "test-namespace", "Active"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.HasPrefix(got, "{") {
		t.Errorf("expected text table, got JSON:\n%s", got)
	}
}

func TestPrintResultTextEmptyDesires(t *testing.T) {
	data := map[string]interface{}{
		"resource_type": "desires",
		"items":         []interface{}{},
	}
	var buf bytes.Buffer
	if err := PrintResult(&buf, FormatText, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := buf.String(), "No desires found.\n"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestPrintResultTextUnknownShapeReturnsError(t *testing.T) {
	var buf bytes.Buffer
	err := PrintResult(&buf, FormatText, map[string]interface{}{"items": []interface{}{map[string]interface{}{"name": "unknown"}}})
	if err == nil || !strings.Contains(err.Error(), "resource_type") {
		t.Fatalf("expected resource_type error, got %v", err)
	}
}

func TestPrintResultTextRejectsNonObjectItems(t *testing.T) {
	tests := []struct {
		name string
		item interface{}
	}{
		{name: "nil", item: nil},
		{name: "scalar", item: "invalid"},
		{name: "array", item: []interface{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			err := PrintResult(&buf, FormatText, map[string]interface{}{
				"resource_type": "pods",
				"items": []interface{}{
					map[string]interface{}{"metadata": map[string]interface{}{"name": "valid"}},
					tt.item,
				},
			})
			if err == nil || !strings.Contains(err.Error(), "item 2 to be an object") {
				t.Fatalf("expected item shape error, got %v", err)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name string
		dur  time.Duration
		want string
	}{
		{"30 seconds", 30 * time.Second, "30s"},
		{"5 minutes", 5 * time.Minute, "5m"},
		{"2 hours", 2 * time.Hour, "2h"},
		{"3 days", 72 * time.Hour, "3d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatDuration(tt.dur); got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.dur, got, tt.want)
			}
		})
	}
}

func TestConditionStatus(t *testing.T) {
	status := map[string]interface{}{
		"conditions": []interface{}{
			map[string]interface{}{"type": "Ready", "status": "True"},
			map[string]interface{}{"type": "Available", "status": "False"},
			map[string]interface{}{"Type": "Successful", "Status": "True"},
		},
	}
	if got := conditionStatus(status, "Ready"); got != "True" {
		t.Errorf("expected 'True', got %q", got)
	}
	if got := conditionStatus(status, "Available"); got != "False" {
		t.Errorf("expected 'False', got %q", got)
	}
	if got := conditionStatus(status, "Missing"); got != "Unknown" {
		t.Errorf("expected 'Unknown' for missing condition, got %q", got)
	}
	if got := conditionStatus(status, "Successful"); got != "True" {
		t.Errorf("expected 'True', got %q", got)
	}
	if got := conditionStatus(map[string]interface{}{}, "Ready"); got != "Unknown" {
		t.Errorf("expected 'Unknown' for no conditions, got %q", got)
	}
}

func TestPodReadyCounts(t *testing.T) {
	tests := []struct {
		name      string
		status    map[string]interface{}
		wantReady int
		wantTotal int
	}{
		{
			name: "When all containers are ready it should report full count",
			status: map[string]interface{}{
				"containerStatuses": []interface{}{
					map[string]interface{}{"ready": true},
					map[string]interface{}{"ready": true},
				},
			},
			wantReady: 2, wantTotal: 2,
		},
		{
			name: "When one container is not ready it should report partial count",
			status: map[string]interface{}{
				"containerStatuses": []interface{}{
					map[string]interface{}{"ready": true},
					map[string]interface{}{"ready": false},
				},
			},
			wantReady: 1, wantTotal: 2,
		},
		{
			name:      "When no container statuses exist it should return zeros",
			status:    map[string]interface{}{},
			wantReady: 0, wantTotal: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready, total := podReadyCounts(tt.status)
			if ready != tt.wantReady || total != tt.wantTotal {
				t.Errorf("got %d/%d, want %d/%d", ready, total, tt.wantReady, tt.wantTotal)
			}
		})
	}
}

func TestPodEffectiveStatus(t *testing.T) {
	tests := []struct {
		name   string
		status map[string]interface{}
		want   string
	}{
		{
			name:   "When pod is running it should show Running",
			status: map[string]interface{}{"phase": "Running", "containerStatuses": []interface{}{map[string]interface{}{"state": map[string]interface{}{"running": map[string]interface{}{}}}}},
			want:   "Running",
		},
		{
			name: "When container is in CrashLoopBackOff it should show that reason",
			status: map[string]interface{}{
				"phase": "Running",
				"containerStatuses": []interface{}{
					map[string]interface{}{"state": map[string]interface{}{"waiting": map[string]interface{}{"reason": "CrashLoopBackOff"}}},
				},
			},
			want: "CrashLoopBackOff",
		},
		{
			name: "When init container is waiting it should show Init prefix",
			status: map[string]interface{}{
				"phase": "Pending",
				"initContainerStatuses": []interface{}{
					map[string]interface{}{"state": map[string]interface{}{"waiting": map[string]interface{}{"reason": "ImagePullBackOff"}}},
				},
			},
			want: "Init:ImagePullBackOff",
		},
		{
			name:   "When no containers exist it should fall back to phase",
			status: map[string]interface{}{"phase": "Pending"},
			want:   "Pending",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podEffectiveStatus(tt.status); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNodeRoles(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]interface{}
		want   string
	}{
		{
			name:   "When labels contain worker and control-plane it should return sorted roles",
			labels: map[string]interface{}{"node-role.kubernetes.io/worker": "", "node-role.kubernetes.io/control-plane": ""},
			want:   "control-plane,worker",
		},
		{
			name:   "When no role labels exist it should return none",
			labels: map[string]interface{}{"app": "test"},
			want:   "<none>",
		},
		{
			name:   "When labels are empty it should return none",
			labels: map[string]interface{}{},
			want:   "<none>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nodeRoles(tt.labels); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintResourceTable_EmptyItems(t *testing.T) {
	var buf bytes.Buffer
	err := PrintResourceTable(&buf, map[string]interface{}{"items": []interface{}{}}, "pods")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "No pods found") {
		t.Errorf("expected 'No pods found', got %q", buf.String())
	}
}

func TestPrintResourceTable_SingleResource(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"resource": map[string]interface{}{
			"metadata": map[string]interface{}{"name": "my-svc", "namespace": "default", "creationTimestamp": "2025-01-01T00:00:00Z"},
			"spec":     map[string]interface{}{"type": "ClusterIP", "clusterIP": "10.0.0.1"},
		},
	}
	if err := PrintResourceTable(&buf, data, "services"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "my-svc") || !strings.Contains(out, "ClusterIP") {
		t.Errorf("expected service details in output, got:\n%s", out)
	}
}

func TestStripCodeFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain text", `{"key":"val"}`, `{"key":"val"}`},
		{"json fence", "```json\n{\"key\":\"val\"}\n```", `{"key":"val"}`},
		{"bare fence", "```\n{\"key\":\"val\"}\n```", `{"key":"val"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripCodeFence(tt.input); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWrapText(t *testing.T) {
	short := "short text"
	if lines := wrapText(short, 80); len(lines) != 1 || lines[0] != short {
		t.Errorf("short text should not wrap, got %v", lines)
	}

	long := "This is a much longer sentence that should definitely be wrapped at some reasonable point in the output"
	lines := wrapText(long, 40)
	if len(lines) < 2 {
		t.Errorf("expected multiple lines for long text, got %d", len(lines))
	}
	for _, line := range lines {
		if len(line) > 40 {
			t.Errorf("line exceeds width: %q (%d chars)", line, len(line))
		}
	}
}

func TestSortItems(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{"metadata": map[string]interface{}{"namespace": "b-ns", "name": "pod-1"}},
		map[string]interface{}{"metadata": map[string]interface{}{"namespace": "a-ns", "name": "pod-2"}},
		map[string]interface{}{"metadata": map[string]interface{}{"namespace": "a-ns", "name": "pod-1"}},
	}
	SortItems(items)

	first := AsMap(AsMap(items[0])["metadata"])
	if GetString(first, "namespace") != "a-ns" || GetString(first, "name") != "pod-1" {
		t.Errorf("first item should be a-ns/pod-1, got %s/%s", GetString(first, "namespace"), GetString(first, "name"))
	}
	last := AsMap(AsMap(items[2])["metadata"])
	if GetString(last, "namespace") != "b-ns" {
		t.Errorf("last item should be in b-ns, got %s", GetString(last, "namespace"))
	}
}

func TestPrintAnalysis_WithStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"name": "test-pod",
		"analysis": map[string]interface{}{
			"pod_phase":          "Running",
			"events_count":       float64(3),
			"log_lines_analyzed": float64(50),
			"ai_analysis":        `{"summary":"Pod is healthy.","severity":"LOW","errors_detected":[],"root_cause":"None","recommended_actions":["Continue monitoring"]}`,
		},
	}
	if err := PrintAnalysis(&buf, data, "test-ns"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"POD ANALYSIS", "test-pod", "test-ns", "AI ANALYSIS", "LOW", "Pod is healthy"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestPrintDiagnosis(t *testing.T) {
	var buf bytes.Buffer
	err := PrintDiagnosis(&buf,
		"Pod is OOMKilled due to memory limit",
		"high",
		"critical",
		[]string{"Container exceeded 512Mi limit", "OOMKilled event recorded"},
		"Increase memory limit to 1Gi",
		map[string]interface{}{
			"steps": []interface{}{"Checked pod status", "Analyzed events", "Reviewed resource limits"},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"DIAGNOSIS",
		"CRITICAL",
		"high",
		"OOMKilled due to memory limit",
		"Container exceeded 512Mi limit",
		"OOMKilled event recorded",
		"Increase memory limit to 1Gi",
		"Investigation Steps",
		"Checked pod status",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintDiagnosis_MinimalFields(t *testing.T) {
	var buf bytes.Buffer
	err := PrintDiagnosis(&buf, "Unknown issue", "", "", nil, "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "DIAGNOSIS") {
		t.Error("expected DIAGNOSIS header")
	}
	if !strings.Contains(out, "Unknown issue") {
		t.Error("expected root cause in output")
	}
}

func TestPrintAnalysis_FallbackForNonJSON(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"name": "test-pod",
		"analysis": map[string]interface{}{
			"pod_phase":    "CrashLoopBackOff",
			"ai_analysis":  "The pod is crashing because of an OOM error.",
			"events_count": float64(0),
		},
	}
	if err := PrintAnalysis(&buf, data, "ns"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "OOM error") {
		t.Error("expected raw analysis text in fallback output")
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"GiB", float64(1273696256), "1.2 GiB"},
		{"MiB", float64(52428800), "50.0 MiB"},
		{"KiB", float64(2048), "2.0 KiB"},
		{"bytes", float64(512), "512 B"},
		{"int", 1073741824, "1.0 GiB"},
		{"string fallback", "unknown", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatBytes(tt.input); got != tt.want {
				t.Errorf("FormatBytes(%v) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestPrintTable_Basic(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]interface{}{"name": "alice", "age": float64(30)},
		map[string]interface{}{"name": "bob", "age": float64(25)},
	}
	cols := []Column{
		{Header: "NAME", Path: "name"},
		{Header: "AGE", Path: "age"},
	}
	if err := PrintTable(&buf, data, cols); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"NAME", "AGE", "alice", "bob", "30", "25"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintTable_WithTransform(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]interface{}{"endpoint": "https://etcd-0.etcd-discovery.clusters-test.svc:2379", "health": true, "took": "10ms"},
		map[string]interface{}{"endpoint": "https://etcd-1.etcd-discovery.clusters-test.svc:2379", "health": false, "took": "5001ms", "error": "context deadline exceeded"},
	}
	cols := []Column{
		{Header: "ENDPOINT", Path: "endpoint", Transform: TransformShortenEndpoint},
		{Header: "HEALTH", Path: "health", Transform: TransformBool},
		{Header: "TOOK", Path: "took"},
		{Header: "ERROR", Path: "error", OmitEmpty: true},
	}
	if err := PrintTable(&buf, data, cols); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"ENDPOINT", "HEALTH", "TOOK", "ERROR", "etcd-0", "etcd-1", "true", "false", "10ms", "5001ms", "context deadline exceeded"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintTable_OmitEmptyColumn(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]interface{}{"name": "a", "error": ""},
		map[string]interface{}{"name": "b"},
	}
	cols := []Column{
		{Header: "NAME", Path: "name"},
		{Header: "ERROR", Path: "error", OmitEmpty: true},
	}
	if err := PrintTable(&buf, data, cols); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "ERROR") {
		t.Errorf("expected ERROR column to be omitted when all values empty:\n%s", out)
	}
}

func TestPrintTable_WithCompute(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]interface{}{
			"Endpoint": "https://etcd-0.svc:2379",
			"Status": map[string]interface{}{
				"leader": float64(1038), "version": "3.5.21",
				"header": map[string]interface{}{"member_id": float64(4426)},
			},
		},
		map[string]interface{}{
			"Endpoint": "https://etcd-1.svc:2379",
			"Status": map[string]interface{}{
				"leader": float64(1038), "version": "3.5.21",
				"header": map[string]interface{}{"member_id": float64(1038)},
			},
		},
	}
	cols := []Column{
		{Header: "ENDPOINT", Path: "Endpoint", Transform: TransformShortenEndpoint},
		{Header: "ROLE", Compute: func(item map[string]interface{}, allItems []interface{}) string {
			var leaderID float64
			for _, it := range allItems {
				status := AsMap(AsMap(it)["Status"])
				if l, ok := status["leader"].(float64); ok {
					leaderID = l
					break
				}
			}
			header := AsMap(AsMap(item["Status"])["header"])
			if memberID, ok := header["member_id"].(float64); ok && memberID == leaderID {
				return "leader"
			}
			return "follower"
		}},
	}
	if err := PrintTable(&buf, data, cols); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "leader") || !strings.Contains(out, "follower") {
		t.Errorf("expected leader/follower roles:\n%s", out)
	}
}

func TestPrintTable_NestedPath(t *testing.T) {
	var buf bytes.Buffer
	data := []interface{}{
		map[string]interface{}{
			"Status": map[string]interface{}{
				"header": map[string]interface{}{"revision": float64(139328)},
				"dbSize": float64(1273696256),
			},
		},
	}
	cols := []Column{
		{Header: "REVISION", Path: "Status.header.revision", Transform: TransformUint64},
		{Header: "DB SIZE", Path: "Status.dbSize", Transform: TransformBytes},
	}
	if err := PrintTable(&buf, data, cols); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "139328") || !strings.Contains(out, "1.2 GiB") {
		t.Errorf("expected resolved nested values:\n%s", out)
	}
}

func TestTransformShortenEndpoint(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{"https://etcd-0.etcd-discovery.clusters-test.svc:2379", "etcd-0"},
		{"https://etcd-1.etcd-discovery.clusters-test.svc:2379", "etcd-1"},
		{"https://single-host:2379", "https://single-host:2379"},
	}
	for _, tt := range tests {
		if got := TransformShortenEndpoint(tt.input); got != tt.want {
			t.Errorf("TransformShortenEndpoint(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTransformShortenURL(t *testing.T) {
	tests := []struct {
		input interface{}
		want  string
	}{
		{"https://etcd-0.etcd-discovery.clusters-test.svc:2380", "etcd-0:2380"},
		{"https://etcd-1.etcd-discovery.clusters-test.svc:2379", "etcd-1:2379"},
	}
	for _, tt := range tests {
		if got := TransformShortenURL(tt.input); got != tt.want {
			t.Errorf("TransformShortenURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatAccessModes(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"single RWO", []interface{}{"ReadWriteOnce"}, "RWO"},
		{"single ROX", []interface{}{"ReadOnlyMany"}, "ROX"},
		{"single RWX", []interface{}{"ReadWriteMany"}, "RWX"},
		{"single RWOP", []interface{}{"ReadWriteOncePod"}, "RWOP"},
		{"multiple modes", []interface{}{"ReadWriteOnce", "ReadOnlyMany"}, "RWO,ROX"},
		{"unknown mode", []interface{}{"CustomMode"}, "CustomMode"},
		{"nil input", nil, ""},
		{"empty slice", []interface{}{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatAccessModes(tt.input); got != tt.want {
				t.Errorf("formatAccessModes() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPrintPVCTable(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "data-etcd-0",
					"namespace":         "clusters-test-ns",
					"creationTimestamp": "2025-01-01T00:00:00Z",
				},
				"spec": map[string]interface{}{
					"volumeName":       "pvc-68d9514c-44cd-484e-aefa-7084db20348c",
					"accessModes":      []interface{}{"ReadWriteOnce"},
					"storageClassName": "standard-rwo",
				},
				"status": map[string]interface{}{
					"phase":    "Bound",
					"capacity": map[string]interface{}{"storage": "21Gi"},
				},
			},
		},
	}
	if err := PrintResourceTable(&buf, data, "persistentvolumeclaims"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"NAMESPACE", "NAME", "STATUS", "VOLUME", "CAPACITY", "ACCESS MODES", "STORAGECLASS", "AGE",
		"clusters-test-ns", "data-etcd-0", "Bound", "pvc-68d9514c", "21Gi", "RWO", "standard-rwo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintPVTable(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{
				"metadata": map[string]interface{}{
					"name":              "pvc-1e2be0c7-8d1f-43a6-9a6b-31c4a9eeadd4",
					"creationTimestamp": "2025-01-01T00:00:00Z",
				},
				"spec": map[string]interface{}{
					"capacity":                      map[string]interface{}{"storage": "8Gi"},
					"accessModes":                   []interface{}{"ReadWriteOnce"},
					"persistentVolumeReclaimPolicy": "Delete",
					"storageClassName":              "standard-rwo",
					"claimRef": map[string]interface{}{
						"namespace": "clusters-test-ns",
						"name":      "data-etcd-0",
					},
				},
				"status": map[string]interface{}{
					"phase": "Bound",
				},
			},
		},
	}
	if err := PrintResourceTable(&buf, data, "persistentvolumes"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"NAME", "CAPACITY", "ACCESS MODES", "RECLAIM POLICY", "STATUS", "CLAIM", "STORAGECLASS", "AGE",
		"pvc-1e2be0c7", "8Gi", "RWO", "Delete", "Bound", "clusters-test-ns/data-etcd-0", "standard-rwo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestPrintDesiresTable(t *testing.T) {
	var buf bytes.Buffer
	data := map[string]interface{}{
		"resource_type": "desires",
		"items": []interface{}{
			map[string]interface{}{
				"database":                    "specs",
				"desire_type":                 "apply",
				"document_id":                 "4a762c3d-a635-561b-93bb-b0c5b7efcd99",
				"create_time":                 "2026-09-07T14:00:03.185552Z",
				"update_time":                 "2026-09-07T14:00:03.185552Z",
				"observed_desire_update_time": nil,
				"value": map[string]interface{}{
					"targetItem": map[string]interface{}{
						"group":     "cert-manager.io",
						"resource":  "certificates",
						"namespace": "clusters-test",
						"name":      "external-api-cert",
					},
				},
			},
			map[string]interface{}{
				"database":                    "status",
				"desire_type":                 "apply",
				"document_id":                 "4a762c3d-a635-561b-93bb-b0c5b7efcd99",
				"create_time":                 "2026-09-07T14:00:03.589434Z",
				"update_time":                 "2026-09-07T14:00:03.589434Z",
				"observed_desire_update_time": "2026-09-07T14:00:03.185552Z",
				"value": map[string]interface{}{
					"appliedResourceGeneration": float64(987654321),
					"conditions": []interface{}{
						map[string]interface{}{"Type": "Successful", "Status": "True"},
						map[string]interface{}{"Type": "Degraded", "Status": "False"},
					},
				},
			},
		},
	}

	if err := PrintResult(&buf, FormatText, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "DATABASE") {
		t.Errorf("expected DATABASE as the first column:\n%s", out)
	}
	for _, want := range []string{
		"TYPE", "DATABASE", "DOCUMENT ID", "RESOURCE", "NAMESPACE", "NAME", "GENERATION", "SUCCESSFUL", "DEGRADED", "UPDATED", "OBSERVED",
		"apply", "specs", "status", "4a762c3d", "certificates.cert-manager.io",
		"clusters-test", "external-api-cert", "True", "False", "2026-09-07T14:00:03.185552Z", "2026-09-07T14:00:03.589434Z",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "CREATED") {
		t.Errorf("output contains CREATED column:\n%s", out)
	}
	if strings.Contains(out, "4a762c3d-a635-561b-93bb-b0c5b7efcd99") {
		t.Errorf("output contains full document ID:\n%s", out)
	}
	statusRowFound := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "status") {
			statusRowFound = true
			if !strings.Contains(line, "987654321") {
				t.Errorf("status row missing generation:\n%s", line)
			}
		}
	}
	if !statusRowFound {
		t.Errorf("output missing status row:\n%s", out)
	}
	if strings.Contains(out, "<nil>") {
		t.Errorf("output contains nil value:\n%s", out)
	}
}

func TestTransformShortID(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want string
	}{
		{name: "UUID", in: "4a762c3d-a635-561b-93bb-b0c5b7efcd99", want: "4a762c3d"},
		{name: "short", in: "abc123", want: "abc123"},
		{name: "missing", in: nil, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TransformShortID(tt.in); got != tt.want {
				t.Errorf("TransformShortID(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
