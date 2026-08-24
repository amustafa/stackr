package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

// The hook must not capture sr's stdout: command substitution turns stdout
// into a pipe, root's TTY check then classifies the run as non-interactive,
// and every prompt silently takes its scripted branch. The cd target instead
// travels through a scratch file named in SR_CD_FILE, leaving stdin/stdout
// attached to the terminal.
const shellHookScript = `
sr() {
  local cd_file exit_code
  cd_file="$(mktemp "${TMPDIR:-/tmp}/sr-cd.XXXXXX")" || {
    command sr "$@"
    return $?
  }

  SR_CD_FILE="$cd_file" command sr "$@"
  exit_code=$?

  if [ -s "$cd_file" ]; then
    local cd_target=""
    IFS= read -r cd_target < "$cd_file"
    if [ -n "$cd_target" ] && [ -d "$cd_target" ]; then
      if cd "$cd_target"; then
        printf 'Switched to worktree at %s\n' "$cd_target"
      else
        exit_code=1
      fi
    fi
  fi
  rm -f "$cd_file"
  return $exit_code
}
`

var shellHookCmd = &cobra.Command{
	Use:   "shell-hook",
	Short: "Print shell integration script",
	Long: `Print a shell function that wraps sr to enable automatic directory
changes when navigating to branches in worktrees.

The function passes sr a scratch file (SR_CD_FILE) to receive the cd target,
so sr's stdin/stdout stay attached to the terminal and interactive prompts
keep working.

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
