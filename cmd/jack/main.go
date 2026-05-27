package main

import (
	"fmt"
	"os"

	"github.com/kpetremann/jackadi/cmd/jack/option"
	"github.com/kpetremann/jackadi/cmd/jack/subcommand/job/result"
	"github.com/kpetremann/jackadi/cmd/jack/subcommand/job/task"
	"github.com/kpetremann/jackadi/cmd/jack/subcommand/node"
	_ "github.com/kpetremann/jackadi/internal/plugin/builtin"
	"github.com/spf13/cobra"
)

var version = "dev"
var commit = "N/A"
var date = "N/A"

func sprintVersion() string {
	if version != "dev" {
		version = fmt.Sprintf("v%s", version)
	}
	return fmt.Sprintf("%s (commit: %s, build date: %s)\n", version, commit, date)
}

func main() {
	var completionCmd = &cobra.Command{
		Use:       "completion [bash|zsh|fish]",
		Short:     "Generate shell completion scripts",
		ValidArgs: []string{"bash", "zsh", "fish"},
		Annotations: map[string]string{
			"commandType": "main",
		},
		Args: cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				_ = cmd.Root().GenBashCompletion(cmd.OutOrStdout())
			case "zsh":
				_ = cmd.Root().GenZshCompletion(cmd.OutOrStdout())
			case "fish":
				_ = cmd.Root().GenFishCompletion(cmd.OutOrStdout(), true)
			}
			return nil
		},
	}

	rootCmd := &cobra.Command{
		Use:     "jack",
		Short:   "Jack is the CLI to operate Jackadi.",
		Version: version,
	}
	rootCmd.SetVersionTemplate(sprintVersion())
	rootCmd.AddGroup(
		&cobra.Group{
			ID:    "operations",
			Title: "Operations:",
		},
	)

	rootCmd.AddCommand(completionCmd)
	rootCmd.AddCommand(task.RunCommand())
	rootCmd.AddCommand(node.Root())
	rootCmd.AddCommand(result.ResultsCmd())

	option.JSONFormat = rootCmd.PersistentFlags().Bool("json", false, "display result in JSON")
	option.SortOutput = rootCmd.PersistentFlags().Bool("sort", true, "sort output (default: true)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
