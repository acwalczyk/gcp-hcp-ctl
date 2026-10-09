package cli

import (
	"os"

	"github.com/spf13/cobra"
)

func newCompletionCmd(rootCmd *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for gcphcpctl.

To load completions:

Bash:
  $ source <(gcphcpctl completion bash)

Zsh:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  $ gcphcpctl completion zsh > "${fpath[1]}/_gcphcpctl"

Fish:
  $ gcphcpctl completion fish | source
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return nil
		},
	}
}
