package cluster

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type upgradePolicyOptions struct {
	start           string
	durationMinutes int32
	frequency       string
	days            []string
	exclusions      []string
	outputFmt       string
}

// newCreateUpgradePolicyCmd creates a policy after verifying that the cluster exists.
func newCreateUpgradePolicyCmd() *cobra.Command {
	opts := &upgradePolicyOptions{}
	cmd := &cobra.Command{
		Use:   "create-upgrade-policy <cluster-name>",
		Short: "Create a control-plane upgrade policy",
		Args:  clusterNameArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			policy, err := opts.build(args[0])
			if err != nil {
				return err
			}

			client := clientFromCmd(cmd)
			if _, err := client.ResolveCluster(cmd.Context(), args[0]); err != nil {
				return err
			}
			created, err := client.ControlPlaneUpgradePolicies().Create(cmd.Context(), client.Namespace(), policy)
			if err != nil {
				return fmt.Errorf("creating upgrade policy: %w", err)
			}
			return printUpgradePolicy(cmd.OutOrStdout(), created, opts.outputFmt)
		},
	}
	addUpgradePolicyFlags(cmd, opts)
	return cmd
}

// newGetUpgradePolicyCmd retrieves a policy in the requested output format.
func newGetUpgradePolicyCmd() *cobra.Command {
	var outputFmt string
	cmd := &cobra.Command{
		Use:   "get-upgrade-policy <cluster-name>",
		Short: "Get a control-plane upgrade policy",
		Args:  clusterNameArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := clientFromCmd(cmd)
			policy, err := client.ControlPlaneUpgradePolicies().Get(cmd.Context(), client.Namespace(), args[0])
			if err != nil {
				return fmt.Errorf("getting upgrade policy: %w", err)
			}
			return printUpgradePolicy(cmd.OutOrStdout(), policy, outputFmt)
		},
	}
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	return cmd
}

// newUpdateUpgradePolicyCmd updates the window and optionally replaces or clears exclusions.
func newUpdateUpgradePolicyCmd() *cobra.Command {
	opts := &upgradePolicyOptions{}
	var clearExclusions bool
	cmd := &cobra.Command{
		Use:   "update-upgrade-policy <cluster-name>",
		Short: "Update a control-plane upgrade policy",
		Long: "Update the weekly maintenance window. Existing maintenance exclusions are preserved " +
			"unless --exclusion replaces them or --clear-exclusions removes them.",
		Args: clusterNameArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			policy, err := opts.build(args[0])
			if err != nil {
				return err
			}

			// Omitted merge-patch fields preserve their current values. Use an
			// explicit empty array to clear exclusions despite the API's omitempty.
			spec := map[string]any{
				"clusterID":         policy.Spec.ClusterID,
				"maintenanceWindow": policy.Spec.MaintenanceWindow,
			}
			if clearExclusions {
				spec["maintenanceExclusions"] = []gcpv1.ControlPlaneMaintenanceExclusion{}
			} else if cmd.Flags().Changed("exclusion") {
				spec["maintenanceExclusions"] = policy.Spec.MaintenanceExclusions
			}
			patchData, err := json.Marshal(map[string]any{"spec": spec})
			if err != nil {
				return fmt.Errorf("encoding upgrade policy update: %w", err)
			}

			client := clientFromCmd(cmd)
			updated, err := client.ControlPlaneUpgradePolicies().Patch(cmd.Context(), client.Namespace(), args[0], patchData)
			if err != nil {
				return fmt.Errorf("updating upgrade policy: %w", err)
			}
			return printUpgradePolicy(cmd.OutOrStdout(), updated, opts.outputFmt)
		},
	}
	addUpgradePolicyFlags(cmd, opts)
	cmd.Flags().BoolVar(&clearExclusions, "clear-exclusions", false, "Remove all maintenance exclusions")
	cmd.MarkFlagsMutuallyExclusive("exclusion", "clear-exclusions")
	return cmd
}

// newDeleteUpgradePolicyCmd requires explicit confirmation before deleting a policy.
func newDeleteUpgradePolicyCmd() *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{
		Use:   "delete-upgrade-policy <cluster-name>",
		Short: "Delete a control-plane upgrade policy",
		Args:  clusterNameArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("--confirm is required to delete an upgrade policy")
			}

			client := clientFromCmd(cmd)
			if err := client.ControlPlaneUpgradePolicies().Delete(cmd.Context(), client.Namespace(), args[0]); err != nil {
				return fmt.Errorf("deleting upgrade policy: %w", err)
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Upgrade policy for cluster %s deleted.\n", args[0])
			return err
		},
	}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm deletion (required)")
	return cmd
}

// clusterNameArgs requires one cluster name and includes usage when it is missing.
func clusterNameArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("cluster name is required\n\nUsage: %s", cmd.UseLine())
	}
	return cobra.ExactArgs(1)(cmd, args)
}

// addUpgradePolicyFlags registers the shared schedule, exclusion, and output flags.
func addUpgradePolicyFlags(cmd *cobra.Command, opts *upgradePolicyOptions) {
	cmd.Flags().StringVar(&opts.start, "start", "", "First maintenance window start in RFC 3339 UTC format (required)")
	cmd.Flags().Int32Var(&opts.durationMinutes, "duration-minutes", 0, "Maintenance window duration in minutes (required)")
	cmd.Flags().StringVar(&opts.frequency, "frequency", "weekly", "Maintenance window frequency (weekly)")
	cmd.Flags().StringSliceVar(&opts.days, "day", nil, "Maintenance window day as a lowercase full weekday name; repeat for multiple days (required)")
	cmd.Flags().StringArrayVar(&opts.exclusions, "exclusion", nil, "Exclusion as name,start,end; repeat for multiple exclusions")
	cmd.Flags().StringVarP(&opts.outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	for _, name := range []string{"start", "duration-minutes", "day"} {
		if err := cmd.MarkFlagRequired(name); err != nil {
			// All flags are registered above; a missing flag is a programming error.
			panic(fmt.Errorf("marking upgrade policy flag %q required: %w", name, err))
		}
	}
}

// build validates schedule inputs and constructs a policy with UTC timestamps.
func (o *upgradePolicyOptions) build(clusterName string) (*gcpv1.ControlPlaneUpgradePolicy, error) {
	start, err := time.Parse(time.RFC3339, o.start)
	if err != nil {
		return nil, fmt.Errorf("--start must be an RFC 3339 timestamp: %w", err)
	}
	if o.durationMinutes <= 0 {
		return nil, fmt.Errorf("--duration-minutes must be greater than zero")
	}
	if len(o.days) == 0 {
		return nil, fmt.Errorf("at least one --day is required")
	}
	if o.frequency != "weekly" {
		return nil, fmt.Errorf("--frequency must be weekly")
	}
	seenDays := make(map[string]struct{}, len(o.days))
	for _, day := range o.days {
		switch day {
		case "monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday":
		default:
			return nil, fmt.Errorf("--day contains invalid day %q; use lowercase full weekday names", day)
		}
		if _, duplicate := seenDays[day]; duplicate {
			return nil, fmt.Errorf("--day contains duplicate day %q", day)
		}
		seenDays[day] = struct{}{}
	}

	exclusions, err := parseExclusions(o.exclusions)
	if err != nil {
		return nil, err
	}

	return &gcpv1.ControlPlaneUpgradePolicy{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gcpv1.GroupVersion.String(),
			Kind:       "ControlPlaneUpgradePolicy",
		},
		ObjectMeta: metav1.ObjectMeta{Name: clusterName},
		Spec: gcpv1.ControlPlaneUpgradePolicySpec{
			ClusterID: clusterName,
			MaintenanceWindow: &gcpv1.ControlPlaneMaintenanceWindow{
				Start:           metav1.NewTime(start.UTC()),
				DurationMinutes: o.durationMinutes,
				Recurrence: gcpv1.ControlPlaneMaintenanceRecurrence{
					Frequency:  o.frequency,
					DaysOfWeek: o.days,
				},
			},
			MaintenanceExclusions: exclusions,
		},
	}, nil
}

// parseExclusions decodes repeated name,start,end values and normalizes times to UTC.
func parseExclusions(values []string) ([]gcpv1.ControlPlaneMaintenanceExclusion, error) {
	exclusions := make([]gcpv1.ControlPlaneMaintenanceExclusion, 0, len(values))
	for _, value := range values {
		parts := strings.Split(value, ",")
		if len(parts) != 3 {
			return nil, fmt.Errorf("--exclusion must be name,start,end")
		}
		start, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			return nil, fmt.Errorf("parsing exclusion start: %w", err)
		}
		end, err := time.Parse(time.RFC3339, parts[2])
		if err != nil {
			return nil, fmt.Errorf("parsing exclusion end: %w", err)
		}
		exclusions = append(exclusions, gcpv1.ControlPlaneMaintenanceExclusion{
			Name:  parts[0],
			Start: metav1.NewTime(start.UTC()),
			End:   metav1.NewTime(end.UTC()),
		})
	}
	return exclusions, nil
}

// printUpgradePolicy writes text, JSON, or YAML without modifying the supplied policy.
func printUpgradePolicy(w io.Writer, policy *gcpv1.ControlPlaneUpgradePolicy, format string) error {
	policy = policy.DeepCopy()
	if policy.APIVersion == "" {
		policy.APIVersion = gcpv1.GroupVersion.String()
	}
	if policy.Kind == "" {
		policy.Kind = "ControlPlaneUpgradePolicy"
	}

	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(w, policy)
	case output.FormatYAML:
		encoded, err := yaml.Marshal(policy)
		if err != nil {
			return err
		}
		_, err = w.Write(encoded)
		return err
	}

	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "Cluster: %s\n", policy.Spec.ClusterID)
	if window := policy.Spec.MaintenanceWindow; window != nil {
		fmt.Fprintf(bw, "Start: %s\n", window.Start.UTC().Format(time.RFC3339))
		fmt.Fprintf(bw, "Duration: %d minutes\n", window.DurationMinutes)
		fmt.Fprintf(bw, "Frequency: %s\n", window.Recurrence.Frequency)
		fmt.Fprintf(bw, "Days: %s\n", strings.Join(window.Recurrence.DaysOfWeek, ", "))
	}
	if len(policy.Spec.MaintenanceExclusions) > 0 {
		fmt.Fprintln(bw, "Exclusions:")
		for _, exclusion := range policy.Spec.MaintenanceExclusions {
			fmt.Fprintf(bw, "  %s: %s to %s\n", exclusion.Name, exclusion.Start.UTC().Format(time.RFC3339), exclusion.End.UTC().Format(time.RFC3339))
		}
	}
	return bw.Flush()
}
