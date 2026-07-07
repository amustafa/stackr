package cmd

import (
	"fmt"
	"os"

	"github.com/amustafa/stackr/internal/context"
	"github.com/amustafa/stackr/pkg/version"
	"github.com/spf13/cobra"
)

var (
	flagCwd         string
	flagDebug       bool
	flagInteractive bool
	flagQuiet       bool
	flagVerify      bool
)

// ctx is populated by PersistentPreRun for every subcommand.
var ctx *context.Context

var rootCmd = &cobra.Command{
	Use:     "sr",
	Short:   "stackr — local stacked-branch workflow for git",
	Version: version.Version,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// init doesn't require a repo context.
		if cmd.Name() == "init" {
			return nil
		}
		// `sr claude install/uninstall --local` targets the current directory's
		// .claude, so it doesn't need a repo either.
		if local, _ := cmd.Flags().GetBool("local"); local && cmd.Parent() != nil && cmd.Parent().Name() == "claude" {
			return nil
		}

		cwd := flagCwd
		if cwd == "" {
			var err error
			cwd, err = os.Getwd()
			if err != nil {
				return fmt.Errorf("could not determine working directory: %w", err)
			}
		}

		var err error
		ctx, err = context.Discover(cwd, flagDebug, flagInteractive)
		if err != nil {
			return err
		}
		ctx.Quiet = flagQuiet
		ctx.Git.Verify = flagVerify
		return nil
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagCwd, "cwd", "", "run as if started in this directory")
	rootCmd.PersistentFlags().BoolVar(&flagDebug, "debug", false, "print git commands as they run")
	rootCmd.PersistentFlags().BoolVar(&flagInteractive, "interactive", true, "enable interactive prompts")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "suppress non-essential output")
	rootCmd.PersistentFlags().BoolVar(&flagVerify, "verify", true, "run git hooks (use --no-verify to skip)")
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
