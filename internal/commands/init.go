package commands

import (
	"context"
	stderrors "errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/lock"
	"github.com/samuelpkg/samuel/internal/plugin"
	"github.com/samuelpkg/samuel/internal/sync"
	"github.com/samuelpkg/samuel/internal/translator/claude"
	"github.com/samuelpkg/samuel/internal/ui"
)

var initCmd = &cobra.Command{
	Use:   "init [project-name]",
	Short: "Initialize Samuel in a project",
	Long: `Initialize Samuel in a new or existing project.

This command:
  - Creates .samuel/ (tasks/, builtins/, plugins/)
  - Writes samuel.toml with sensible defaults
  - Installs Samuel's built-in skills under ~/.samuel/builtins/
  - Generates AGENTS.md at the project root and per folder

Examples:
  samuel init my-project          # create directory, init inside it
  samuel init .                   # init the current directory
  samuel init --force             # overwrite existing samuel.toml
  samuel init --minimal           # skip the starter pack hint
  samuel init --json              # machine-readable envelope`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().BoolP("force", "f", false, "Overwrite existing samuel.toml / AGENTS.md")
	initCmd.Flags().Bool("minimal", false, "Skip the starter pack hint at the end")
	initCmd.Flags().Bool("yes", false, "Skip the interactive confirmation prompt")
	initCmd.Flags().Bool("non-interactive", false, "Run without prompts (CI use)")
}

// initFlags captures the parsed init invocation.
type initFlags struct {
	force          bool
	minimal        bool
	yes            bool
	nonInteractive bool
	jsonMode       bool

	// Resolved on parse.
	projectName  string
	absTargetDir string
	createdDir   bool
}

func parseInitFlags(cmd *cobra.Command, args []string) (*initFlags, error) {
	f := &initFlags{}
	f.force, _ = cmd.Flags().GetBool("force")
	f.minimal, _ = cmd.Flags().GetBool("minimal")
	f.yes, _ = cmd.Flags().GetBool("yes")
	f.nonInteractive, _ = cmd.Flags().GetBool("non-interactive")
	f.jsonMode = JSONMode(cmd)

	target := "."
	if len(args) > 0 {
		target = args[0]
	}
	if err := validateProjectName(target); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, (&errors.Error{
			Component:   "init",
			Problem:     "invalid target path",
			Path:        target,
			Recoverable: true,
		}).Wrap(err)
	}
	f.absTargetDir = abs
	if target != "." {
		f.projectName = filepath.Base(abs)
		if _, statErr := os.Stat(abs); os.IsNotExist(statErr) {
			f.createdDir = true
		}
	} else {
		f.projectName = filepath.Base(abs)
	}
	return f, nil
}

// validateProjectName matches Go module name rules: no slashes, no
// spaces, no leading dot. "." is allowed (it means current directory).
func validateProjectName(name string) error {
	if name == "." {
		return nil
	}
	if strings.ContainsAny(name, " \t\n") {
		return &errors.Error{
			Component:   "init",
			Problem:     "project name cannot contain whitespace",
			Path:        name,
			Recoverable: true,
		}
	}
	if strings.ContainsAny(filepath.Base(name), `\:*?"<>|`) {
		return &errors.Error{
			Component:   "init",
			Problem:     "project name contains forbidden characters",
			Fix:         "use alphanumerics, dashes, underscores, dots",
			Path:        name,
			Recoverable: true,
		}
	}
	return nil
}

// validateInitTarget enforces "do not init inside Samuel's own repo"
// and the existing-samuel.toml refusal-unless-force rule.
func validateInitTarget(f *initFlags) error {
	if isSamuelRepository(f.absTargetDir) {
		return &errors.Error{
			Component:   "init",
			Problem:     "cannot initialize inside the Samuel repository itself",
			Fix:         "run `samuel init <project-name>` outside the Samuel checkout",
			Path:        f.absTargetDir,
			Recoverable: true,
		}
	}
	tomlPath := filepath.Join(f.absTargetDir, config.ProjectFile)
	if _, err := os.Stat(tomlPath); err == nil && !f.force {
		// Smart bare invocation: existing project + no --force == status
		// mode. Caller (runInit) handles the status branch.
		return errInitAlreadyDone
	}
	return nil
}

// errInitAlreadyDone signals to runInit that the project is already
// initialized — print status and exit 0 instead of erroring.
var errInitAlreadyDone = stderrors.New("init: project already initialized")

func runInit(cmd *cobra.Command, args []string) error {
	flags, err := parseInitFlags(cmd, args)
	if err != nil {
		return renderStructuredError(err)
	}

	if flags.createdDir {
		if mkErr := os.MkdirAll(flags.absTargetDir, 0o755); mkErr != nil {
			return renderStructuredError((&errors.Error{
				Component:   "init",
				Problem:     "cannot create project directory",
				Path:        flags.absTargetDir,
				Recoverable: true,
			}).Wrap(mkErr))
		}
	}

	if err := validateInitTarget(flags); err != nil {
		if stderrors.Is(err, errInitAlreadyDone) {
			return runInitStatus(cmd, flags)
		}
		return renderStructuredError(err)
	}

	// Pre-existing CLAUDE.md at the project root: the built-in Claude
	// translator skips files without our autogen marker, so anything a
	// user (or v1) wrote by hand is preserved. The warning tells them
	// how to opt into managed mirroring if that's what they want.
	if _, err := os.Stat(filepath.Join(flags.absTargetDir, "CLAUDE.md")); err == nil {
		ui.Warn("Found existing CLAUDE.md; leaving it untouched. Delete it and run `samuel sync` to let Samuel manage CLAUDE.md as an AGENTS.md mirror.")
	}

	if !displayAndConfirm(flags) {
		return nil
	}

	// 1. Run the orchestrator → installs ~/.samuel/builtins/
	o := buildOrchestrator("", BuildVersion())
	ctx := context.Background()
	results, err := o.Install(ctx, installOptionsFromCmd(false, flags.force, false))
	if err != nil {
		return renderStructuredError(err)
	}

	// 1b. Append the install's mutations to samuel.lock so a future
	// `samuel uninstall` (PRD 0006) can replay them in reverse.
	for _, r := range results {
		if recErr := lock.RecordMutations(flags.absTargetDir, r.Component, r.Mutations); recErr != nil {
			ui.Warn("samuel.lock write failed: %v", recErr)
		}
	}

	// 2. Build samuel.toml defaults and save it.
	cfg := config.Defaults()
	if err := config.Save(flags.absTargetDir, cfg); err != nil {
		return renderStructuredError(err)
	}

	// 3. Write .samuel/ project layout (tasks/, builtins/, plugins/, README).
	if err := writeProjectLayout(flags.absTargetDir, ""); err != nil {
		return renderStructuredError(err)
	}

	// 4. Write root AGENTS.md from the rendered template.
	rootAgents := filepath.Join(flags.absTargetDir, "AGENTS.md")
	if _, err := os.Stat(rootAgents); err != nil || flags.force {
		body := renderRootAgentsMD(flags.projectName, cfg)
		if writeErr := os.WriteFile(rootAgents, []byte(body), 0o644); writeErr != nil {
			return renderStructuredError((&errors.Error{
				Component:   "init",
				Problem:     "cannot write project AGENTS.md",
				Path:        rootAgents,
				Recoverable: true,
			}).Wrap(writeErr))
		}
	}

	// 5. Run sync to generate per-folder AGENTS.md.
	syncRes, syncErr := sync.SyncFolderContext(sync.Options{
		RootDir:  flags.absTargetDir,
		MaxDepth: -1,
		Force:    flags.force,
	})
	if syncErr != nil {
		return renderStructuredError((&errors.Error{
			Component:   "init",
			Problem:     "failed to generate per-folder AGENTS.md",
			Recoverable: true,
		}).Wrap(syncErr))
	}

	// 6. Run the built-in Claude translator so CLAUDE.md mirrors land
	// alongside every AGENTS.md the sync step produced. Default-on; the
	// [translators.claude] section in samuel.toml is opt-out only.
	var mirrorRes *claude.Result
	if cfg.ClaudeTranslatorEnabled() {
		mirrorRes, _ = claude.Mirror(claude.Options{
			RootDir:  flags.absTargetDir,
			MaxDepth: -1,
			Force:    flags.force,
		})
	}

	return reportInitSuccess(cmd, flags, results, syncRes, mirrorRes)
}

// displayAndConfirm prints the planned actions and (interactively)
// asks the user to confirm. --yes / --non-interactive / --json skip
// the prompt; in non-tty environments we also skip.
func displayAndConfirm(f *initFlags) bool {
	if f.jsonMode || f.yes || f.nonInteractive {
		return true
	}
	if !isStdinInteractive() {
		return true
	}
	ui.Bold("Samuel will:")
	ui.ListItem(1, "create %s", filepath.Join(f.absTargetDir, ".samuel/"))
	ui.ListItem(1, "write %s", filepath.Join(f.absTargetDir, config.ProjectFile))
	ui.ListItem(1, "write %s", filepath.Join(f.absTargetDir, "AGENTS.md"))
	ui.ListItem(1, "generate per-folder AGENTS.md under %s", f.absTargetDir)
	ui.ListItem(1, "mirror AGENTS.md → CLAUDE.md for Claude Code")
	ui.ListItem(1, "install built-ins under ~/.samuel/builtins/")
	fmt.Print("\nProceed? [Y/n] ")
	var ans string
	_, _ = fmt.Scanln(&ans)
	ans = strings.TrimSpace(strings.ToLower(ans))
	return ans == "" || ans == "y" || ans == "yes"
}

func isStdinInteractive() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

// runInitStatus handles the smart-bare-invocation case: project is
// already initialized, so report status and exit 0.
func runInitStatus(cmd *cobra.Command, f *initFlags) error {
	cfg, err := config.Load(f.absTargetDir)
	if err != nil {
		return renderStructuredError(err)
	}
	if f.jsonMode {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"path":                f.absTargetDir,
			"already_initialized": true,
			"version":             cfg.Version,
			"default_methodology": cfg.DefaultMethodology,
			"plugin_count":        len(cfg.Plugins),
		})
		return nil
	}
	ui.Bold("Samuel already initialized in %s", f.absTargetDir)
	ui.TableRow("samuel.toml version", cfg.Version)
	ui.TableRow("methodology", cfg.DefaultMethodology)
	ui.TableRow("plugins", fmt.Sprintf("%d installed", len(cfg.Plugins)))
	ui.Dim("Run `samuel doctor` to check health, or `samuel init --force` to refresh defaults.")
	return nil
}

// reportInitSuccess prints the post-init summary in either human or
// JSON form.
func reportInitSuccess(cmd *cobra.Command, f *initFlags, results []plugin.InstallResult, syncRes *sync.Result, mirrorRes *claude.Result) error {
	if f.jsonMode {
		components := make([]map[string]any, 0, len(results))
		for _, r := range results {
			components = append(components, map[string]any{
				"name":              r.Component,
				"mutations":         len(r.Mutations),
				"already_installed": r.AlreadyInstalled,
				"skipped":           r.Skipped,
			})
		}
		payload := map[string]any{
			"path":         f.absTargetDir,
			"project_name": f.projectName,
			"created":      append(syncRes.Created, filepath.Join(f.absTargetDir, "AGENTS.md")),
			"updated":      syncRes.Updated,
			"skipped":      syncRes.Skipped,
			"components":   components,
		}
		if mirrorRes != nil {
			payload["claude_mirror"] = map[string]any{
				"created": mirrorRes.Created,
				"updated": mirrorRes.Updated,
				"skipped": mirrorRes.Skipped,
			}
		}
		ui.PrintJSON(commandPath(cmd), payload)
		return nil
	}
	ui.Success("Initialized Samuel in %s", f.absTargetDir)
	renderInstallResults(results)
	ui.SuccessItem(1, "samuel.toml: written")
	ui.SuccessItem(1, ".samuel/ layout: created")
	ui.SuccessItem(1, "AGENTS.md (root + per-folder): %d created, %d updated, %d skipped",
		len(syncRes.Created), len(syncRes.Updated), len(syncRes.Skipped))
	if mirrorRes != nil {
		ui.SuccessItem(1, "CLAUDE.md mirror: %d created, %d updated, %d skipped",
			len(mirrorRes.Created), len(mirrorRes.Updated), len(mirrorRes.Skipped))
	}
	if !f.minimal {
		fmt.Println()
		ui.Bold("Next steps:")
		if f.createdDir {
			ui.ListItem(1, "cd %s", filepath.Base(f.absTargetDir))
		}
		ui.ListItem(1, "samuel doctor          # verify the install")
		ui.ListItem(1, "samuel sync            # regenerate per-folder AGENTS.md after changes")

		fmt.Println()
		ui.Dim("Tip: .samuel/.gitignore was written for you. Add these root-level entries")
		ui.Dim("if you don't want them tracked (both are regenerable):")
		ui.Dim("  CLAUDE.md      # translator mirror, regenerated by `samuel sync`")
		ui.Dim("  samuel.lock    # install state, commit if you want reproducible deploys")
	}
	return nil
}

// BuildVersion returns the SamuelComponent install version. Wraps the
// build-time Version so tests can stub the value via SetBuildVersion.
func BuildVersion() string {
	if buildVersionOverride != "" {
		return buildVersionOverride
	}
	return Version
}

var buildVersionOverride string

// SetBuildVersion overrides BuildVersion for tests. The empty string
// clears the override (production behavior).
func SetBuildVersion(v string) { buildVersionOverride = v }
