// Package templates embeds and renders the implementation +
// discovery prompts Ralph hands to the agent.
//
// Per RFD 0006 §"Per-project override": when
// .samuel/templates/ralph/<name>.md.tmpl exists, it shadows the
// embedded default. The variables in PromptContext mirror the
// "prompt-template-variables" wiki concept — Samuel, Project,
// Methodology, Iteration, Config, Guardrails, Paths, State, Mode,
// Hooks, Plugins.
package templates

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/ar4mirez/samuel/internal/methodology/ralph/prd"
)

//go:embed prompt.md.tmpl
var defaultPromptTmpl string

//go:embed discovery-prompt.md.tmpl
var defaultDiscoveryTmpl string

// PromptContext is the data handed to text/template at render time.
// Fields cover the 11 RFD 0006 sections.
type PromptContext struct {
	Samuel      SamuelInfo
	Project     ProjectInfo
	Methodology MethodologyInfo
	Iteration   IterationInfo
	Config      prd.AutoConfig
	Guardrails  GuardrailsInfo
	Paths       PathsInfo
	State       StateInfo
	Mode        string // "implementation" or "discovery"
	Hooks       []string
	Plugins     []string
}

type SamuelInfo struct {
	Version string
}

type ProjectInfo struct {
	Name        string
	Description string
}

type MethodologyInfo struct {
	Name string
}

type IterationInfo struct {
	Number int
	TaskID string
	Type   string
}

type GuardrailsInfo struct {
	MaxFunctionLines int
	MaxFileLines     int
	RequireTests     bool
}

type PathsInfo struct {
	PRD              string
	Progress         string
	ProgressContext  string
	TaskContext      string
	ProjectSnapshot  string
	AgentsMD         string
	PromptTemplate   string
}

type StateInfo struct {
	PendingTasks    int
	CompletedTasks  int
	TotalIterations int
}

// Render renders the implementation prompt. Per-project override at
// projectDir/.samuel/templates/ralph/prompt.md.tmpl shadows the
// embedded default.
func Render(projectDir string, ctx PromptContext) (string, error) {
	return renderNamed(projectDir, "prompt.md.tmpl", defaultPromptTmpl, ctx)
}

// RenderDiscovery renders the discovery prompt. Same override path
// rules as Render.
func RenderDiscovery(projectDir string, ctx PromptContext) (string, error) {
	return renderNamed(projectDir, "discovery-prompt.md.tmpl", defaultDiscoveryTmpl, ctx)
}

func renderNamed(projectDir, name, defaultBody string, ctx PromptContext) (string, error) {
	body := defaultBody
	overridePath := filepath.Join(projectDir, ".samuel", "templates", "ralph", name)
	if data, err := os.ReadFile(overridePath); err == nil {
		body = string(data)
	}
	t, err := template.New(name).Funcs(funcMap()).Parse(body)
	if err != nil {
		return "", fmt.Errorf("parse template %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute template %s: %w", name, err)
	}
	return buf.String(), nil
}

func funcMap() template.FuncMap {
	return template.FuncMap{
		"join":              strings.Join,
		"indent":            indent,
		"relpath":           relpath,
		"hasPlugin":         hasPlugin,
		"commitConvention":  commitConvention,
		"focusDescription":  focusDescription,
	}
}

func indent(spaces int, s string) string {
	pad := strings.Repeat(" ", spaces)
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func relpath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}

func hasPlugin(plugins []string, name string) bool {
	for _, p := range plugins {
		if p == name {
			return true
		}
	}
	return false
}

func commitConvention(c prd.AutoConfig) string { return "conventional commits" }

func focusDescription(focus string) string {
	switch strings.ToLower(focus) {
	case "testing":
		return "Focus on test coverage gaps, missing edge case tests, flaky tests, and test infrastructure improvements."
	case "docs", "documentation":
		return "Focus on missing documentation, outdated README, missing godocs, and API documentation."
	case "security":
		return "Focus on input validation, authentication, authorization, dependency vulnerabilities, and OWASP top 10."
	case "performance":
		return "Focus on hot paths, unnecessary allocations, N+1 queries, caching opportunities, and benchmarks."
	case "refactoring":
		return "Focus on code duplication, long functions, high complexity, dead code, and architectural improvements."
	default:
		return "Look for improvements related to: " + focus
	}
}
