package cli

import (
	"fmt"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newConfigCmd(effective *config.Config, configPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show effective configuration",
		Long: `Show the effective CLI configuration after merging config file,
environment variables, and CLI flags.

Config file location: ~/.gcphcpctl/config.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := *configPath
			if cfgPath == "" {
				cfgPath = config.DefaultConfigPath()
			}

			// Print the effective config as YAML, keyed the same way as config.yaml
			// (and the GCPHCPCTL_* vars). Marshaling the struct keeps this in step
			// with Config automatically; omitempty hides unset fields.
			out, err := yaml.Marshal(effective)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if _, err := fmt.Fprintf(w, "config file: %s\n\n", cfgPath); err != nil {
				return err
			}
			_, err = w.Write(out)
			return err
		},
	}
	return cmd
}
