package main

import (
	"github.com/spf13/cobra"
)

// Cobra only generates its default `completion` command when the root has
// subcommands, and dtui takes views as positional args, so it is declared here.
var completionCmd = &cobra.Command{
	Use:                   "completion [bash|zsh|fish]",
	Short:                 "Generate a shell completion script",
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		out := cmd.OutOrStdout()
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletionV2(out, true)
		case "zsh":
			return cmd.Root().GenZshCompletion(out)
		default:
			return cmd.Root().GenFishCompletion(out, true)
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
