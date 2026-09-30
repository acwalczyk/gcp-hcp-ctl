package version

import (
	"fmt"
	"sort"
	"strings"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	utilversion "k8s.io/apimachinery/pkg/util/version"
)

func newListCmd() *cobra.Command {
	var outputFmt string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List supported OpenShift versions",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			versions, err := clientFromCmd(cmd).Versions().List(cmd.Context())
			if err != nil {
				return fmt.Errorf("listing versions: %w", err)
			}

			out := cmd.OutOrStdout()
			switch output.ParseFormat(outputFmt) {
			case output.FormatJSON:
				return output.PrintJSON(out, prepareVersionListForOutput(versions))
			case output.FormatYAML:
				return output.PrintResourceYAML(out, prepareVersionListForOutput(versions))
			}

			if len(versions.Items) == 0 {
				_, err := fmt.Fprintln(out, "No versions found.")
				return err
			}

			defaults := map[string]string{}
			channels, err := clientFromCmd(cmd).Channels().List(cmd.Context())
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Warning: unable to load channel defaults: %v\n", err)
			} else {
				for _, channel := range channels.Items {
					defaults[channel.Name] = channel.Spec.InstallDefaultVersion
				}
			}

			items := append([]gcpv1.Version(nil), versions.Items...)
			sort.SliceStable(items, func(i, j int) bool {
				left, leftErr := utilversion.ParseSemantic(items[i].Name)
				right, rightErr := utilversion.ParseSemantic(items[j].Name)
				if leftErr != nil || rightErr != nil {
					return items[i].Name < items[j].Name
				}
				return left.LessThan(right)
			})

			table := output.NewTable(out, "VERSION", "CHANNEL GROUPS")
			for _, version := range items {
				table.AddRow(
					version.Name,
					formatChannelGroups(version.Spec.ChannelGroups, version.Name, defaults),
				)
			}
			return table.Flush()
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	return cmd
}

func prepareVersionListForOutput(versions *gcpv1.VersionList) *gcpv1.VersionList {
	versions = versions.DeepCopy()
	if versions.APIVersion == "" {
		versions.APIVersion = gcpv1.GroupVersion.String()
	}
	if versions.Kind == "" {
		versions.Kind = "VersionList"
	}
	for i := range versions.Items {
		versions.Items[i].ManagedFields = nil
	}
	return versions
}

func formatChannelGroups(groups []string, version string, defaults map[string]string) string {
	formatted := make([]string, len(groups))
	for i, group := range groups {
		formatted[i] = group
		if defaults[group] == version {
			formatted[i] += " (default)"
		}
	}
	return strings.Join(formatted, ", ")
}
