package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ar4mirez/samuel/internal/config"
	"github.com/ar4mirez/samuel/internal/errors"
	"github.com/ar4mirez/samuel/internal/plugin"
	"github.com/ar4mirez/samuel/internal/plugin/capability"
	"github.com/ar4mirez/samuel/internal/plugin/oci"
	"github.com/ar4mirez/samuel/internal/plugin/registry"
	"github.com/ar4mirez/samuel/internal/plugin/service"
	"github.com/ar4mirez/samuel/internal/plugin/verify"
	"github.com/ar4mirez/samuel/internal/ui"
)

var (
	installCmd   = &cobra.Command{Use: "install [plugin][@version-range]", Short: "Install a plugin", RunE: runInstall}
	uninstallCmd = &cobra.Command{Use: "uninstall <plugin>", Short: "Uninstall a plugin", RunE: runUninstall, Args: cobra.ExactArgs(1)}
	lsCmd        = &cobra.Command{Use: "ls [plugin]", Short: "List installed plugins", RunE: runLs}
	searchCmd    = &cobra.Command{Use: "search <query>", Short: "Search the plugin registry", RunE: runSearch, Args: cobra.MinimumNArgs(1)}
	infoCmd      = &cobra.Command{Use: "info <plugin>", Short: "Show plugin manifest detail", RunE: runInfo, Args: cobra.ExactArgs(1)}
	updateCmd    = &cobra.Command{Use: "update [plugin]", Short: "Refresh registry / update plugins", RunE: runUpdate}
)

// testRegistrySources lets tests pin a fixture index URL.
var testRegistrySources []registry.Source

// testOciEngine lets tests inject a fake OCI engine without a real
// container runtime.
var testOciEngine oci.Engine

// testPrompt lets tests stub capability grants without TTY.
var testPrompt capability.PromptFn

func init() {
	rootCmd.AddCommand(installCmd, uninstallCmd, lsCmd, searchCmd, infoCmd, updateCmd)

	installCmd.Flags().Bool("yes", false, "Auto-grant requested capabilities")
	installCmd.Flags().Bool("allow-unsigned", false, "Skip signature verification")
	installCmd.Flags().Bool("allow-prerelease", false, "Allow prerelease versions during resolution")
	installCmd.Flags().Bool("force", false, "Force reinstall even when already installed")
	installCmd.Flags().Bool("dry-run", false, "Resolve + verify but do not write")
	installCmd.Flags().Bool("non-interactive", false, "Fail-closed on prompts (CI use)")

	lsCmd.Flags().Bool("all", false, "Include available-but-not-installed plugins from the registry")
	lsCmd.Flags().String("type", "", "Filter by tier: skill|wasm|oci")

	updateCmd.Flags().Bool("all", false, "Update every plugin to its latest compatible version")
}

// buildService wires the install path against the project's config +
// registry sources + verifier.
func buildService(projectDir string) (*service.Service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(home, ".samuel", "cache", "registries")
	cfg, _ := config.Load(projectDir)

	sources := testRegistrySources
	if sources == nil {
		if cfg != nil && len(cfg.Registries) > 0 {
			for _, r := range cfg.Registries {
				sources = append(sources, registry.Source{Name: r.Name, URL: r.URL})
			}
		} else {
			sources = []registry.Source{
				{Name: "official", URL: "github.com/samuelpkg/samuel-registry"},
			}
		}
	}

	verCacheDir := filepath.Join(home, ".samuel", "cache", "verify")
	verifier := verify.NewCache(verCacheDir, BuildVersion(), verify.Default())

	prompt := testPrompt
	if prompt == nil {
		prompt = consolePrompt
	}

	var ociEngine oci.Engine
	if testOciEngine != nil {
		ociEngine = testOciEngine
	} else if rt, err := oci.DetectRuntime(); err == nil {
		ociEngine = oci.NewCLI(rt)
	}

	return &service.Service{
		ProjectDir:  projectDir,
		HomeDir:     home,
		Sources:     sources,
		Registry:    registry.NewClient(cacheDir),
		Verifier:    verifier,
		Policy:      verify.DefaultPolicy(),
		OciEngine:   ociEngine,
		Prompt:      prompt,
	}, nil
}

// consolePrompt is the human-facing capability-grant flow. v2.0 ships
// the inline confirmation; v2.1 swaps in a huh form (which requires a
// non-trivial dep we'd rather not pull in for beta.1).
func consolePrompt(plugin string, reqs []capability.Requested) capability.PromptDecision {
	ui.Warn("Plugin %q requests these capabilities:", plugin)
	for _, r := range reqs {
		marker := "•"
		if r.Risky() {
			marker = "⚠"
		}
		ui.ListItem(1, "%s %s", marker, r.Summary())
	}
	fmt.Print("Grant all? [y/N] ")
	var ans string
	_, _ = fmt.Scanln(&ans)
	ans = strings.TrimSpace(strings.ToLower(ans))
	if ans == "y" || ans == "yes" {
		return capability.PromptDecision{Granted: true, Reason: "user-prompt"}
	}
	return capability.PromptDecision{Granted: false, Reason: "user-declined"}
}

func runInstall(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	svc, err := buildService(cwd)
	if err != nil {
		return renderStructuredError(err)
	}
	if len(args) == 0 {
		return listInstalledForBareInstall(cmd, svc)
	}
	name, constraint := splitNameVersion(args[0])

	yes, _ := cmd.Flags().GetBool("yes")
	allowUnsigned, _ := cmd.Flags().GetBool("allow-unsigned")
	allowPrerelease, _ := cmd.Flags().GetBool("allow-prerelease")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	nonInteractive, _ := cmd.Flags().GetBool("non-interactive")
	verbose, _ := cmd.Flags().GetBool("verbose")

	if err := svc.EnsureProjectInitialized(); err != nil {
		return renderStructuredError(err)
	}

	res, err := svc.Install(cmd.Context(), service.InstallOptions{
		Name:            name,
		Constraint:      constraint,
		AllowPrerelease: allowPrerelease,
		AllowUnsigned:   allowUnsigned,
		Force:           force,
		DryRun:          dryRun,
		Verbose:         verbose,
		Yes:             yes,
		NonInteractive:  nonInteractive,
	})
	if err != nil {
		return renderStructuredError(err)
	}

	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"name":     res.Name,
			"version":  res.Version,
			"kind":     string(res.Kind),
			"source":   res.Source,
			"digest":   res.Digest,
			"grants":   describeGrants(res.Grants),
			"verified": res.Verified.Verified,
			"reason":   res.Verified.Reason,
		})
		return nil
	}
	ui.Success("Installed %s@%s (%s)", res.Name, res.Version, res.Kind)
	if res.Digest != "" {
		ui.ListItem(1, "digest: %s", res.Digest)
	}
	if len(res.Grants) > 0 {
		ui.ListItem(1, "capabilities: %d granted", len(res.Grants))
	}
	ui.ListItem(1, "signature: %s", verify.Describe(res.Verified))
	return nil
}

// listInstalledForBareInstall is the smart-bare-invocation branch:
// `samuel install` with no args lists installed plugins + the
// available-plugin discovery hint.
func listInstalledForBareInstall(cmd *cobra.Command, svc *service.Service) error {
	plugins, err := svc.ListInstalled()
	if err != nil {
		return renderStructuredError(err)
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"installed": plugins,
			"hint":      "samuel search <query>",
		})
		return nil
	}
	ui.Bold("Installed plugins (%d)", len(plugins))
	for _, p := range plugins {
		ui.ListItem(1, "%s@%s (%s)", p.Name, p.Version, p.Kind)
	}
	if len(plugins) == 0 {
		ui.Dim("  none — try `samuel search react` to discover plugins")
	} else {
		ui.Dim("Discover more with `samuel search <query>`.")
	}
	return nil
}

func splitNameVersion(arg string) (string, string) {
	if i := strings.Index(arg, "@"); i > 0 {
		return arg[:i], arg[i+1:]
	}
	return arg, ""
}

func describeGrants(grants []capability.Grant) []map[string]any {
	out := make([]map[string]any, 0, len(grants))
	for _, g := range grants {
		out = append(out, map[string]any{
			"kind":    string(g.Kind),
			"targets": g.Targets,
			"reason":  g.Reason,
		})
	}
	return out
}

func runUninstall(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	svc, err := buildService(cwd)
	if err != nil {
		return renderStructuredError(err)
	}
	if err := svc.EnsureProjectInitialized(); err != nil {
		return renderStructuredError(err)
	}
	name := args[0]
	res, err := svc.Uninstall(cmd.Context(), name, plugin.UninstallOptions{})
	if err != nil {
		return renderStructuredError(err)
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"name":    res.Name,
			"version": res.Version,
			"skipped": res.Skipped,
			"mutations": len(res.Mutations),
		})
		return nil
	}
	if res.Skipped {
		ui.Dim("%s: not installed", name)
		return nil
	}
	ui.Success("Uninstalled %s", res.Name)
	return nil
}

func runLs(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	svc, err := buildService(cwd)
	if err != nil {
		return renderStructuredError(err)
	}
	if err := svc.EnsureProjectInitialized(); err != nil {
		return renderStructuredError(err)
	}
	all, _ := cmd.Flags().GetBool("all")
	kind, _ := cmd.Flags().GetString("type")

	if !all {
		entries, err := svc.ListInstalled()
		if err != nil {
			return renderStructuredError(err)
		}
		filtered := filterByKind(entries, kind)
		if len(args) == 1 {
			return renderLsDetail(cmd, svc, args[0])
		}
		return renderLsList(cmd, filtered, false)
	}
	avail, err := svc.ListAvailable(cmd.Context())
	if err != nil {
		return renderStructuredError(err)
	}
	avail = filterAvailByKind(avail, kind)
	return renderLsAvailable(cmd, avail)
}

func filterByKind(entries []config.PluginEntry, kind string) []config.PluginEntry {
	if kind == "" {
		return entries
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func filterAvailByKind(entries []service.AvailableEntry, kind string) []service.AvailableEntry {
	if kind == "" {
		return entries
	}
	out := entries[:0]
	for _, e := range entries {
		if e.Plugin.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func renderLsList(cmd *cobra.Command, entries []config.PluginEntry, includeAvailable bool) error {
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"installed": entries,
		})
		return nil
	}
	ui.Bold("Installed plugins (%d)", len(entries))
	if len(entries) == 0 {
		ui.Dim("  none")
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, e := range entries {
		ui.ListItem(1, "%-30s %-10s %s", e.Name, e.Version, e.Kind)
	}
	_ = includeAvailable
	return nil
}

func renderLsAvailable(cmd *cobra.Command, entries []service.AvailableEntry) error {
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"available": entries,
		})
		return nil
	}
	ui.Bold("Available plugins (%d)", len(entries))
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	for _, e := range entries {
		status := "available"
		if e.Installed {
			status = "installed@" + e.InstalledVersion
		}
		ui.ListItem(1, "%-30s %-10s %s", e.Name, e.Plugin.Latest, status)
	}
	return nil
}

func renderLsDetail(cmd *cobra.Command, svc *service.Service, name string) error {
	entries, err := svc.ListInstalled()
	if err != nil {
		return renderStructuredError(err)
	}
	for _, e := range entries {
		if e.Name == name {
			if JSONMode(cmd) {
				ui.PrintJSON(commandPath(cmd), e)
				return nil
			}
			ui.Bold("Plugin %s", name)
			ui.TableRow("version", e.Version)
			ui.TableRow("kind", e.Kind)
			return nil
		}
	}
	return renderStructuredError(&errors.Error{
		Component:   "ls",
		Problem:     "plugin not installed",
		Path:        name,
		Recoverable: true,
	})
}

func runSearch(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	svc, err := buildService(cwd)
	if err != nil {
		return renderStructuredError(err)
	}
	query := strings.Join(args, " ")
	var hits []registry.SearchHit
	for _, src := range svc.Sources {
		idx, err := svc.Registry.LoadIndex(cmd.Context(), src, false)
		if err != nil {
			continue
		}
		hits = append(hits, registry.Search(idx, query)...)
	}
	if JSONMode(cmd) {
		out := make([]map[string]any, 0, len(hits))
		for _, h := range hits {
			out = append(out, map[string]any{
				"name":        h.Name,
				"latest":      h.Plugin.Latest,
				"description": h.Plugin.Description,
				"tags":        h.Plugin.Tags,
				"repo":        h.Plugin.Repo,
				"score":       h.Score,
			})
		}
		ui.PrintJSON(commandPath(cmd), map[string]any{"hits": out, "query": query})
		return nil
	}
	if len(hits) == 0 {
		ui.Dim("No results for %q", query)
		return nil
	}
	ui.Bold("Search: %q (%d)", query, len(hits))
	for _, h := range hits {
		ui.ListItem(1, "%s@%s — %s", h.Name, h.Plugin.Latest, h.Plugin.Description)
	}
	return nil
}

func runInfo(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	svc, err := buildService(cwd)
	if err != nil {
		return renderStructuredError(err)
	}
	name := args[0]

	installed, _ := svc.ListInstalled()
	for _, e := range installed {
		if e.Name == name {
			if JSONMode(cmd) {
				ui.PrintJSON(commandPath(cmd), map[string]any{
					"installed": true,
					"name":      e.Name,
					"version":   e.Version,
					"kind":      e.Kind,
				})
				return nil
			}
			ui.Bold("Plugin %s (installed)", name)
			ui.TableRow("version", e.Version)
			ui.TableRow("kind", e.Kind)
			return nil
		}
	}
	_, src, p, err := svc.Registry.FindFirst(cmd.Context(), svc.Sources, name)
	if err != nil {
		return renderStructuredError(err)
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"installed":   false,
			"name":        name,
			"latest":      p.Latest,
			"description": p.Description,
			"tags":        p.Tags,
			"repo":        p.Repo,
			"registry":    src.Name,
			"kind":        p.Kind,
		})
		return nil
	}
	ui.Bold("Plugin %s (registry %s)", name, src.Name)
	ui.TableRow("latest", p.Latest)
	ui.TableRow("repo", p.Repo)
	ui.TableRow("description", p.Description)
	ui.TableRow("tags", strings.Join(p.Tags, ", "))
	return nil
}

func runUpdate(cmd *cobra.Command, args []string) error {
	cwd, _ := os.Getwd()
	svc, err := buildService(cwd)
	if err != nil {
		return renderStructuredError(err)
	}
	if err := svc.EnsureProjectInitialized(); err != nil {
		return renderStructuredError(err)
	}
	all, _ := cmd.Flags().GetBool("all")
	if len(args) == 0 && !all {
		// Refresh registry indexes + report plugins with updates.
		_, err := svc.Registry.Refresh(cmd.Context(), svc.Sources)
		if err != nil {
			return renderStructuredError(err)
		}
		avail, err := svc.ListAvailable(cmd.Context())
		if err != nil {
			return renderStructuredError(err)
		}
		updated := availableWithUpdates(avail)
		if JSONMode(cmd) {
			ui.PrintJSON(commandPath(cmd), map[string]any{"updates": updated})
			return nil
		}
		ui.Bold("Available updates (%d)", len(updated))
		for _, u := range updated {
			ui.ListItem(1, "%s: %s → %s", u.Name, u.InstalledVersion, u.Plugin.Latest)
		}
		return nil
	}
	// With plugin args or --all → reinstall.
	names := args
	if all {
		installed, err := svc.ListInstalled()
		if err != nil {
			return renderStructuredError(err)
		}
		for _, p := range installed {
			names = append(names, p.Name)
		}
	}
	results := []map[string]any{}
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	for _, n := range names {
		res, err := svc.Install(ctx, service.InstallOptions{Name: n, Yes: true, Force: true})
		if err != nil {
			ui.ErrorItem(1, "%s: %v", n, err)
			results = append(results, map[string]any{"name": n, "error": err.Error()})
			continue
		}
		ui.SuccessItem(1, "%s -> %s", res.Name, res.Version)
		results = append(results, map[string]any{"name": res.Name, "version": res.Version})
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{"updated": results})
	}
	return nil
}

func availableWithUpdates(entries []service.AvailableEntry) []service.AvailableEntry {
	out := entries[:0]
	for _, e := range entries {
		if e.HasUpdate() {
			out = append(out, e)
		}
	}
	return out
}
