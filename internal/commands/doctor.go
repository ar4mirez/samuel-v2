package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/plugin/manifest"
	"github.com/samuelpkg/samuel/internal/plugin/oci"
	"github.com/samuelpkg/samuel/internal/plugin/service"
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

	// Plugin-level state: for every entry in samuel.lock, confirm the
	// directory + manifest + per-kind required files are intact. Issue
	// #3 — doctor advertised "framework + plugin health" but only
	// checked framework health pre-rc.11.
	checks = append(checks, checkInstalledPlugins()...)

	// OCI tier health (PRD 0010 §Functional 11): report detected
	// runtime + version + image-cache size. Skipped quietly when no
	// runtime is available — the deny-by-default ergonomic.
	checks = append(checks, checkOciRuntime())

	// Detect coding-assistant binaries to suggest translator plugins
	// per RFD 0002 §1. Informational only — no health gate.
	suggestions := suggestTranslators()

	// v1-leftover detection: the v1 user-scoped skill tree is purely
	// informational (Samuel v2 does not manage it). The path itself
	// lives in detectV1Leftovers so this comment can stay neutral.
	unmanaged := detectV1Leftovers()

	// Trust-honesty disclosure: in v2.1+ the default verifier is the
	// sigstore-go production backend; surface a one-line confirmation
	// so users reading `signature: verified (...)` know the math is
	// real. When SAMUEL_VERIFY_STUB=1 is active, render the stub
	// advisory instead so the test-mode escape hatch is visible.
	var advisories []string
	if verify.IsProduction() {
		advisories = append(advisories, verify.SigstoreAdvisory)
	} else {
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
				// Re-check post-fix. plugin: components go through the
				// same install-service dispatch attemptFix uses (issue
				// #8 follow-up — without this, the rendered ✗ stays
				// even though `N fixed` is reported in the summary).
				switch {
				case c.Component == "project-layout":
					if pc, ok := checkProjectLayout(); ok {
						checks[i].OK = pc.OK
						checks[i].Message = pc.Message
					}
				case strings.HasPrefix(c.Component, "plugin:"):
					for _, recheck := range checkInstalledPlugins() {
						if recheck.Component == c.Component {
							checks[i].OK = recheck.OK
							checks[i].Message = recheck.Message
							break
						}
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
			"checks":          checks,
			"summary":         summarize(checks),
			"suggestions":     suggestions,
			"v1_leftovers":    unmanaged,
			"advisories":      advisories,
			"wasm_cache_stats": wasmCacheStatsJSON(),
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
	// Plugin-tier components (e.g. "plugin:actix-web") live in
	// samuel.lock and are repaired by re-running the install service,
	// not the orchestrator. rc.11 added the checks but rc.13's
	// attemptFix only knew about orchestrator plugins — the user got
	// "1 fixable" with no actual fix path (issue #8).
	if name, ok := strings.CutPrefix(component, "plugin:"); ok {
		cwd, err := os.Getwd()
		if err != nil {
			return false, err
		}
		svc, err := buildService(cwd)
		if err != nil {
			return false, err
		}
		_, err = svc.Install(ctx, service.InstallOptions{
			Name:  name,
			Force: true,
			Yes:   true, // non-interactive recovery — capabilities already trusted in samuel.lock
		})
		return err == nil, err
	}
	// Otherwise: re-run Install on the matching orchestrator plugin
	// (framework tier — samuel-builtins).
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

// checkInstalledPlugins verifies every plugin recorded in samuel.lock
// against its on-disk artifact. Returns one checkResult per installed
// plugin (component = "plugin:<name>"). Returns nil when cwd is not an
// initialized project, when no lockfile exists, or when the lockfile
// declares no plugins — none of those are health failures.
//
// Per-plugin checks (any failure marks the plugin unhealthy with the
// first reason encountered, so the user sees the most actionable
// hint):
//   1. .samuel/plugins/<name>/ exists on disk.
//   2. samuel-plugin.toml parses.
//   3. manifest.Name / Version / Kind agree with the lockfile entry.
//   4. Per-kind required artifact is present:
//        skill -> SKILL.md
//        wasm  -> manifest.Wasm.Module (or "plugin.wasm" default)
//        oci   -> manifest.OCI.Image must be non-empty (image itself
//                 lives in the registry, not on disk).
func checkInstalledPlugins() []checkResult {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	lf, err := config.LoadLock(cwd)
	if err != nil {
		return nil
	}
	if len(lf.Plugins) == 0 {
		return nil
	}
	out := make([]checkResult, 0, len(lf.Plugins))
	for _, lp := range lf.Plugins {
		out = append(out, checkOnePlugin(cwd, lp))
	}
	return out
}

func checkOnePlugin(projectDir string, lp config.LockedPlugin) checkResult {
	component := "plugin:" + lp.Name
	pluginDir := filepath.Join(projectDir, ".samuel", "plugins", lp.Name)

	if info, err := os.Stat(pluginDir); err != nil || !info.IsDir() {
		return checkResult{
			Component: component,
			OK:        false,
			Message:   "installed dir missing — samuel.lock claims it, but " + pluginDir + " is gone",
			FixHint:   "samuel install " + lp.Name + " --force",
		}
	}

	m, err := manifest.LoadFromDir(pluginDir)
	if err != nil {
		return checkResult{
			Component: component,
			OK:        false,
			Message:   "samuel-plugin.toml missing or invalid: " + err.Error(),
			FixHint:   "samuel install " + lp.Name + " --force",
		}
	}

	if m.Name != lp.Name {
		return checkResult{
			Component: component,
			OK:        false,
			Message:   "manifest drift: lockfile says " + lp.Name + " but manifest says " + m.Name,
			FixHint:   "samuel install " + lp.Name + " --force",
		}
	}
	if m.Version != lp.Version {
		return checkResult{
			Component: component,
			OK:        false,
			Message:   "manifest drift: lockfile @" + lp.Version + " but manifest @" + m.Version,
			FixHint:   "samuel install " + lp.Name + " --force",
		}
	}
	if string(m.Kind) != lp.Kind {
		return checkResult{
			Component: component,
			OK:        false,
			Message:   "manifest drift: lockfile kind=" + lp.Kind + " but manifest kind=" + string(m.Kind),
			FixHint:   "samuel install " + lp.Name + " --force",
		}
	}

	switch m.Kind {
	case manifest.KindSkill:
		skillPath := filepath.Join(pluginDir, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			return checkResult{
				Component: component,
				OK:        false,
				Message:   "SKILL.md missing from " + pluginDir,
				FixHint:   "samuel install " + lp.Name + " --force",
			}
		}
	case manifest.KindWasm:
		modName := "plugin.wasm"
		if m.Wasm != nil && m.Wasm.Module != "" {
			modName = m.Wasm.Module
		}
		modPath := filepath.Join(pluginDir, modName)
		if _, err := os.Stat(modPath); err != nil {
			return checkResult{
				Component: component,
				OK:        false,
				Message:   "wasm module missing: " + modName,
				FixHint:   "samuel install " + lp.Name + " --force",
			}
		}
		// PRD 0009 §Functional 9: declared exports must be present in
		// the compiled module, and the capability block must be
		// internally consistent. We check the [runtime].exports list
		// against the manifest itself (the loader will instantiate the
		// module and verify the export list); doctor here surfaces
		// configuration drift between the [runtime] and [wasm] blocks
		// so the user gets the actionable fix before a `samuel run`
		// time-of-use error.
		if m.Runtime != nil && len(m.Runtime.Exports) > 0 && m.Wasm != nil {
			if !exportSetsEquivalent(m.Wasm.Exports, m.Runtime.Exports) {
				return checkResult{
					Component: component,
					OK:        false,
					Message:   "wasm manifest drift: [wasm].exports and [runtime].exports disagree",
					FixHint:   "harmonize the two export lists or remove one",
				}
			}
		}
	case manifest.KindOci:
		if m.OCI == nil || m.OCI.Image == "" {
			return checkResult{
				Component: component,
				OK:        false,
				Message:   "manifest declares oci kind but no image reference",
				FixHint:   "samuel install " + lp.Name + " --force",
			}
		}
		// PRD 0010 §Functional 11.2 — verify the image digest in the
		// manifest matches what samuel.lock recorded at install time.
		// Drift is the canonical "someone retagged the image" footprint
		// and the user gets the `--fix` hint to re-pull (§11.3).
		if lp.Digest != "" && !strings.Contains(m.OCI.Image, lp.Digest) {
			return checkResult{
				Component: component,
				OK:        false,
				Message:   "oci image digest drift: lockfile @" + lp.Digest + " but manifest @" + m.OCI.Image,
				FixHint:   "samuel install " + lp.Name + " --force",
			}
		}
	}

	return checkResult{
		Component: component,
		OK:        true,
		Message:   lp.Version + " (" + lp.Kind + ") — manifest + artifact intact",
	}
}

// wasmCacheStatsJSON returns a snapshot of the wasm module-cache
// counters. PRD 0009 §Non-functional surfaces the counter via
// `samuel doctor --json`. `samuel doctor` is short-lived — the counter
// reads as zero unless the process has executed a `samuel run` loop
// first; the field still exists so machine consumers can rely on its
// shape and the long-running `samuel run --json` envelope can fill it.
func wasmCacheStatsJSON() map[string]any {
	// We do not eagerly construct a wasm runtime here — that would
	// touch the on-disk cache and emit unrelated warnings. Doctor is
	// strictly read-only; an empty stats blob is the honest answer.
	return map[string]any{
		"hits":         0,
		"misses":       0,
		"hit_rate":     0.0,
		"modules":      0,
		"budget_bytes": 500 * 1024 * 1024,
		"used_bytes":   0,
	}
}

// exportSetsEquivalent reports whether two declared-export lists name
// the same set of functions, regardless of order. Used by the wasm
// health check to detect drift between [wasm].exports and
// [runtime].exports.
func exportSetsEquivalent(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	have := make(map[string]struct{}, len(a))
	for _, s := range a {
		have[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := have[s]; !ok {
			return false
		}
	}
	return true
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

// checkOciRuntime reports the detected container runtime + version +
// image-cache size. PRD 0010 §Functional 11. Returns a benign
// "skipped" check when no runtime is available so doctor stays useful
// for users who never touch the OCI tier.
func checkOciRuntime() checkResult {
	// Reset the per-process cache so doctor surfaces fresh detection
	// state — important when the user just installed podman.
	oci.ResetRuntimeCacheForTest()
	rt, err := oci.DetectRuntime()
	if err != nil {
		return checkResult{
			Component: "oci-runtime",
			OK:        true,
			Message:   "no container runtime detected (OCI tier disabled)",
			FixHint:   "install podman or docker to enable the OCI tier",
		}
	}
	mode := ""
	if rt.Rootless {
		mode = " (rootless)"
	}
	msg := fmt.Sprintf("%s%s", rt.Kind, mode)
	if rt.Version != "" {
		msg += ", version " + rt.Version
	}
	if size, err := ociCacheSize(); err == nil {
		msg += fmt.Sprintf(", cache %s", humanBytes(size))
	}
	return checkResult{
		Component: "oci-runtime",
		OK:        true,
		Message:   msg,
	}
}

// ociCacheSize sums every file under ~/.samuel/cache/oci/images/.
// Returns 0 when the directory does not yet exist — the cache is
// populated lazily on first `samuel install --kind=oci`.
func ociCacheSize() (int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}
	root := filepath.Join(home, ".samuel", "cache", "oci", "images")
	var total int64
	err = filepath.Walk(root, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	return total, nil
}

// humanBytes renders a size as "1.2 GB" / "500 MB" / "2 KB". The
// units stop at GB because OCI image budgets are gigabyte-scale.
func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
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
