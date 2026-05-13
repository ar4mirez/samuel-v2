package commands

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/plugin/verify"
	"github.com/samuelpkg/samuel/internal/ui"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check framework + plugin health",
	Long: `Walk every installed plugin's Check() and render a unified health
report. Read-only by default; use --fix to attempt automatic repair.

Examples:
  samuel doctor              # report health
  samuel doctor --fix        # repair detected issues
  samuel doctor --json       # machine-readable envelope`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
	doctorCmd.Flags().Bool("fix", false, "Attempt to repair issues automatically")
}

// checkResult is the rendered form of a HealthStatus, plus repair
// metadata when --fix is in effect.
type checkResult struct {
	Component string `json:"component"`
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	FixHint   string `json:"fix_hint,omitempty"`
	Fixed     bool   `json:"fixed,omitempty"`
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	fix, _ := cmd.Flags().GetBool("fix")

	o := buildOrchestrator("", BuildVersion())
	ctx := context.Background()
	statuses := o.Doctor(ctx)
	checks := make([]checkResult, 0, len(statuses)+1)
	for _, s := range statuses {
		checks = append(checks, checkResult{
			Component: s.Component,
			OK:        s.OK,
			Message:   s.Message,
			FixHint:   s.FixHint,
		})
	}
	// Project-level state: when cwd is an initialized project, verify
	// .samuel/builtins/ exists and is non-empty. PRD 0002 §5 lists this
	// as a doctor concern distinct from the global tree.
	if pc, ok := checkProjectLayout(); ok {
		checks = append(checks, pc)
	}

	// Detect coding-assistant binaries to suggest translator plugins
	// per RFD 0002 §1. Informational only — no health gate.
	suggestions := suggestTranslators()

	// v1-leftover detection: the v1 user-scoped skill tree is purely
	// informational (Samuel v2 does not manage it). The path itself
	// lives in detectV1Leftovers so this comment can stay neutral.
	unmanaged := detectV1Leftovers()

	// Trust-honesty disclosure: the v2.0 default verifier is a stub
	// that enforces policy but not cryptographic signatures. Surface
	// that so users reading `signature: verified (...)` understand
	// what the line currently means. Empty slice when v2.1+ ships a
	// production-grade verifier (verify.IsProduction returns true).
	var advisories []string
	if !verify.IsProduction() {
		advisories = append(advisories, verify.StubAdvisory)
	}

	if fix {
		for i, c := range checks {
			if c.OK {
				continue
			}
			fixed, fixErr := attemptFix(ctx, o, c.Component)
			if fixErr != nil {
				ui.Warn("could not fix %s: %v", c.Component, fixErr)
				continue
			}
			checks[i].Fixed = fixed
			if fixed {
				// Re-check post-fix.
				switch c.Component {
				case "project-layout":
					if pc, ok := checkProjectLayout(); ok {
						checks[i].OK = pc.OK
						checks[i].Message = pc.Message
					}
				default:
					for _, s := range o.Doctor(ctx) {
						if s.Component == c.Component {
							checks[i].OK = s.OK
							checks[i].Message = s.Message
							break
						}
					}
				}
			}
		}
	}

	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"checks":       checks,
			"summary":      summarize(checks),
			"suggestions":  suggestions,
			"v1_leftovers": unmanaged,
			"advisories":   advisories,
		})
		return nil
	}

	renderDoctorHuman(checks, suggestions, unmanaged, advisories)
	return nil
}

func attemptFix(ctx context.Context, o orchestratorIface, component string) (bool, error) {
	// Project-layout is owned by the init command, not a plugin.
	if component == "project-layout" {
		cwd, err := os.Getwd()
		if err != nil {
			return false, err
		}
		// Re-running Install on the SamuelComponent guarantees the
		// global tree exists before we mirror it into the project.
		for _, p := range o.Plugins() {
			if p.Name() != "samuel-builtins" {
				continue
			}
			if _, ierr := p.Install(ctx, plugin.InstallOptions{Force: true, Stdout: os.Stdout}); ierr != nil {
				return false, ierr
			}
		}
		if err := writeProjectLayout(cwd, ""); err != nil {
			return false, err
		}
		return true, nil
	}
	// Otherwise: re-run Install on the matching plugin.
	for _, p := range o.Plugins() {
		if p.Name() != component {
			continue
		}
		_, err := p.Install(ctx, plugin.InstallOptions{Force: true, Stdout: os.Stdout})
		if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, &errors.Error{
		Component:   "doctor",
		Problem:     "no plugin matches " + component,
		Recoverable: false,
	}
}

// checkProjectLayout reports the health of the project's .samuel/
// directory. Returns (result, true) when cwd is an initialized project
// (has a samuel.toml); (_, false) otherwise so doctor can skip rendering
// when invoked outside a project.
func checkProjectLayout() (checkResult, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return checkResult{}, false
	}
	if _, err := os.Stat(filepath.Join(cwd, "samuel.toml")); err != nil {
		return checkResult{}, false
	}
	builtins := filepath.Join(cwd, ".samuel", "builtins")
	info, statErr := os.Stat(builtins)
	if statErr != nil || !info.IsDir() {
		return checkResult{
			Component: "project-layout",
			OK:        false,
			Message:   ".samuel/builtins/ missing from project",
			FixHint:   "samuel doctor --fix",
		}, true
	}
	entries, _ := os.ReadDir(builtins)
	if len(entries) == 0 {
		return checkResult{
			Component: "project-layout",
			OK:        false,
			Message:   ".samuel/builtins/ exists but is empty",
			FixHint:   "samuel doctor --fix",
		}, true
	}
	return checkResult{
		Component: "project-layout",
		OK:        true,
		Message:   ".samuel/ layout intact",
	}, true
}

// orchestratorIface is the minimal surface doctor needs from the
// orchestrator. Easier to fake in tests than the concrete struct.
type orchestratorIface interface {
	Plugins() []plugin.Plugin
	Doctor(ctx context.Context) []plugin.HealthStatus
}

func summarize(checks []checkResult) map[string]int {
	out := map[string]int{"passed": 0, "failed": 0, "fixable": 0, "fixed": 0}
	for _, c := range checks {
		if c.OK {
			out["passed"]++
		} else {
			out["failed"]++
			if c.FixHint != "" {
				out["fixable"]++
			}
		}
		if c.Fixed {
			out["fixed"]++
		}
	}
	return out
}

func renderDoctorHuman(checks []checkResult, suggestions []string, unmanaged []string, advisories []string) {
	ui.Bold("Samuel doctor")
	for _, c := range checks {
		if c.OK {
			ui.SuccessItem(1, "%s — %s", c.Component, c.Message)
		} else {
			ui.ErrorItem(1, "%s — %s", c.Component, c.Message)
			if c.FixHint != "" {
				ui.ListItem(2, "fix: %s", c.FixHint)
			}
		}
		if c.Fixed {
			ui.SuccessItem(2, "(repaired this run)")
		}
	}
	s := summarize(checks)
	ui.Print("")
	ui.Bold("Summary: %d passed, %d failed, %d fixable, %d fixed", s["passed"], s["failed"], s["fixable"], s["fixed"])
	if len(advisories) > 0 {
		ui.Print("")
		ui.Section("Advisories")
		for _, a := range advisories {
			ui.Warn("%s", a)
		}
	}
	if len(suggestions) > 0 {
		ui.Print("")
		ui.Section("Suggested translator plugins")
		for _, s := range suggestions {
			ui.ListItem(1, "%s", s)
		}
	}
	if len(unmanaged) > 0 {
		ui.Print("")
		ui.Section("Unmanaged v1 content")
		for _, u := range unmanaged {
			ui.ListItem(1, "%s", u)
		}
	}
}

// suggestTranslators looks for known coding-assistant binaries on PATH
// and suggests installing the matching translator plugin per RFD 0002 §1.
//
// Claude is intentionally absent: the built-in Claude translator under
// internal/translator/claude/ handles AGENTS.md → CLAUDE.md mirroring,
// so no plugin is needed.
//
// Tools that read AGENTS.md natively are also absent — suggesting a
// no-op translator install would be misleading.
//
// Tools with a richer-than-mirror surface (per-folder rule files, glob
// matchers, agent prompts) stay in plugins because the mapping is
// non-trivial.
func suggestTranslators() []string {
	candidates := map[string]string{
		"cursor": "cursor-translator (Cursor)", // agnostic-allow: PRD 0002 §7.7
	}
	var out []string
	for bin, label := range candidates {
		if _, err := exec.LookPath(bin); err == nil {
			out = append(out, label)
		}
	}
	return out
}

// detectV1Leftovers reports whether the v1 user-scoped skills tree
// exists from a prior install. v2 does not manage it; the hint is
// informational so users know to remove it manually if desired (PRD
// 0002 §7.6 migration helper).
func detectV1Leftovers() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	p := filepath.Join(home, ".claude", "skills") // agnostic-allow: PRD 0002 §7.6 v1 leftover detection
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		entries, _ := os.ReadDir(p)
		if len(entries) > 0 {
			return []string{p + " (v1 skill tree — Samuel v2 does not manage it)"}
		}
	}
	return nil
}
