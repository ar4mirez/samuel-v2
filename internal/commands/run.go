package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/agents"
	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/methodology/hooks"
	"github.com/samuelpkg/samuel/internal/methodology/ralph"
	"github.com/samuelpkg/samuel/internal/methodology/ralph/prd"
	"github.com/samuelpkg/samuel/internal/ui"
)

// methodologyAliases maps short methodology names to their canonical
// names. Lets `samuel run rw` route to ralph.
var methodologyAliases = map[string]string{
	"rw":    "ralph",
	"ralph": "ralph",
}

// runCmd is the v2 primary verb. `samuel auto` is a permanent alias
// (v1 compat).
var runCmd = &cobra.Command{
	Use:     "run [methodology]",
	Aliases: []string{"auto"},
	Short:   "Autonomous AI coding loop (Ralph methodology)",
	Long: `Run a methodology — the autonomous coding loop. The methodology argument
is positional; when omitted Samuel falls back to ` + "`samuel.toml [default_methodology]`" + `,
then to ` + "`ralph`" + ` (the only built-in).

Bare ` + "`samuel run`" + ` is smart:
  - prd.toon exists  → shows status (read-only, exit 0)
  - prd.toon missing → prints actionable help (exit 1)

Subcommands:
  init       Initialize .samuel/run/
  start      Begin or resume the loop
  pilot      Discover-and-implement zero-setup loop
  status     Show progress and current state
  tasks      List every task
  done       Mark a task completed (CLI mutation)
  skip       Mark a task skipped
  reset      Reset a task to pending
  enqueue    Add a task with auto-id
  task add   Add a task with explicit id (CI/scripts)
  convert    Convert PRD markdown to prd.toon

Examples:
  samuel run init --prd .samuel/tasks/0004-prd-methodology.md
  samuel run start --iterations 20
  samuel run
  samuel run done 1.1 --commit-sha $(git rev-parse HEAD)`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRunBare,
}

var (
	runInitCmd    = &cobra.Command{Use: "init", Short: "Initialize .samuel/run/", RunE: runRunInit}
	runStartCmd   = &cobra.Command{Use: "start", Short: "Begin or resume the autonomous loop", RunE: runRunStart}
	runStatusCmd  = &cobra.Command{Use: "status", Short: "Show loop status", RunE: runRunStatus}
	runPilotCmd   = &cobra.Command{Use: "pilot", Short: "Initialize pilot mode and start", RunE: runRunPilot}
	runConvertCmd = &cobra.Command{Use: "convert <prd-path>", Short: "Convert markdown PRD to prd.toon", Args: cobra.ExactArgs(1), RunE: runRunConvert}
	runTasksCmd   = &cobra.Command{Use: "tasks", Short: "List every task", RunE: runRunTaskList}

	runDoneCmd    = &cobra.Command{Use: "done <task-id>", Short: "Mark a task completed", Args: cobra.ExactArgs(1), RunE: runRunTaskDone}
	runSkipCmd    = &cobra.Command{Use: "skip <task-id>", Short: "Mark a task skipped", Args: cobra.ExactArgs(1), RunE: runRunTaskSkip}
	runResetCmd   = &cobra.Command{Use: "reset <task-id>", Short: "Reset a task to pending", Args: cobra.ExactArgs(1), RunE: runRunTaskReset}
	runEnqueueCmd = &cobra.Command{Use: "enqueue <title>", Short: "Add a task with auto-id", Args: cobra.ExactArgs(1), RunE: runRunTaskEnqueue}

	runTaskCmd    = &cobra.Command{Use: "task", Short: "Explicit-id task subcommands (CI/scripts)"}
	runTaskAddCmd = &cobra.Command{Use: "add <task-id> <title>", Short: "Add a task with explicit id", Args: cobra.ExactArgs(2), RunE: runRunTaskAdd}
)

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.AddCommand(runInitCmd, runStartCmd, runStatusCmd, runPilotCmd, runConvertCmd, runTasksCmd)
	runCmd.AddCommand(runDoneCmd, runSkipCmd, runResetCmd, runEnqueueCmd, runTaskCmd)
	runTaskCmd.AddCommand(runTaskAddCmd)

	// init flags
	runInitCmd.Flags().String("prd", "", "Path to PRD markdown to convert")
	runInitCmd.Flags().String("ai-tool", "claude", "Agent adapter (claude|codex|copilot|gemini|kiro)")
	runInitCmd.Flags().Int("max-iterations", 50, "Maximum loop iterations")
	runInitCmd.Flags().String("sandbox", "none", "Sandbox mode (none|oci)")
	runInitCmd.Flags().String("sandbox-image", "", "Container image when sandbox=oci")
	runInitCmd.Flags().String("methodology", "ralph", "Methodology to run")

	// start flags
	runStartCmd.Flags().Int("iterations", 0, "Override max iterations for this run")
	runStartCmd.Flags().BoolP("yes", "y", false, "Skip confirmation prompt")
	runStartCmd.Flags().Bool("dry-run", false, "Render prompt + invoke adapter in dry-run mode")
	runStartCmd.Flags().Bool("profile", false, "Emit [hooks.timing] entries to progress.md")
	runStartCmd.Flags().Bool("discover-only", false, "Run discovery iterations only (pilot mode)")

	// status flags
	runStatusCmd.Flags().Int("tail", 0, "Show last N entries from progress.md")

	// pilot flags
	runPilotCmd.Flags().String("focus", "", "Focus area (testing|docs|security|performance|refactoring)")
	runPilotCmd.Flags().Int("discover-interval", prd.DefaultDiscoverInterval, "Iterations between discovery passes")
	runPilotCmd.Flags().Int("max-discovery-tasks", prd.DefaultMaxDiscoveryTasks, "Max tasks generated per discovery iteration")

	// tasks flags
	runTasksCmd.Flags().String("status", "", "Filter by status (pending|completed|skipped|blocked|in_progress)")

	// done flags
	runDoneCmd.Flags().String("commit-sha", "", "Commit SHA associated with the completion")
	runDoneCmd.Flags().Int("iteration", 0, "Iteration number that completed this task")
	runSkipCmd.Flags().String("reason", "", "Reason for skipping")

	// enqueue flags
	runEnqueueCmd.Flags().String("priority", prd.PriorityMedium, "Task priority")
	runEnqueueCmd.Flags().String("complexity", prd.ComplexityMedium, "Task complexity")
	runEnqueueCmd.Flags().String("source", prd.SourceManual, "Task source (manual|prd|pilot-discovery)")
}

// runRunBare implements smart bare invocation. The positional
// methodology argument is honoured here too: `samuel run ralph` shows
// status with the ralph methodology resolved.
func runRunBare(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getwd: %w", err)
	}
	methodology := resolveMethodology(args)
	if methodology != "ralph" {
		return fmt.Errorf("samuel run: only the built-in 'ralph' methodology is available in v2.0; got %q", methodology)
	}
	prdPath := prd.PRDPath(cwd)
	if _, err := os.Stat(prdPath); err == nil {
		return runRunStatus(cmd, args)
	}
	fmt.Fprintln(os.Stderr, "samuel: no autonomous loop initialized in this directory.")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "  Initialize one:  samuel run init")
	fmt.Fprintln(os.Stderr, "  From a PRD:      samuel run init --prd .samuel/tasks/0001-prd-feature.md")
	fmt.Fprintln(os.Stderr, "  Zero-setup mode: samuel run pilot")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "See 'samuel run --help' for the full subcommand list.")
	return errors.New("no auto loop initialized")
}

// resolveMethodology picks the methodology in priority order:
//  1. positional arg (with alias substitution)
//  2. samuel.toml default_methodology
//  3. "ralph" fallback
func resolveMethodology(args []string) string {
	if len(args) > 0 {
		if v, ok := methodologyAliases[args[0]]; ok {
			return v
		}
		return args[0]
	}
	cwd, err := os.Getwd()
	if err == nil {
		if cfg, lerr := config.Load(filepath.Join(cwd, config.ProjectFile)); lerr == nil && cfg.DefaultMethodology != "" {
			return cfg.DefaultMethodology
		}
	}
	return "ralph"
}

// --- init ---

func runRunInit(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	aiTool, _ := cmd.Flags().GetString("ai-tool")
	maxIter, _ := cmd.Flags().GetInt("max-iterations")
	sandbox, _ := cmd.Flags().GetString("sandbox")
	sandboxImage, _ := cmd.Flags().GetString("sandbox-image")
	prdPath, _ := cmd.Flags().GetString("prd")
	methodology, _ := cmd.Flags().GetString("methodology")
	if !agents.IsValid(aiTool) {
		return fmt.Errorf("unsupported agent %q (supported: %v)", aiTool, agents.List())
	}
	if methodology == "" {
		methodology = "ralph"
	}
	runDir := prd.RunPath(cwd)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return fmt.Errorf("mkdir .samuel/run: %w", err)
	}
	cfg := prd.AutoConfig{
		MaxIterations: maxIter,
		QualityChecks: detectQualityChecks(cwd),
		AITool:        aiTool,
		PromptFile:    filepath.Join(prd.RunDir, prd.PromptFile),
		Sandbox:       sandbox,
		SandboxImage:  sandboxImage,
		Methodology:   methodology,
	}
	var p *prd.AutoPRD
	if prdPath != "" {
		tasksPath := prd.FindTasksFile(prdPath)
		converted, err := prd.ConvertMarkdownToPRD(prdPath, tasksPath)
		if err != nil {
			return err
		}
		p = converted
		p.Config = cfg
	} else {
		p = prd.NewAutoPRD(filepath.Base(cwd), "Autonomous loop project")
		p.Config = cfg
	}
	if err := p.Save(prd.PRDPath(cwd)); err != nil {
		return err
	}
	progressPath := filepath.Join(runDir, prd.ProgressFile)
	if _, err := os.Stat(progressPath); os.IsNotExist(err) {
		_ = os.WriteFile(progressPath, []byte(""), 0o644)
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"runDir":      runDir,
			"prdPath":     prd.PRDPath(cwd),
			"agent":       aiTool,
			"sandbox":     sandbox,
			"methodology": methodology,
			"tasks":       p.Progress.TotalTasks,
		})
		return nil
	}
	ui.Success("Auto loop initialized at %s/", runDir)
	ui.Print("  PRD:     %s", prd.PRDPath(cwd))
	ui.Print("  Tasks:   %d", p.Progress.TotalTasks)
	ui.Print("  Agent:   %s", aiTool)
	ui.Print("  Sandbox: %s", sandbox)
	if methodology != "" {
		ui.Print("  Methodology: %s", methodology)
	}
	ui.Print("")
	ui.Info("Next: samuel run start")
	return nil
}

func detectQualityChecks(cwd string) []string {
	if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err == nil {
		return []string{"go test ./...", "go vet ./...", "go build ./..."}
	}
	if _, err := os.Stat(filepath.Join(cwd, "package.json")); err == nil {
		return []string{"npm test", "npm run lint", "npm run build"}
	}
	if _, err := os.Stat(filepath.Join(cwd, "Cargo.toml")); err == nil {
		return []string{"cargo test", "cargo clippy", "cargo build"}
	}
	if _, err := os.Stat(filepath.Join(cwd, "requirements.txt")); err == nil {
		return []string{"pytest", "ruff check ."}
	}
	return []string{}
}

// --- start ---

func runRunStart(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	p, _, err := prd.Load(prd.PRDPath(cwd))
	if err != nil {
		return fmt.Errorf("no auto loop found. Run 'samuel run init' first: %w", err)
	}
	if iter, _ := cmd.Flags().GetInt("iterations"); iter > 0 {
		p.Config.MaxIterations = iter
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	profile, _ := cmd.Flags().GetBool("profile")
	discoverOnly, _ := cmd.Flags().GetBool("discover-only")
	if discoverOnly {
		if !p.Config.PilotMode || p.Config.PilotConfig == nil {
			return fmt.Errorf("--discover-only requires pilot mode; run 'samuel run pilot' first")
		}
		p.Config.PilotConfig.DiscoverInterval = 1
	}

	// Empty queue + non-pilot mode means the loop has nothing to do and
	// will exit cleanly with no output (loop.go: queue-empty break path).
	// Catch it here so the user gets actionable feedback instead of a
	// silent exit 0 that's indistinguishable from a crash in CI logs.
	if p.CountPendingTasks() == 0 && !p.Config.PilotMode {
		if JSONMode(cmd) {
			ui.PrintJSON(commandPath(cmd), map[string]any{
				"iterations_run": 0,
				"pending_tasks":  0,
				"message":        "no pending tasks; nothing to do",
			})
			return nil
		}
		ui.Info("No pending tasks. Add one with `samuel run enqueue <title>`, or initialize from a PRD with `samuel run init --prd <path>`.")
		return nil
	}

	yes, _ := cmd.Flags().GetBool("yes")
	if !yes && !dryRun {
		fmt.Fprintf(os.Stdout, "Start autonomous loop on %s using %s for %d iterations? [y/N] ", cwd, p.Config.AITool, p.Config.MaxIterations)
		var ans string
		_, _ = fmt.Fscanln(os.Stdin, &ans)
		if !strings.EqualFold(ans, "y") && !strings.EqualFold(ans, "yes") {
			return errors.New("aborted")
		}
	}
	registry := hooks.NewRegistry()
	ralph.RegisterDefaults(registry)
	cfg := ralph.NewLoopConfig(cwd, p)
	cfg.DryRun = dryRun
	cfg.Profile = profile
	cfg.Hooks = registry
	cfg.OnIterStart = func(iter int, iterType string, task *prd.AutoTask) {
		title := "(no task)"
		if task != nil {
			title = task.Title
		}
		ui.Info("Iter %d [%s] %s", iter, iterType, title)
	}
	cfg.OnIterEnd = func(iter int, err error) {
		if err != nil {
			ui.Warn("Iter %d ended with error: %v", iter, err)
		}
	}
	return ralph.RunAutoLoop(context.Background(), cfg)
}

// --- status ---

func runRunStatus(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	p, warnings, err := prd.Load(prd.PRDPath(cwd))
	if err != nil {
		return fmt.Errorf("no auto loop found. Run 'samuel run init' first")
	}
	p.RecalculateProgress()
	tail, _ := cmd.Flags().GetInt("tail")
	counts := taskStatusCounts(p)
	next := p.GetNextTask()
	var nextSummary *map[string]string
	if next != nil {
		nextSummary = &map[string]string{"id": next.ID, "title": next.Title}
	}
	if JSONMode(cmd) {
		pct := 0
		if p.Progress.TotalTasks > 0 {
			pct = (p.Progress.CompletedTasks * 100) / p.Progress.TotalTasks
		}
		warningStrings := make([]string, 0, len(warnings))
		for _, w := range warnings {
			warningStrings = append(warningStrings, w.Message)
		}
		ui.PrintJSONWithWarnings(commandPath(cmd), map[string]any{
			"project":            p.Project.Name,
			"status":             p.Progress.Status,
			"pilotMode":          p.Config.PilotMode,
			"aiTool":             p.Config.AITool,
			"sandbox":            p.Config.Sandbox,
			"maxIterations":      p.Config.MaxIterations,
			"totalTasks":         p.Progress.TotalTasks,
			"completedTasks":     p.Progress.CompletedTasks,
			"progressPercent":    pct,
			"totalIterationsRun": p.Progress.TotalIterationsRun,
			"taskCounts":         counts,
			"nextTask":           nextSummary,
		}, warningStrings)
		return nil
	}
	ui.Header("Auto Loop Status")
	ui.TableRow("Project", p.Project.Name)
	if p.Config.PilotMode {
		ui.TableRow("Mode", "pilot (autonomous discovery)")
	}
	ui.TableRow("Status", p.Progress.Status)
	pct := 0
	if p.Progress.TotalTasks > 0 {
		pct = (p.Progress.CompletedTasks * 100) / p.Progress.TotalTasks
	}
	ui.TableRow("Progress", fmt.Sprintf("%d/%d tasks (%d%%)", p.Progress.CompletedTasks, p.Progress.TotalTasks, pct))
	ui.TableRow("Agent", p.Config.AITool)
	ui.TableRow("Sandbox", p.Config.Sandbox)
	ui.TableRow("Max Iterations", fmt.Sprintf("%d", p.Config.MaxIterations))
	if p.Progress.TotalIterationsRun > 0 {
		ui.TableRow("Iterations Run", fmt.Sprintf("%d", p.Progress.TotalIterationsRun))
	}
	if p.Progress.LastIterationAt != "" {
		ui.TableRow("Last Iteration", p.Progress.LastIterationAt)
	}
	ui.Print("")
	ui.Print("  Pending: %d  Completed: %d  Blocked: %d  Skipped: %d",
		counts["pending"], counts["completed"], counts["blocked"], counts["skipped"])
	if next != nil {
		ui.Print("")
		ui.Info("Next task: %s %s", next.ID, next.Title)
	}
	if tail > 0 {
		printProgressTail(cwd, tail)
	}
	return nil
}

func taskStatusCounts(p *prd.AutoPRD) map[string]int {
	c := map[string]int{"pending": 0, "in_progress": 0, "completed": 0, "skipped": 0, "blocked": 0}
	for _, t := range p.Tasks {
		c[t.Status]++
	}
	return c
}

func printProgressTail(cwd string, n int) {
	body, err := os.ReadFile(filepath.Join(prd.RunPath(cwd), prd.ProgressFile))
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if n > len(lines) {
		n = len(lines)
	}
	ui.Print("")
	ui.Section(fmt.Sprintf("Last %d progress entries", n))
	for _, l := range lines[len(lines)-n:] {
		ui.Print("  %s", l)
	}
}

// --- tasks list ---

func runRunTaskList(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	p, _, err := prd.Load(prd.PRDPath(cwd))
	if err != nil {
		return fmt.Errorf("no auto loop found. Run 'samuel run init' first")
	}
	filter, _ := cmd.Flags().GetString("status")
	type entry struct {
		ID       string `json:"id"`
		Title    string `json:"title"`
		Status   string `json:"status"`
		Priority string `json:"priority"`
	}
	var rows []entry
	for _, t := range p.Tasks {
		if filter != "" && t.Status != filter {
			continue
		}
		rows = append(rows, entry{ID: t.ID, Title: t.Title, Status: t.Status, Priority: t.Priority})
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{"tasks": rows})
		return nil
	}
	ui.Header("Tasks")
	for _, r := range rows {
		ui.Print("  [%-12s] %s — %s", r.Status, r.ID, r.Title)
	}
	if len(rows) == 0 {
		ui.Dim("No tasks match the filter.")
	}
	return nil
}

// --- convert ---

func runRunConvert(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	prdPath := args[0]
	tasksPath := prd.FindTasksFile(prdPath)
	converted, err := prd.ConvertMarkdownToPRD(prdPath, tasksPath)
	if err != nil {
		return err
	}
	out := prd.PRDPath(cwd)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := converted.Save(out); err != nil {
		return err
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"project": converted.Project.Name,
			"tasks":   converted.Progress.TotalTasks,
			"source":  prdPath,
			"output":  out,
		})
		return nil
	}
	ui.Success("Converted %s → %s (%d tasks)", prdPath, out, converted.Progress.TotalTasks)
	return nil
}

// --- pilot ---

func runRunPilot(cmd *cobra.Command, _ []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	focus, _ := cmd.Flags().GetString("focus")
	discoverInterval, _ := cmd.Flags().GetInt("discover-interval")
	maxDiscovery, _ := cmd.Flags().GetInt("max-discovery-tasks")
	runDir := prd.RunPath(cwd)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return err
	}
	prdPath := prd.PRDPath(cwd)
	if _, err := os.Stat(prdPath); errors.Is(err, os.ErrNotExist) {
		base := prd.AutoConfig{
			MaxIterations: prd.DefaultPilotIterations,
			QualityChecks: detectQualityChecks(cwd),
			AITool:        agents.Default(),
			Sandbox:       "none",
			Methodology:   "ralph",
		}
		pilot := &prd.PilotConfig{
			DiscoverInterval:  discoverInterval,
			MaxDiscoveryTasks: maxDiscovery,
			Focus:             focus,
		}
		p := ralph.InitPilotPRD(cwd, base, pilot)
		if err := p.Save(prdPath); err != nil {
			return err
		}
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{
			"prdPath": prdPath,
			"focus":   focus,
		})
		return nil
	}
	ui.Success("Pilot mode initialized at %s", prdPath)
	ui.Info("Run `samuel run start` to begin")
	return nil
}
