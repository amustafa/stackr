package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for sr.

To load completions:

Bash:
  $ source <(sr completion bash)
  # Or add to ~/.bashrc:
  $ sr completion bash > /etc/bash_completion.d/sr

Zsh:
  $ source <(sr completion zsh)
  # Or add to fpath:
  $ sr completion zsh > "${fpath[1]}/_sr"

Fish:
  $ sr completion fish | source
  $ sr completion fish > ~/.config/fish/completions/sr.fish
`,
	Args:      cobra.ExactValidArgs(1),
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletion(os.Stdout)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletion(os.Stdout)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
