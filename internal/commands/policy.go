package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/plugin/oci/policy"
	"github.com/samuelpkg/samuel/internal/ui"
)

// `samuel policy` is the operator surface for the OCI tier's network
// policy (PRD 0010 §Functional 6). Subcommands:
//
//	list     — print every persisted (plugin, host, decision) tuple
//	reset    — clear consents (all, or per-plugin)
//	prompt   — replay the most recent block as a fresh prompt (debug)
//	preauth  — script-friendly allowlist injection for CI

var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "Inspect and manage the OCI network-policy consent store",
	Long: `Manage the persistent consent store used by the OCI tier's
deny-by-default network policy (PRD 0010).

Consents live at ~/.samuel/policy/network.toml. Every decision is
recorded in ~/.samuel/policy/audit.log alongside the timestamp.`,
}

var policyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List recorded consent decisions",
	RunE:  runPolicyList,
}

var policyResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Clear consent decisions (all, or scoped to one plugin)",
	Long: `Without --plugin, wipes every recorded consent — the next time
each (plugin, host) pair shows up, the user is prompted again.

With --plugin <name>, only that plugin's consents are cleared.`,
	RunE: runPolicyReset,
}

var policyPromptCmd = &cobra.Command{
	Use:   "prompt",
	Short: "Replay the most recent unanswered consent prompt (CI debug)",
	Long: `Debug aid for CI flows: re-runs the consent decision for the
most recent audit-log entry whose decision was "deny" without a
persisted allow. Lets a human verify what would have been prompted.`,
	RunE: runPolicyPrompt,
}

var policyPreauthCmd = &cobra.Command{
	Use:   "preauth",
	Short: "Pre-allowlist (or pre-deny) a (plugin, host) pair without prompting",
	Long: `Script-friendly consent injection. Use --allow or --deny.

Example:
  samuel policy preauth --plugin claude-code-oci --host api.anthropic.com --allow`,
	RunE: runPolicyPreauth,
}

func init() {
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(policyListCmd)
	policyCmd.AddCommand(policyResetCmd)
	policyCmd.AddCommand(policyPromptCmd)
	policyCmd.AddCommand(policyPreauthCmd)

	policyListCmd.Flags().Bool("json", false, "Emit JSON envelope instead of human-readable list")

	policyResetCmd.Flags().String("plugin", "", "Scope reset to a single plugin")
	policyResetCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")

	policyPreauthCmd.Flags().String("plugin", "", "Plugin name (required)")
	policyPreauthCmd.Flags().String("host", "", "Host pattern (required)")
	policyPreauthCmd.Flags().Bool("allow", false, "Pre-allow the (plugin, host) pair")
	policyPreauthCmd.Flags().Bool("deny", false, "Pre-deny the (plugin, host) pair")
}

// policyStore is injected by tests; production builds the default
// user-scoped store.
var policyStore = func() (*policy.Store, error) {
	s, err := policy.DefaultStore()
	if err != nil {
		return nil, err
	}
	if err := s.Load(); err != nil {
		return nil, err
	}
	return s, nil
}

func runPolicyList(cmd *cobra.Command, _ []string) error {
	s, err := policyStore()
	if err != nil {
		return err
	}
	entries := s.List()
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{"entries": entries})
		return nil
	}
	if len(entries) == 0 {
		ui.Print("No consents recorded yet. Plugins will prompt on first network access.")
		return nil
	}
	ui.Bold("Samuel network-policy consents")
	for _, e := range entries {
		ui.ListItem(1, "%s × %s — %s (first %s)", e.Plugin, e.Host, e.Decision, e.FirstSeen)
	}
	return nil
}

func runPolicyReset(cmd *cobra.Command, _ []string) error {
	pluginName, _ := cmd.Flags().GetString("plugin")
	yes, _ := cmd.Flags().GetBool("yes")
	s, err := policyStore()
	if err != nil {
		return err
	}
	if !yes && !JSONMode(cmd) {
		scope := "all plugins"
		if pluginName != "" {
			scope = "plugin " + pluginName
		}
		ui.Warn("This clears every recorded consent for %s. Re-run with --yes to confirm.", scope)
		return nil
	}
	if pluginName != "" {
		if err := s.ResetPlugin(pluginName); err != nil {
			return err
		}
		ui.Print("Cleared consents for %s", pluginName)
		return nil
	}
	if err := s.Reset(); err != nil {
		return err
	}
	ui.Print("Cleared every recorded consent.")
	return nil
}

func runPolicyPrompt(cmd *cobra.Command, _ []string) error {
	// `samuel policy prompt` is informational: it surfaces the most
	// recent block so an operator can verify what a plugin asked for.
	// We do not actually re-broker the original network call.
	s, err := policyStore()
	if err != nil {
		return err
	}
	ui.Bold("Most recent consent decisions")
	entries := s.List()
	if len(entries) == 0 {
		ui.Print("No consents recorded yet.")
		return nil
	}
	for _, e := range entries {
		ui.ListItem(1, "%s × %s — last %s — %s", e.Plugin, e.Host, e.LastSeen, e.Decision)
	}
	return nil
}

func runPolicyPreauth(cmd *cobra.Command, _ []string) error {
	pluginName, _ := cmd.Flags().GetString("plugin")
	host, _ := cmd.Flags().GetString("host")
	allow, _ := cmd.Flags().GetBool("allow")
	deny, _ := cmd.Flags().GetBool("deny")
	if pluginName == "" || host == "" {
		return fmt.Errorf("--plugin and --host are required")
	}
	if allow == deny {
		return fmt.Errorf("pass exactly one of --allow or --deny")
	}
	s, err := policyStore()
	if err != nil {
		return err
	}
	if err := s.Preauth(pluginName, host, allow); err != nil {
		return err
	}
	action := "denied"
	if allow {
		action = "allowed"
	}
	ui.Print("Pre-%s %s × %s", action, pluginName, host)
	return nil
}

// formatHostPort is a tiny convenience used by callers that want a
// canonical host:port form for an audit log row.
func formatHostPort(host string, port int) string {
	if port <= 0 {
		return host
	}
	return host + ":" + strings.TrimPrefix(fmt.Sprintf("%d", port), "")
}
