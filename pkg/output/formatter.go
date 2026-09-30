// Package output provides formatting utilities for CLI output.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"
	resourceyaml "sigs.k8s.io/yaml"
)

// Format represents an output format.
type Format string

const (
	FormatText Format = "text"
	FormatJSON Format = "json"
	FormatYAML Format = "yaml"
)

// ParseFormat parses a string into a Format, defaulting to text.
func ParseFormat(s string) Format {
	switch strings.ToLower(s) {
	case "json":
		return FormatJSON
	case "yaml":
		return FormatYAML
	default:
		return FormatText
	}
}

// PrintJSON writes data as indented JSON to the writer.
func PrintJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// PrintYAML writes data as YAML to the writer.
func PrintYAML(w io.Writer, data interface{}) error {
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(data); err != nil {
		_ = enc.Close()
		return err
	}
	return enc.Close()
}

// PrintResourceYAML writes an API resource as YAML using its JSON field names.
func PrintResourceYAML(w io.Writer, data interface{}) error {
	encoded, err := resourceyaml.Marshal(data)
	if err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}

// PrintResult formats and prints an execution result based on the output format.
func PrintResult(w io.Writer, format Format, data map[string]interface{}) error {
	switch format {
	case FormatYAML:
		return PrintYAML(w, data)
	case FormatJSON:
		return PrintJSON(w, data)
	case FormatText:
		return PrintResourceTable(w, data, "")
	default:
		return fmt.Errorf("unsupported output format %q", format)
	}
}

// Table provides a simple table writer for text output.
type Table struct {
	w       *tabwriter.Writer
	headers []string
}

// NewTable creates a new table with the given headers.
func NewTable(w io.Writer, headers ...string) *Table {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	t := &Table{w: tw, headers: headers}
	fmt.Fprintln(tw, strings.Join(headers, "\t"))
	return t
}

// AddRow adds a row to the table.
func (t *Table) AddRow(values ...string) {
	fmt.Fprintln(t.w, strings.Join(values, "\t"))
}

// Flush writes the table output.
func (t *Table) Flush() error {
	return t.w.Flush()
}

// PrintResourceTable formats a structured resource result as a table.
func PrintResourceTable(w io.Writer, result map[string]interface{}, resourceType string) error {
	items, ok := result["items"].([]interface{})
	if !ok {
		if resource, rOk := result["resource"].(map[string]interface{}); rOk {
			items = []interface{}{resource}
		} else {
			return fmt.Errorf("text output requires items or resource in the result")
		}
	}
	for i, item := range items {
		if _, ok := item.(map[string]interface{}); !ok {
			return fmt.Errorf("text output requires item %d to be an object", i+1)
		}
	}

	if resourceType == "" {
		resourceType, _ = result["resource_type"].(string)
	}
	if resourceType == "" {
		return fmt.Errorf("text output requires resource_type")
	}

	if len(items) == 0 {
		fmt.Fprintf(w, "No %s found.\n", resourceType)
		return nil
	}

	switch resourceType {
	case "pods":
		return printPodsTable(w, items)
	case "deployments":
		return printDeploymentsTable(w, items)
	case "hostedclusters":
		return printHostedClustersTable(w, items)
	case "services", "svc":
		return printServicesTable(w, items)
	case "namespaces", "ns":
		return printNamespacesTable(w, items)
	case "nodes":
		return printNodesTable(w, items)
	case "events", "ev":
		return printEventsTable(w, items)
	case "configmaps", "cm":
		return printConfigMapsTable(w, items)
	case "desires":
		return printDesiresTable(w, items)
	case "persistentvolumeclaims", "pvc":
		return PrintTable(w, items, []Column{
			{Header: "NAMESPACE", Path: "metadata.namespace"},
			{Header: "NAME", Path: "metadata.name"},
			{Header: "STATUS", Path: "status.phase"},
			{Header: "VOLUME", Path: "spec.volumeName"},
			{Header: "CAPACITY", Path: "status.capacity.storage"},
			{Header: "ACCESS MODES", Path: "spec.accessModes", Transform: TransformAccessModes},
			{Header: "STORAGECLASS", Path: "spec.storageClassName"},
			{Header: "AGE", Path: "metadata.creationTimestamp", Transform: TransformAge},
		})
	case "persistentvolumes", "pv":
		return PrintTable(w, items, []Column{
			{Header: "NAME", Path: "metadata.name"},
			{Header: "CAPACITY", Path: "spec.capacity.storage"},
			{Header: "ACCESS MODES", Path: "spec.accessModes", Transform: TransformAccessModes},
			{Header: "RECLAIM POLICY", Path: "spec.persistentVolumeReclaimPolicy"},
			{Header: "STATUS", Path: "status.phase"},
			{Header: "CLAIM", Compute: func(item map[string]interface{}, _ []interface{}) string {
				claimRef := AsMap(item["spec"])
				cr := AsMap(claimRef["claimRef"])
				if ns := GetString(cr, "namespace"); ns != "" {
					return ns + "/" + GetString(cr, "name")
				}
				return ""
			}},
			{Header: "STORAGECLASS", Path: "spec.storageClassName"},
			{Header: "AGE", Path: "metadata.creationTimestamp", Transform: TransformAge},
		})
	default:
		return printGenericTable(w, items, resourceType)
	}
}

func printPodsTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAMESPACE", "NAME", "READY", "STATUS", "RESTARTS", "AGE")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		status := AsMap(m["status"])

		readyCount, totalCount := podReadyCounts(status)
		podStatus := podEffectiveStatus(status)
		restarts := podRestartCount(status)

		t.AddRow(
			GetString(meta, "namespace"),
			GetString(meta, "name"),
			fmt.Sprintf("%d/%d", readyCount, totalCount),
			podStatus,
			fmt.Sprintf("%d", restarts),
			age(GetString(meta, "creationTimestamp")),
		)
	}
	return t.Flush()
}

func printDeploymentsTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAMESPACE", "NAME", "READY", "UP-TO-DATE", "AVAILABLE", "AGE")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		spec := AsMap(m["spec"])
		status := AsMap(m["status"])

		desired := getInt(spec, "replicas")
		ready := getInt(status, "readyReplicas")
		updated := getInt(status, "updatedReplicas")
		available := getInt(status, "availableReplicas")

		t.AddRow(
			GetString(meta, "namespace"),
			GetString(meta, "name"),
			fmt.Sprintf("%d/%d", ready, desired),
			fmt.Sprintf("%d", updated),
			fmt.Sprintf("%d", available),
			age(GetString(meta, "creationTimestamp")),
		)
	}
	return t.Flush()
}

func printHostedClustersTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAMESPACE", "NAME", "VERSION", "PROGRESS", "AVAILABLE", "AGE")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		spec := AsMap(m["spec"])
		status := AsMap(m["status"])

		release := AsMap(spec["release"])
		version := GetString(release, "image")
		if version == "" {
			version = "<none>"
		} else if len(version) > 40 {
			version = version[:40] + "..."
		}

		progress := GetString(status, "progress")
		available := conditionStatus(status, "Available")

		t.AddRow(
			GetString(meta, "namespace"),
			GetString(meta, "name"),
			version,
			progress,
			available,
			age(GetString(meta, "creationTimestamp")),
		)
	}
	return t.Flush()
}

func printServicesTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAMESPACE", "NAME", "TYPE", "CLUSTER-IP", "AGE")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		spec := AsMap(m["spec"])

		t.AddRow(
			GetString(meta, "namespace"),
			GetString(meta, "name"),
			GetString(spec, "type"),
			GetString(spec, "clusterIP"),
			age(GetString(meta, "creationTimestamp")),
		)
	}
	return t.Flush()
}

func printConfigMapsTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAMESPACE", "NAME", "DATA", "AGE")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		data := AsMap(m["data"])

		t.AddRow(
			GetString(meta, "namespace"),
			GetString(meta, "name"),
			fmt.Sprintf("%d", len(data)),
			age(GetString(meta, "creationTimestamp")),
		)
	}
	return t.Flush()
}

func printDesiresTable(w io.Writer, items []interface{}) error {
	return PrintTable(w, items, []Column{
		{Header: "DATABASE", Path: "database"},
		{Header: "TYPE", Path: "desire_type"},
		{Header: "DOCUMENT ID", Path: "document_id", Transform: TransformShortID},
		{Header: "RESOURCE", Compute: func(item map[string]interface{}, _ []interface{}) string {
			target := AsMap(AsMap(item["value"])["targetItem"])
			resource := GetString(target, "resource")
			if group := GetString(target, "group"); group != "" {
				return resource + "." + group
			}
			return resource
		}},
		{Header: "NAMESPACE", Path: "value.targetItem.namespace", OmitEmpty: true},
		{Header: "NAME", Path: "value.targetItem.name", OmitEmpty: true},
		{Header: "GENERATION", Path: "value.appliedResourceGeneration", Transform: TransformUint64, OmitEmpty: true},
		{Header: "SUCCESSFUL", Compute: func(item map[string]interface{}, _ []interface{}) string {
			if GetString(item, "database") != "status" {
				return ""
			}
			return conditionStatus(AsMap(item["value"]), "Successful")
		}, OmitEmpty: true},
		{Header: "DEGRADED", Compute: func(item map[string]interface{}, _ []interface{}) string {
			if GetString(item, "database") != "status" {
				return ""
			}
			return conditionStatus(AsMap(item["value"]), "Degraded")
		}, OmitEmpty: true},
		{Header: "UPDATED", Path: "update_time"},
		{Header: "OBSERVED", Path: "observed_desire_update_time", OmitEmpty: true},
	})
}

func formatAccessModes(v interface{}) string {
	modes, ok := v.([]interface{})
	if !ok || len(modes) == 0 {
		return ""
	}
	abbrevs := map[string]string{
		"ReadWriteOnce":    "RWO",
		"ReadOnlyMany":     "ROX",
		"ReadWriteMany":    "RWX",
		"ReadWriteOncePod": "RWOP",
	}
	parts := make([]string, 0, len(modes))
	for _, m := range modes {
		s := fmt.Sprintf("%v", m)
		if abbr, ok := abbrevs[s]; ok {
			parts = append(parts, abbr)
		} else {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ",")
}

func printNamespacesTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAME", "STATUS", "AGE")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		status := AsMap(m["status"])
		t.AddRow(
			GetString(meta, "name"),
			GetString(status, "phase"),
			age(GetString(meta, "creationTimestamp")),
		)
	}
	return t.Flush()
}

func printNodesTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "NAME", "STATUS", "ROLES", "AGE", "VERSION")
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		status := AsMap(m["status"])
		nodeInfo := AsMap(status["nodeInfo"])

		labels := AsMap(meta["labels"])
		roles := nodeRoles(labels)
		ready := conditionStatus(status, "Ready")
		readyStr := "NotReady"
		if ready == "True" {
			readyStr = "Ready"
		}

		t.AddRow(
			GetString(meta, "name"),
			readyStr,
			roles,
			age(GetString(meta, "creationTimestamp")),
			GetString(nodeInfo, "kubeletVersion"),
		)
	}
	return t.Flush()
}

func printEventsTable(w io.Writer, items []interface{}) error {
	t := NewTable(w, "LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE")
	for _, item := range items {
		m := AsMap(item)
		involvedObject := AsMap(m["involvedObject"])
		objRef := fmt.Sprintf("%s/%s", GetString(involvedObject, "kind"), GetString(involvedObject, "name"))

		lastTimestamp := GetString(m, "lastTimestamp")
		if lastTimestamp == "" {
			lastTimestamp = GetString(m, "eventTime")
		}

		t.AddRow(
			age(lastTimestamp),
			GetString(m, "type"),
			GetString(m, "reason"),
			objRef,
			GetString(m, "message"),
		)
	}
	return t.Flush()
}

func printGenericTable(w io.Writer, items []interface{}, resourceType string) error {
	clusterScoped := isClusterScoped(items)
	if clusterScoped {
		t := NewTable(w, "NAME", "AGE")
		for _, item := range items {
			m := AsMap(item)
			meta := AsMap(m["metadata"])
			t.AddRow(
				GetString(meta, "name"),
				age(GetString(meta, "creationTimestamp")),
			)
		}
		_ = t.Flush()
	} else {
		t := NewTable(w, "NAMESPACE", "NAME", "AGE")
		for _, item := range items {
			m := AsMap(item)
			meta := AsMap(m["metadata"])
			t.AddRow(
				GetString(meta, "namespace"),
				GetString(meta, "name"),
				age(GetString(meta, "creationTimestamp")),
			)
		}
		_ = t.Flush()
	}
	fmt.Fprintf(w, "\n%d %s found.\n", len(items), resourceType)
	return nil
}

func isClusterScoped(items []interface{}) bool {
	for _, item := range items {
		m := AsMap(item)
		meta := AsMap(m["metadata"])
		if GetString(meta, "namespace") != "" {
			return false
		}
	}
	return true
}

func nodeRoles(labels map[string]interface{}) string {
	var roles []string
	for key := range labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
			if role != "" {
				roles = append(roles, role)
			}
		}
	}
	if len(roles) == 0 {
		return "<none>"
	}
	sort.Strings(roles)
	return strings.Join(roles, ",")
}

// AsMap safely converts an interface to a string map.
func AsMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return map[string]interface{}{}
}

// GetString retrieves a string value from a map by key.
func GetString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}

func podEffectiveStatus(status map[string]interface{}) string {
	phase := GetString(status, "phase")

	containers, ok := status["containerStatuses"].([]interface{})
	if !ok || len(containers) == 0 {
		initContainers, iOk := status["initContainerStatuses"].([]interface{})
		if iOk {
			for i, ic := range initContainers {
				icm := AsMap(ic)
				state := AsMap(icm["state"])
				if waiting := AsMap(state["waiting"]); len(waiting) > 0 {
					reason := GetString(waiting, "reason")
					if reason != "" {
						return fmt.Sprintf("Init:%s", reason)
					}
					return fmt.Sprintf("Init:%d/%d", i, len(initContainers))
				}
				if terminated := AsMap(state["terminated"]); len(terminated) > 0 {
					if code := getInt(terminated, "exitCode"); code != 0 {
						return "Init:Error"
					}
				}
			}
		}
		return phase
	}

	for _, c := range containers {
		cm := AsMap(c)
		state := AsMap(cm["state"])

		if waiting := AsMap(state["waiting"]); len(waiting) > 0 {
			if reason := GetString(waiting, "reason"); reason != "" {
				return reason
			}
		}
		if terminated := AsMap(state["terminated"]); len(terminated) > 0 {
			if reason := GetString(terminated, "reason"); reason != "" {
				return reason
			}
		}
	}
	return phase
}

func podReadyCounts(status map[string]interface{}) (ready, total int) {
	containers, ok := status["containerStatuses"].([]interface{})
	if !ok {
		return 0, 0
	}
	total = len(containers)
	for _, c := range containers {
		cm := AsMap(c)
		if r, ok := cm["ready"].(bool); ok && r {
			ready++
		}
	}
	return
}

func podRestartCount(status map[string]interface{}) int {
	containers, ok := status["containerStatuses"].([]interface{})
	if !ok {
		return 0
	}
	total := 0
	for _, c := range containers {
		cm := AsMap(c)
		total += getInt(cm, "restartCount")
	}
	return total
}

func conditionStatus(status map[string]interface{}, condType string) string {
	conditions, ok := status["conditions"].([]interface{})
	if !ok {
		return "Unknown"
	}
	for _, c := range conditions {
		cm := AsMap(c)
		conditionType := GetString(cm, "type")
		if conditionType == "" {
			conditionType = GetString(cm, "Type")
		}
		if conditionType == condType {
			conditionStatus := GetString(cm, "status")
			if conditionStatus == "" {
				conditionStatus = GetString(cm, "Status")
			}
			return conditionStatus
		}
	}
	return "Unknown"
}

// Age formats a Kubernetes timestamp as a human-readable duration.
func Age(timestamp string) string {
	return age(timestamp)
}

func age(timestamp string) string {
	if timestamp == "" {
		return "<unknown>"
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return timestamp
	}
	return formatDuration(time.Since(t))
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours() / 24)
	return fmt.Sprintf("%dd", days)
}

// PrintAnalysis renders AI analysis output for a pod in a human-readable format.
func PrintAnalysis(w io.Writer, data map[string]interface{}, namespace string) error {
	name := GetString(data, "name")
	analysis := AsMap(data["analysis"])

	phase := GetString(analysis, "pod_phase")
	if phase == "" {
		phase = "Unknown"
	}
	eventsCount := getInt(analysis, "events_count")
	logLines := getInt(analysis, "log_lines_analyzed")

	fmt.Fprintln(w)
	fmt.Fprintln(w, "POD ANALYSIS")
	fmt.Fprintln(w, "============")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  Pod:       %s\n", name)
	fmt.Fprintf(w, "  Namespace: %s\n", namespace)
	fmt.Fprintf(w, "  Phase:     %s\n", phase)
	fmt.Fprintf(w, "  Events:    %d\n", eventsCount)
	fmt.Fprintf(w, "  Logs:      %d lines analyzed\n", logLines)
	fmt.Fprintln(w)

	aiAnalysis := GetString(analysis, "ai_analysis")
	aiError := GetString(analysis, "error")

	if aiError != "" {
		fmt.Fprintln(w, "AI ANALYSIS")
		fmt.Fprintln(w, "===========")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", aiError)
		fmt.Fprintln(w)
		return nil
	}

	if aiAnalysis == "" || aiAnalysis == "<nil>" {
		fmt.Fprintln(w, "AI ANALYSIS")
		fmt.Fprintln(w, "===========")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "  No analysis available.")
		fmt.Fprintln(w)
		return nil
	}

	fmt.Fprintln(w, "AI ANALYSIS")
	fmt.Fprintln(w, "===========")
	fmt.Fprintln(w)

	if rendered := renderStructuredAnalysis(w, aiAnalysis); rendered {
		return nil
	}

	fmt.Fprintln(w, aiAnalysis)
	fmt.Fprintln(w)
	return nil
}

// renderStructuredAnalysis attempts to parse the AI response as structured JSON
// and render it in a human-readable format. Returns true if it succeeded.
func renderStructuredAnalysis(w io.Writer, raw string) bool {
	cleaned := stripCodeFence(raw)

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return false
	}

	if _, ok := parsed["summary"]; !ok {
		return false
	}

	severity := stringVal(parsed, "severity")
	if severity != "" {
		fmt.Fprintf(w, "  Severity:  %s\n\n", severity)
	}

	if summary := stringVal(parsed, "summary"); summary != "" {
		printSection(w, "Summary", summary)
	}

	if errors := listVal(parsed, "errors_detected"); len(errors) > 0 {
		printListSection(w, "Errors Detected", errors)
	} else if errStr := stringVal(parsed, "errors_detected"); errStr != "" {
		printSection(w, "Errors Detected", errStr)
	}

	if rca := stringVal(parsed, "root_cause"); rca != "" {
		printSection(w, "Root Cause Analysis", rca)
	}

	if actions := listVal(parsed, "recommended_actions"); len(actions) > 0 {
		printNumberedSection(w, "Recommended Actions", actions)
	} else if actStr := stringVal(parsed, "recommended_actions"); actStr != "" {
		printSection(w, "Recommended Actions", actStr)
	}

	fmt.Fprintln(w)
	return true
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:len(s)-3]
	}
	return strings.TrimSpace(s)
}

func stringVal(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func listVal(m map[string]interface{}, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func printSection(w io.Writer, title, body string) {
	fmt.Fprintf(w, "  %s\n", title)
	for _, line := range wrapText(body, 76) {
		fmt.Fprintf(w, "    %s\n", line)
	}
	fmt.Fprintln(w)
}

func printListSection(w io.Writer, title string, items []string) {
	fmt.Fprintf(w, "  %s\n", title)
	for _, item := range items {
		lines := wrapText(item, 72)
		fmt.Fprintf(w, "    • %s\n", lines[0])
		for _, cont := range lines[1:] {
			fmt.Fprintf(w, "      %s\n", cont)
		}
	}
	fmt.Fprintln(w)
}

func printNumberedSection(w io.Writer, title string, items []string) {
	fmt.Fprintf(w, "  %s\n", title)
	for i, item := range items {
		lines := wrapText(item, 72)
		fmt.Fprintf(w, "    %d. %s\n", i+1, lines[0])
		for _, cont := range lines[1:] {
			fmt.Fprintf(w, "       %s\n", cont)
		}
	}
	fmt.Fprintln(w)
}

func wrapText(s string, width int) []string {
	if len(s) <= width {
		return []string{s}
	}
	var lines []string
	for len(s) > width {
		cut := width
		for cut > 0 && s[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = width
		}
		lines = append(lines, s[:cut])
		s = strings.TrimSpace(s[cut:])
	}
	if s != "" {
		lines = append(lines, s)
	}
	return lines
}

// PrintDiagnosis renders a structured diagnosis from the diagnose-agent in human-readable format.
func PrintDiagnosis(w io.Writer, rootCause, confidence, severity string, evidence []string, recommendation string, metadata map[string]interface{}) error {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "DIAGNOSIS")
	fmt.Fprintln(w, "=========")
	fmt.Fprintln(w)

	if severity != "" {
		fmt.Fprintf(w, "  Severity:   %s\n", strings.ToUpper(severity))
	}
	if confidence != "" {
		fmt.Fprintf(w, "  Confidence: %s\n", confidence)
	}
	fmt.Fprintln(w)

	if rootCause != "" {
		printSection(w, "Root Cause", rootCause)
	}

	if len(evidence) > 0 {
		printListSection(w, "Evidence", evidence)
	}

	if recommendation != "" {
		printSection(w, "Recommendation", recommendation)
	}

	if metadata != nil {
		steps, _ := metadata["steps"].([]interface{})
		if len(steps) > 0 {
			strs := make([]string, 0, len(steps))
			for _, s := range steps {
				if str, ok := s.(string); ok {
					strs = append(strs, str)
				}
			}
			if len(strs) > 0 {
				printNumberedSection(w, "Investigation Steps", strs)
			}
		}
	}

	return nil
}

// shortenEndpoint extracts "etcd-N" from a full etcd endpoint URL.
func shortenEndpoint(endpoint string) string {
	if parts := strings.Split(endpoint, "."); len(parts) > 1 {
		return strings.TrimPrefix(parts[0], "https://")
	}
	return endpoint
}

// FormatBytes converts a numeric byte count to a human-readable string.
func FormatBytes(v interface{}) string {
	var b float64
	switch n := v.(type) {
	case float64:
		b = n
	case int:
		b = float64(n)
	default:
		return fmt.Sprintf("%v", v)
	}

	const (
		KiB = 1024
		MiB = 1024 * KiB
		GiB = 1024 * MiB
	)

	switch {
	case b >= GiB:
		return fmt.Sprintf("%.1f GiB", b/GiB)
	case b >= MiB:
		return fmt.Sprintf("%.1f MiB", b/MiB)
	case b >= KiB:
		return fmt.Sprintf("%.1f KiB", b/KiB)
	default:
		return fmt.Sprintf("%.0f B", b)
	}
}

// Column defines a column for PrintTable.
type Column struct {
	// Header is the column header displayed in the table.
	Header string
	// Path is a dot-separated path to extract the value from each item
	// (e.g., "Status.version", "header.revision").
	Path string
	// Transform is an optional function to format the extracted value.
	// If nil, the value is printed with fmt.Sprintf("%v", v).
	Transform func(v interface{}) string
	// Compute is an optional function that receives the full item and all items
	// to produce a value. Used for derived fields (e.g., leader detection).
	// If set, Path is ignored.
	Compute func(item map[string]interface{}, allItems []interface{}) string
	// OmitEmpty hides the column if all values are empty across all items.
	OmitEmpty bool
}

// PrintTable renders a slice of items as a table using the given column definitions.
// Falls back to JSON if data is not a slice or is empty.
func PrintTable(w io.Writer, data interface{}, columns []Column) error {
	items, ok := data.([]interface{})
	if !ok || len(items) == 0 {
		return PrintJSON(w, data)
	}

	// Determine which columns to include (handle OmitEmpty)
	active := make([]bool, len(columns))
	for i, col := range columns {
		if !col.OmitEmpty {
			active[i] = true
			continue
		}
		for _, item := range items {
			if val := resolveColumn(col, AsMap(item), items); val != "" {
				active[i] = true
				break
			}
		}
	}

	// Build headers
	var headers []string
	for i, col := range columns {
		if active[i] {
			headers = append(headers, col.Header)
		}
	}
	t := NewTable(w, headers...)

	// Build rows
	for _, item := range items {
		m := AsMap(item)
		var row []string
		for i, col := range columns {
			if !active[i] {
				continue
			}
			row = append(row, resolveColumn(col, m, items))
		}
		t.AddRow(row...)
	}

	return t.Flush()
}

func resolveColumn(col Column, item map[string]interface{}, allItems []interface{}) string {
	if col.Compute != nil {
		return col.Compute(item, allItems)
	}

	v := resolvePath(item, col.Path)
	if col.Transform != nil {
		return col.Transform(v)
	}
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// resolvePath navigates a dot-separated path through nested maps.
func resolvePath(m map[string]interface{}, path string) interface{} {
	parts := strings.Split(path, ".")
	var current interface{} = m
	for _, part := range parts {
		cm, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		current = cm[part]
	}
	return current
}

// --- Reusable transforms for Column.Transform ---

// TransformShortenEndpoint shortens "https://etcd-0.etcd-discovery...svc:2379" to "etcd-0".
func TransformShortenEndpoint(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	if parts := strings.Split(s, "."); len(parts) > 1 {
		return strings.TrimPrefix(parts[0], "https://")
	}
	return s
}

// TransformShortenURL shortens a URL to "host:port" form.
func TransformShortenURL(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	host, port := s, ""
	if idx := strings.LastIndex(s, ":"); idx != -1 {
		host = s[:idx]
		port = s[idx:]
	}
	if parts := strings.SplitN(host, ".", 2); len(parts) > 1 {
		host = parts[0]
	}
	return host + port
}

// TransformShortenURLList shortens a list of URLs.
func TransformShortenURLList(v interface{}) string {
	arr, ok := v.([]interface{})
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	parts := make([]string, 0, len(arr))
	for _, u := range arr {
		if s, ok := u.(string); ok {
			parts = append(parts, TransformShortenURL(s))
		}
	}
	return strings.Join(parts, ",")
}

// TransformBytes formats a numeric byte count as a human-readable string.
func TransformBytes(v interface{}) string {
	return FormatBytes(v)
}

// TransformBool formats a boolean value, treating nil as "false".
func TransformBool(v interface{}) string {
	if b, ok := v.(bool); ok {
		return fmt.Sprintf("%v", b)
	}
	return "false"
}

// TransformAccessModes abbreviates Kubernetes access modes (e.g., ReadWriteOnce → RWO).
func TransformAccessModes(v interface{}) string {
	return formatAccessModes(v)
}

// TransformAge formats a Kubernetes timestamp as a human-readable duration.
func TransformAge(v interface{}) string {
	if s, ok := v.(string); ok {
		return age(s)
	}
	return ""
}

// TransformUint64 formats a float64 as an integer string without scientific notation.
func TransformUint64(v interface{}) string {
	if v == nil {
		return ""
	}
	if f, ok := v.(float64); ok {
		return fmt.Sprintf("%.0f", f)
	}
	return fmt.Sprintf("%v", v)
}

// TransformShortID truncates identifiers to eight characters for table output.
func TransformShortID(v interface{}) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprintf("%v", v)
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

// SortItems sorts a list of Kubernetes items by namespace then name.
func SortItems(items []interface{}) {
	sort.Slice(items, func(i, j int) bool {
		mi := AsMap(AsMap(items[i])["metadata"])
		mj := AsMap(AsMap(items[j])["metadata"])
		nsI := GetString(mi, "namespace")
		nsJ := GetString(mj, "namespace")
		if nsI != nsJ {
			return nsI < nsJ
		}
		return GetString(mi, "name") < GetString(mj, "name")
	})
}
