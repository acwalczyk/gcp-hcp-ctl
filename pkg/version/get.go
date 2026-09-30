package version

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	var outputFmt string

	cmd := &cobra.Command{
		Use:   "get <version>",
		Short: "Get a supported OpenShift version",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("version is required\n\nUsage: %s", cmd.UseLine())
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			version, err := clientFromCmd(cmd).Versions().Get(cmd.Context(), args[0])
			if err != nil {
				return fmt.Errorf("getting version %q: %w", args[0], err)
			}
			return printVersion(cmd.OutOrStdout(), version, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	return cmd
}

func printVersion(w io.Writer, version *gcpv1.Version, format string) error {
	version = version.DeepCopy()
	version.ManagedFields = nil
	if version.APIVersion == "" {
		version.APIVersion = gcpv1.GroupVersion.String()
	}
	if version.Kind == "" {
		version.Kind = "Version"
	}

	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(w, version)
	case output.FormatYAML:
		return output.PrintResourceYAML(w, version)
	}

	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "Version:        %s\n", version.Name)
	fmt.Fprintf(bw, "Channel Groups: %s\n", strings.Join(version.Spec.ChannelGroups, ", "))
	return bw.Flush()
}
