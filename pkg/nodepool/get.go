package nodepool

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	var outputFmt string

	cmd := &cobra.Command{
		Use:   "get <nodepool-name>",
		Short: "Get a nodepool by name",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("nodepool name is required\n\nUsage: %s", cmd.UseLine())
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			np, err := clientFromCmd(cmd).ResolveNodePool(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			return printNodePool(cmd.OutOrStdout(), np, outputFmt)
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	return cmd
}
