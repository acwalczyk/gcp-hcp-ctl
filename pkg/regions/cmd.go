// Package regions implements the "regions" command group, which lists the
// regions advertised by an environment's endpoint-discovery manifest. It is a
// read-only, unauthenticated view of the same manifest the CLI uses for
// endpoint discovery (see pkg/discovery).
package regions

import (
	"github.com/spf13/cobra"
)

// NewRegionsCmd returns the "regions" command group.
func NewRegionsCmd() *cobra.Command {
	regionsCmd := &cobra.Command{
		Use:          "regions",
		Short:        "List regions available for an environment",
		SilenceUsage: true,
	}

	regionsCmd.AddCommand(newListCmd())

	return regionsCmd
}
