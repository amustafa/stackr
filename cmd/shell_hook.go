package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

const shellHookScript = `
sr() {
  local output
  output="$(command sr "$@")"
  local exit_code=$?

  local cd_target=""
  local rest=""
  while IFS= read -r line; do
    case "$line" in
      __sr_cd:*)
        cd_target="${line#__sr_cd:}"
        ;;
      *)
        if [ -n "$rest" ]; then
          rest="$rest
$line"
        else
          rest="$line"
        fi
        ;;
    esac
  done <<< "$output"

  if [ -n "$rest" ]; then
    printf '%s\n' "$rest"
  fi

  if [ -n "$cd_target" ]; then
    cd "$cd_target" || return 1
    printf 'Switched to worktree at %s\n' "$cd_target"
  fi

  return $exit_code
}
`

var shellHookCmd = &cobra.Command{
	Use:   "shell-hook",
	Short: "Print shell integration script",
	Long: `Print a shell function that wraps sr to enable automatic directory
changes when navigating to branches in worktrees.

Add this to your shell rc file (.bashrc or .zshrc):

  eval "$(sr shell-hook)"`,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := os.Stdout.WriteString(shellHookScript)
		return err
	},
}

func init() {
	rootCmd.AddCommand(shellHookCmd)
}
