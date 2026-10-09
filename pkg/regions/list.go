package regions

import (
	"fmt"
	"sort"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/discovery"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/spf13/cobra"
)

// listedRegion is one row of the regions listing: a manifest entry plus its
// region name, so json/yaml output is a stable, self-describing list rather than
// a map keyed by region. It embeds RegionInfo so it stays in step with the
// manifest's per-region fields automatically.
type listedRegion struct {
	Region               string `json:"region" yaml:"region"`
	discovery.RegionInfo `yaml:",inline"`
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List regions advertised by an environment's discovery manifest",
		Long: `List the regions published in an environment's endpoint-discovery manifest.

The default text output shows only the region names — the values you pass to
--region. Endpoints (platform API, OIDC issuer) are resolved automatically, so
they are omitted here; use -o json or -o yaml to see the full per-region detail.

The environment selects which manifest to fetch:

  gcphcpctl regions list --env integration

For a shared dev sector or an ephemeral CI run, pass the discovery subdomain as
--env (e.g. --env <infra-id>.dev).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// The persistent --env flag is declared on the root command and already
			// merged with the config file by the root PersistentPreRunE, so reading
			// it here honors flag > env > config precedence.
			environment, _ := cmd.Flags().GetString("environment")
			outputFmt, _ := cmd.Flags().GetString("output")

			if environment == "" {
				return fmt.Errorf("--env is required to locate the discovery manifest")
			}

			resolver := discovery.NewResolver()
			manifest, err := resolver.FetchManifest(cmd.Context(), environment)
			if err != nil {
				return err
			}

			listedRegions := flattenRegions(manifest)
			out := cmd.OutOrStdout()

			switch output.ParseFormat(outputFmt) {
			case output.FormatJSON:
				return output.PrintJSON(out, listedRegions)
			case output.FormatYAML:
				return output.PrintYAML(out, listedRegions)
			default:
			}

			if len(listedRegions) == 0 {
				_, err := fmt.Fprintln(out, "No regions found in the discovery manifest.")
				return err
			}

			table := output.NewTable(out, "REGION")
			for _, region := range listedRegions {
				table.AddRow(region.Region)
			}
			return table.Flush()
		},
	}

	return cmd
}

// flattenRegions returns the manifest's regions as a slice sorted by region
// name for deterministic output.
func flattenRegions(manifest *discovery.Manifest) []listedRegion {
	listedRegions := make([]listedRegion, 0, len(manifest.Regions))
	for regionName, info := range manifest.Regions {
		listedRegions = append(listedRegions, listedRegion{Region: regionName, RegionInfo: info})
	}
	sort.Slice(listedRegions, func(i, j int) bool {
		return listedRegions[i].Region < listedRegions[j].Region
	})
	return listedRegions
}
