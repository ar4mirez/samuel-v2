package templates

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

func sampleCtx() PromptContext {
	return PromptContext{
		Samuel:      SamuelInfo{Version: "2.0.0-beta.2"},
		Project:     ProjectInfo{Name: "demo", Description: "demo project"},
		Methodology: MethodologyInfo{Name: "ralph"},
		Iteration:   IterationInfo{Number: 3, TaskID: "1.2", Type: "implementation"},
		Config: prd.AutoConfig{
			AITool:        "claude",
			Sandbox:       "oci",
			MaxIterations: 50,
			QualityChecks: []string{"go test ./..."},
		},
		Guardrails: GuardrailsInfo{MaxFunctionLines: 50, MaxFileLines: 300, RequireTests: true},
		Paths: PathsInfo{
			PRD:              ".samuel/run/prd.toon",
			Progress:         ".samuel/run/progress.md",
			ProgressContext:  ".samuel/run/progress-context.md",
			TaskContext:      ".samuel/run/task-context.toon",
			ProjectSnapshot:  ".samuel/run/project-snapshot.toon",
			AgentsMD:         "AGENTS.md",
		},
		Plugins: []string{"sample-skill"},
	}
}

func TestRender_ImplementationPrompt_MentionsCLIDone(t *testing.T) {
	dir := t.TempDir()
	out, err := Render(dir, sampleCtx())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, "samuel run done 1.2") {
		t.Fatalf("expected CLI-mutation instruction; got %s", out)
	}
	if !strings.Contains(out, "task-context.toon") {
		t.Fatalf("expected task-context.toon reference; got %s", out)
	}
}

func TestRender_DiscoveryPrompt_MentionsCLIEnqueue(t *testing.T) {
	dir := t.TempDir()
	out, err := RenderDiscovery(dir, sampleCtx())
	if err != nil {
		t.Fatalf("RenderDiscovery: %v", err)
	}
	if !strings.Contains(out, "samuel run enqueue") {
		t.Fatalf("expected enqueue CLI; got %s", out)
	}
	if !strings.Contains(out, "project-snapshot.toon") {
		t.Fatalf("expected snapshot reference; got %s", out)
	}
}

func TestRender_PerProjectOverride_ShadowsEmbedded(t *testing.T) {
	dir := t.TempDir()
	overrideDir := filepath.Join(dir, ".samuel", "templates", "ralph")
	if err := os.MkdirAll(overrideDir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := "CUSTOM PROMPT FOR {{ .Project.Name }}\n"
	if err := os.WriteFile(filepath.Join(overrideDir, "prompt.md.tmpl"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Render(dir, sampleCtx())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.HasPrefix(out, "CUSTOM PROMPT FOR demo") {
		t.Fatalf("expected override to win; got %q", out)
	}
}

func TestRender_FocusInjection(t *testing.T) {
	dir := t.TempDir()
	ctx := sampleCtx()
	ctx.Config.PilotConfig = &prd.PilotConfig{
		DiscoverInterval:  5,
		MaxDiscoveryTasks: 10,
		Focus:             "testing",
	}
	out, err := RenderDiscovery(dir, ctx)
	if err != nil {
		t.Fatalf("RenderDiscovery: %v", err)
	}
	if !strings.Contains(out, "test coverage gaps") {
		t.Fatalf("expected focus-area copy; got %s", out)
	}
}
