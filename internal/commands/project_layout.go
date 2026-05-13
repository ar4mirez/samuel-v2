package commands

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/samuelpkg/samuel/internal/builtins"
	"github.com/samuelpkg/samuel/internal/components/samuel"
	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/errors"
	"github.com/samuelpkg/samuel/internal/sync"
)

// projectLayout writes the .samuel/ directory layout into projectDir:
//
//	.samuel/
//	  tasks/      (PRDs go here — populated by the user / agents)
//	  builtins/   (copy of the global ~/.samuel/builtins/ tree)
//	  plugins/    (empty until Milestone 3)
//	  README.md   (describes the layout)
//
// Two PRDs ask for this layout (PRD 0002 §10.0, PRD 0002 §3.7). The
// builtins/ subdirectory is a COPY of the global tree (not a symlink)
// per the open-question resolution in 0002-prd-core.md.
func writeProjectLayout(projectDir, homeDir string) error {
	for _, sub := range []string{"tasks", "builtins", "plugins"} {
		if err := os.MkdirAll(filepath.Join(projectDir, ".samuel", sub), 0o755); err != nil {
			return (&errors.Error{
				Component:   "init",
				Problem:     "cannot create .samuel/" + sub,
				Path:        filepath.Join(projectDir, ".samuel", sub),
				Recoverable: true,
			}).Wrap(err)
		}
	}
	// Mirror the global builtins tree into the project so the project
	// stays self-contained — surviving a user moving the global home.
	if err := mirrorBuiltins(projectDir, homeDir); err != nil {
		return err
	}
	if err := writeLayoutReadme(projectDir); err != nil {
		return err
	}
	return writeLayoutGitignore(projectDir)
}

// mirrorBuiltins copies the global builtins tree into
// <projectDir>/.samuel/builtins/. Falls back to writing the embedded
// FS directly when the global tree is missing (e.g., samuel init
// invoked without first running the SamuelComponent install).
func mirrorBuiltins(projectDir, homeDir string) error {
	target := filepath.Join(projectDir, ".samuel", "builtins")
	// Prefer the global tree (it's the source of truth post-install).
	c := samuel.New(nil, homeDir, "") // Source not needed for path resolution
	globalPath, err := c.GlobalPath()
	if err == nil {
		if info, statErr := os.Stat(globalPath); statErr == nil && info.IsDir() {
			return copyTree(globalPath, target)
		}
	}
	// Fall back to the embedded fs.FS directly.
	return copyFS(builtins.FS(), target)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(src, p)
		if relErr != nil {
			return relErr
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		in, openErr := os.Open(p)
		if openErr != nil {
			return openErr
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		w, createErr := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if createErr != nil {
			return createErr
		}
		defer w.Close()
		_, copyErr := io.Copy(w, in)
		return copyErr
	})
}

func copyFS(src fs.FS, dst string) error {
	return fs.WalkDir(src, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if !filepath.IsLocal(p) {
			return fmt.Errorf("non-local path in builtin source: %s", p)
		}
		out := filepath.Join(dst, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		f, openErr := src.Open(p)
		if openErr != nil {
			return openErr
		}
		defer f.Close()
		w, createErr := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if createErr != nil {
			return createErr
		}
		defer w.Close()
		_, copyErr := io.Copy(w, f)
		return copyErr
	})
}

// writeLayoutGitignore creates .samuel/.gitignore listing the transient
// state samuel writes inside the managed dir during normal operation:
//
//   - lock        runtime PID file (used during concurrent-command guards)
//   - run/        autonomous-loop state (prd.toon, progress.md)
//   - plugins/    installed plugin artifacts (regenerable via `samuel install`)
//   - builtins/   mirror of the global tree (regenerable via `samuel doctor --fix`)
//
// Without this, every user running `samuel init` inside a git repo
// would see these paths untracked on the first `git status` and have
// to figure out what to ignore. The file is idempotent — a user can
// edit it and re-running `samuel init --force` won't clobber the
// edits.
//
// Root-level entries (CLAUDE.md, samuel.lock) are intentionally NOT
// touched here. They depend on the user's preference (commit the
// CLAUDE.md mirror? track samuel.lock for reproducible installs?), and
// the project root is the user's territory. `samuel init` prints a tip
// in its success summary so the user knows what to add if they want
// the defaults.
func writeLayoutGitignore(projectDir string) error {
	path := filepath.Join(projectDir, ".samuel", ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return nil // user-owned; leave it alone
	}
	body := `# Auto-generated by samuel init. Tracks transient state samuel writes
# inside .samuel/ that should not be committed.

lock
run/
plugins/
builtins/
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return (&errors.Error{
			Component:   "init",
			Problem:     "cannot write .samuel/.gitignore",
			Path:        path,
			Recoverable: true,
		}).Wrap(err)
	}
	return nil
}

// writeLayoutReadme creates .samuel/README.md describing the directory
// layout. Idempotent — does not overwrite a user-edited README.
func writeLayoutReadme(projectDir string) error {
	path := filepath.Join(projectDir, ".samuel", "README.md")
	if _, err := os.Stat(path); err == nil {
		return nil // user-owned; leave it alone
	}
	body := `# .samuel/

This directory contains project-level Samuel state. The framework owns
the layout; the contents are yours to read and (mostly) edit.

` + "```" + `
.samuel/
├── tasks/      # PRDs and task lists (you author these)
├── builtins/   # local copy of Samuel's embedded built-ins (do not edit)
└── plugins/    # discovered plugin manifests (populated in Milestone 3)
` + "```" + `

The framework treats ` + "`.samuel/builtins/`" + ` as immutable; if you need to
customize a built-in, fork it as a regular skill under ` + "`.samuel/plugins/`" + `
(see the ` + "`create-skill`" + ` built-in).
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return (&errors.Error{
			Component:   "init",
			Problem:     "cannot write .samuel/README.md",
			Path:        path,
			Recoverable: true,
		}).Wrap(err)
	}
	return nil
}

// renderRootAgentsMD renders the project-level AGENTS.md from cfg. The
// template stays under 150 lines after expansion (CI checks the rendered
// output, not the source). v2 emits AGENTS.md ONLY — tool-specific
// context files are the job of translator plugins, not the core.
func renderRootAgentsMD(projectName string, cfg *config.Config) string {
	if projectName == "" {
		projectName = "project"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", projectName)
	fmt.Fprintf(&b, "%s. Customize with project-specific instructions. -->\n", sync.AutoGenMarker)
	b.WriteString("<!-- AI agents load this file as the project's primary context. -->\n\n")

	b.WriteString("## Methodology\n\n")
	method := "ralph"
	if cfg.DefaultMethodology != "" {
		method = cfg.DefaultMethodology
	}
	fmt.Fprintf(&b, "Default methodology: `%s`\n\n", method)
	if m, ok := cfg.Methodology[method]; ok {
		if m.Agent != "" {
			fmt.Fprintf(&b, "- Agent: `%s`\n", m.Agent)
		}
		if m.MaxIterations > 0 {
			fmt.Fprintf(&b, "- Max iterations: %d\n", m.MaxIterations)
		}
		if len(m.QualityChecks) > 0 {
			b.WriteString("- Quality checks:\n")
			for _, q := range m.QualityChecks {
				fmt.Fprintf(&b, "  - `%s`\n", q)
			}
		}
		b.WriteString("\n")
	}

	if cfg.Guardrails != nil {
		b.WriteString("## Guardrails\n\n")
		fmt.Fprintf(&b, "- Max function lines: %d\n", cfg.Guardrails.MaxFunctionLines)
		fmt.Fprintf(&b, "- Max file lines: %d\n", cfg.Guardrails.MaxFileLines)
		fmt.Fprintf(&b, "- Tests required: %v\n\n", cfg.Guardrails.RequireTests)
	}

	if len(cfg.Plugins) > 0 {
		b.WriteString("## Installed plugins\n\n")
		for _, p := range cfg.Plugins {
			fmt.Fprintf(&b, "- `%s` (%s, v%s)\n", p.Name, p.Kind, p.Version)
		}
		b.WriteString("\n")
	}

	b.WriteString("## Layout\n\n")
	b.WriteString("- `.samuel/tasks/` — PRDs and task lists\n")
	b.WriteString("- `.samuel/builtins/` — framework built-in skills (do not edit)\n")
	b.WriteString("- `.samuel/plugins/` — local plugin manifests\n\n")

	b.WriteString("## Commands\n\n")
	b.WriteString("- `samuel sync` — refresh per-folder AGENTS.md\n")
	b.WriteString("- `samuel doctor` — verify framework + plugin health\n")
	b.WriteString("- `samuel init` — re-run with --force to refresh defaults\n")
	return b.String()
}

// isSamuelRepository reports whether dir is the Samuel source repo
// itself. Detecting this prevents users from accidentally running
// `samuel init` against the framework's own checkout — which would
// stamp `.samuel/` onto the repo and likely surprise the next commit.
//
// Heuristic: look for paths only Samuel's own checkout has.
func isSamuelRepository(dir string) bool {
	// v2 lives at <repo>/go.mod with module = github.com/samuelpkg/samuel,
	// next to internal/builtins/content/ralph/SKILL.md.
	goMod := filepath.Join(dir, "go.mod")
	mod, err := os.ReadFile(goMod)
	if err != nil {
		return false
	}
	if !strings.Contains(string(mod), "module github.com/samuelpkg/samuel") {
		return false
	}
	canary := filepath.Join(dir, "internal", "builtins", "content", "ralph", "SKILL.md")
	if _, err := os.Stat(canary); err != nil {
		return false
	}
	return true
}
