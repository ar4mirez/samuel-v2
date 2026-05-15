// Package commands wires the Cobra root command and every subcommand
// for the Samuel v2 CLI. Subcommands live in their own files
// (version.go, init.go, etc.). main.go calls Execute().
package commands

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var rootCmd = &cobra.Command{
	Use:   "samuel",
	Short: "Samuel - Rails for AI coding assistants",
	Long: `Samuel is a thin framework + plugin loader for AI coding assistants.
It manages AGENTS.md, syncs per-folder context, runs methodology loops, and
installs plugins (skills, WASM, OCI). Tool-specific behaviour comes from
translator plugins, not the core.

Examples:
  samuel version                  # Show CLI version
  samuel version --json           # Machine-readable envelope`,
	SilenceUsage:  true,
	SilenceErrors: true,
	// SuggestionsMinimumDistance enables Cobra's "did you mean?" hint when a
	// user types an unknown command. Distance 2 is the documented sweet spot —
	// catches typos like 'samuel buld' -> 'samuel build' without firing on
	// genuinely different inputs. Cargo and gh use the same default.
	SuggestionsMinimumDistance: 2,
}

// Execute runs the root command and returns any error from the leaf
// handler. main.go is responsible for mapping that into an exit code.
func Execute() error {
	return rootCmd.Execute()
}

// JSONMode reports whether the --json persistent flag is set on the
// invoked command. Subcommands branch on this to switch from
// human-readable output to the v4 JSON envelope.
func JSONMode(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if f := cmd.Flags().Lookup("json"); f != nil {
		return f.Value.String() == "true"
	}
	if f := cmd.Root().PersistentFlags().Lookup("json"); f != nil {
		return f.Value.String() == "true"
	}
	return false
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().Bool("json", false, "Output JSON envelope for programmatic consumption")
	rootCmd.PersistentFlags().Bool("no-deprecation", false, "Suppress legacy-command deprecation warnings (CI scripts)")
}

// ResetFlagsForTest restores every persistent and subcommand flag to
// its declared default. Cobra reuses the same singleton between
// rootCmd.Execute() calls, so values from a previous invocation leak
// into the next unless we explicitly clear them. Production code never
// runs Execute twice; tests do, all the time.
func ResetFlagsForTest() {
	rootCmd.PersistentFlags().VisitAll(func(f *pflagFlag) {
		_ = f.Value.Set(f.DefValue)
		f.Changed = false
	})
	resetTree(rootCmd)
}

// resetTree resets every flag on cmd and recurses into nested
// subcommands. Required for command groups like `policy <subcommand>`
// where a leaked --allow flag from one test bleeds into the next.
func resetTree(cmd *cobra.Command) {
	for _, sub := range cmd.Commands() {
		sub.Flags().VisitAll(func(f *pflagFlag) {
			_ = f.Value.Set(f.DefValue)
			f.Changed = false
		})
		resetTree(sub)
	}
}

// pflagFlag is the cobra/pflag flag struct alias used by ResetFlagsForTest.
type pflagFlag = pflag.Flag
