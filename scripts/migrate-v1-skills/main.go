// Command migrate-v1-skills mechanically ports the 79 v1 SKILL.md
// directories at samuel_v1/.claude/skills/ into per-plugin source trees
// rooted at migration-output/samuel-<name>/. Each tree contains the
// samuel-plugin.toml manifest, the original SKILL.md unchanged, any
// scripts/references/assets the v1 skill ships, a generated README,
// MIT LICENSE, and a release.yml stub that references the reusable
// workflow at github.com/samuelpkg/samuel-plugin-release.
//
// PRD 0005 §Functional 1 is the contract for this tool. The script is
// one-shot — once the registry is populated and the per-plugin repos
// are pushed, this directory is deleted.
//
// Usage:
//
//	go run ./scripts/migrate-v1-skills \
//	    -src ../samuel_v1/.claude/skills \
//	    -out ./migration-output \
//	    [-dry-run]
//
// The tool is idempotent: re-running overwrites generated files but
// leaves the source SKILL.md content untouched.
package main

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dropList is the set of v1 skills explicitly excluded from migration
// per PRD 0005 task 4.1 — both relied on v1's directory layout and have
// no v2 analogue (init is built-in, framework upgrades are out-of-tree).
var dropList = map[string]bool{
	"initialize-project": true,
	"update-framework":   true,
}

// upstreamSource marks a v1 skill as a verbatim copy of an Anthropic
// community plugin. These are not ported to their own samuel-<name>
// repo — they are registered in samuel-registry as subpath = "<name>"
// pointing at github.com/anthropics/skills with upstream = true.
const upstreamSource = "github.com/anthropics/skills"

// frontmatter is the subset of v1 SKILL.md YAML we care about.
type frontmatter struct {
	Name        string
	Description string
	License     string
	Author      string
	Version     string
	Category    string
	Language    string
	Extensions  string
	Source      string
}

// registryEntry mirrors one [[plugins]] entry in samuel-registry/index.toml.
type registryEntry struct {
	Name        string
	Repo        string
	Subpath     string
	Latest      string
	Description string
	Categories  []string
	Tags        []string
	Upstream    bool
}

func main() {
	var (
		src    = flag.String("src", "../samuel_v1/.claude/skills", "v1 skills root")
		out    = flag.String("out", "./migration-output", "output directory for per-plugin trees")
		dryRun = flag.Bool("dry-run", false, "print planned operations without writing")
		owner  = flag.String("owner", "samuelpkg", "GitHub owner for samuel-<name> repos")
	)
	flag.Parse()

	entries, err := os.ReadDir(*src)
	if err != nil {
		fail("read source: %v", err)
	}

	var (
		ported   []registryEntry
		upstream []registryEntry
		skipped  []string
	)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		skillDir := filepath.Join(*src, name)
		skillFile := filepath.Join(skillDir, "SKILL.md")
		if _, statErr := os.Stat(skillFile); statErr != nil {
			continue
		}

		if dropList[name] {
			skipped = append(skipped, name+" (dropped per PRD 0005)")
			continue
		}

		fm, err := readFrontmatter(skillFile)
		if err != nil {
			fail("parse %s: %v", name, err)
		}
		if fm.Name == "" {
			fm.Name = name
		}

		// Anthropic upstream entries are referenced by subpath, not migrated.
		if fm.Source == upstreamSource {
			upstream = append(upstream, registryEntry{
				Name:        fm.Name,
				Repo:        upstreamSource,
				Subpath:     fm.Name,
				Latest:      "main",
				Description: firstLine(fm.Description),
				Categories:  categoriesFor(fm),
				Tags:        tagsFor(fm),
				Upstream:    true,
			})
			skipped = append(skipped, name+" (anthropic upstream subpath)")
			continue
		}

		repoName := "samuel-" + fm.Name
		repoDir := filepath.Join(*out, repoName)

		if *dryRun {
			fmt.Printf("plan: %s -> %s\n", name, repoDir)
		} else {
			if err := materialize(skillDir, repoDir, fm, *owner); err != nil {
				fail("materialize %s: %v", name, err)
			}
		}

		ported = append(ported, registryEntry{
			Name:        fm.Name,
			Repo:        fmt.Sprintf("github.com/%s/%s", *owner, repoName),
			Latest:      "1.0.0",
			Description: firstLine(fm.Description),
			Categories:  categoriesFor(fm),
			Tags:        tagsFor(fm),
		})
	}

	sort.Slice(ported, func(i, j int) bool { return ported[i].Name < ported[j].Name })
	sort.Slice(upstream, func(i, j int) bool { return upstream[i].Name < upstream[j].Name })

	if !*dryRun {
		regPath := filepath.Join(*out, "samuel-registry", "index.toml")
		if err := writeRegistry(regPath, append(append([]registryEntry{}, ported...), upstream...)); err != nil {
			fail("write registry: %v", err)
		}
	}

	fmt.Println()
	fmt.Printf("ported:   %d (will be pushed as github.com/%s/samuel-<name>)\n", len(ported), *owner)
	fmt.Printf("upstream: %d (anthropics/skills subpath entries)\n", len(upstream))
	fmt.Printf("skipped:  %d\n", len(skipped))
	for _, s := range skipped {
		fmt.Printf("  - %s\n", s)
	}
}

func materialize(srcSkillDir, dstRepoDir string, fm frontmatter, owner string) error {
	if err := os.MkdirAll(dstRepoDir, 0o755); err != nil {
		return err
	}

	if err := copyFile(filepath.Join(srcSkillDir, "SKILL.md"), filepath.Join(dstRepoDir, "SKILL.md")); err != nil {
		return fmt.Errorf("copy SKILL.md: %w", err)
	}

	for _, sub := range []string{"scripts", "references", "assets"} {
		s := filepath.Join(srcSkillDir, sub)
		if _, err := os.Stat(s); err != nil {
			continue
		}
		if err := copyDir(s, filepath.Join(dstRepoDir, sub)); err != nil {
			return fmt.Errorf("copy %s: %w", sub, err)
		}
	}

	if err := os.WriteFile(filepath.Join(dstRepoDir, "samuel-plugin.toml"), []byte(renderManifest(fm)), 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dstRepoDir, "README.md"), []byte(renderREADME(fm, owner)), 0o644); err != nil {
		return fmt.Errorf("write README: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dstRepoDir, "LICENSE"), []byte(licenseMIT(licenseHolderFor(fm))), 0o644); err != nil {
		return fmt.Errorf("write LICENSE: %w", err)
	}
	wfDir := filepath.Join(dstRepoDir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(wfDir, "release.yml"), []byte(releaseWorkflow), 0o644); err != nil {
		return fmt.Errorf("write workflow: %w", err)
	}
	return nil
}

// renderManifest builds the samuel-plugin.toml content. Field order
// follows the schema documented in [internal/plugin/manifest]. Quoted
// strings are escaped for TOML.
func renderManifest(fm frontmatter) string {
	var b strings.Builder
	b.WriteString("# Generated by scripts/migrate-v1-skills. Hand-edit after migration.\n")
	fmt.Fprintf(&b, "name = %q\n", fm.Name)
	b.WriteString("version = \"1.0.0\"\n")
	b.WriteString("kind = \"skill\"\n")
	if s := firstLine(fm.Description); s != "" {
		fmt.Fprintf(&b, "summary = %q\n", s)
	}
	if fm.License != "" {
		fmt.Fprintf(&b, "license = %q\n", fm.License)
	} else {
		b.WriteString("license = \"MIT\"\n")
	}
	if fm.Author != "" {
		fmt.Fprintf(&b, "authors = [%q]\n", fm.Author)
	}
	b.WriteString("\n[samuel]\n")
	b.WriteString("framework = \"^2.0.0\"\n")
	b.WriteString("protocol  = \"^1.0.0\"\n")

	b.WriteString("\n[provides]\n")
	fmt.Fprintf(&b, "skills = [%q]\n", fm.Name)

	b.WriteString("\n[capabilities.filesystem]\n")
	b.WriteString("read  = [\"/workspace\"]\n")
	b.WriteString("write = []\n")

	b.WriteString("\n[metadata]\n")
	if fm.Category != "" {
		fmt.Fprintf(&b, "category = %q\n", fm.Category)
	}
	if fm.Language != "" {
		fmt.Fprintf(&b, "language = %q\n", fm.Language)
	}
	if fm.Extensions != "" {
		fmt.Fprintf(&b, "extensions = %q\n", fm.Extensions)
	}
	if fm.Source != "" {
		fmt.Fprintf(&b, "source = %q\n", fm.Source)
	}
	return b.String()
}

// renderREADME builds the short README that ships in each generated
// repo. The body is intentionally minimal — discovery happens via the
// registry and `samuel info`.
func renderREADME(fm frontmatter, owner string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# samuel-%s\n\n", fm.Name)
	if s := firstLine(fm.Description); s != "" {
		fmt.Fprintf(&b, "> %s\n\n", s)
	}
	b.WriteString("## Install\n\n")
	b.WriteString("```sh\n")
	fmt.Fprintf(&b, "samuel install %s\n", fm.Name)
	b.WriteString("```\n\n")
	b.WriteString("## Source\n\n")
	fmt.Fprintf(&b, "- Repository: https://github.com/%s/samuel-%s\n", owner, fm.Name)
	b.WriteString("- License: " + nonEmpty(fm.License, "MIT") + "\n")
	if fm.Source != "" {
		fmt.Fprintf(&b, "- Upstream: %s\n", fm.Source)
	}
	b.WriteString("\nThis plugin is part of the [Samuel v2 plugin ecosystem](https://github.com/" + owner + "/samuel).\n")
	return b.String()
}

// releaseWorkflow is the per-plugin release.yml stub. It hands off to
// the reusable workflow that lives in samuel-plugin-release.
const releaseWorkflow = `# Generated by scripts/migrate-v1-skills. Plugin authors should keep this
# file in sync with samuel-plugin-release's latest stable tag.
name: release

on:
  push:
    tags:
      - "v*"

permissions:
  contents: write
  packages: write
  id-token: write   # cosign keyless signing

jobs:
  release:
    uses: samuelpkg/samuel-plugin-release/.github/workflows/release.yml@v1
    with:
      manifest: samuel-plugin.toml
    secrets: inherit
`

// licenseHolderFor returns the entity named in the LICENSE file. For
// Anthropic-sourced skills we preserve their attribution; for everything
// else we default to the Samuel maintainers.
func licenseHolderFor(fm frontmatter) string {
	if fm.Author == "anthropic" || fm.Source == upstreamSource {
		return "Anthropic, PBC and the Samuel maintainers"
	}
	return "the Samuel maintainers"
}

const mitTemplate = `MIT License

Copyright (c) 2026 %s

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
`

func licenseMIT(holder string) string {
	return fmt.Sprintf(mitTemplate, holder)
}

func writeRegistry(path string, entries []registryEntry) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Samuel v2 plugin registry — generated by scripts/migrate-v1-skills.\n")
	b.WriteString("# See https://github.com/samuelpkg/samuel-registry for contribution rules.\n\n")
	b.WriteString("schema_version = 1\n\n")
	for _, e := range entries {
		b.WriteString("[[plugins]]\n")
		fmt.Fprintf(&b, "name        = %q\n", e.Name)
		fmt.Fprintf(&b, "repo        = %q\n", e.Repo)
		if e.Subpath != "" {
			fmt.Fprintf(&b, "subpath     = %q\n", e.Subpath)
		}
		fmt.Fprintf(&b, "latest      = %q\n", e.Latest)
		if e.Description != "" {
			fmt.Fprintf(&b, "description = %q\n", e.Description)
		}
		if len(e.Categories) > 0 {
			fmt.Fprintf(&b, "categories  = %s\n", tomlList(e.Categories))
		}
		if len(e.Tags) > 0 {
			fmt.Fprintf(&b, "tags        = %s\n", tomlList(e.Tags))
		}
		if e.Upstream {
			b.WriteString("upstream    = true\n")
		}
		b.WriteString("\n")
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func tomlList(items []string) string {
	quoted := make([]string, len(items))
	for i, s := range items {
		quoted[i] = fmt.Sprintf("%q", s)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// categoriesFor produces stable tags from the v1 frontmatter so that
// `samuel search --category framework` works post-migration.
func categoriesFor(fm frontmatter) []string {
	if fm.Category == "" {
		return nil
	}
	return []string{fm.Category}
}

func tagsFor(fm frontmatter) []string {
	var tags []string
	if fm.Language != "" {
		tags = append(tags, fm.Language)
	}
	return tags
}

// --- frontmatter parsing -------------------------------------------------
//
// SKILL.md frontmatter follows a small subset of YAML: top-level keys, a
// nested `metadata` map with scalar leaves, and the `description` field
// using the `|` block-scalar style. We parse it by hand to avoid pulling
// gopkg.in/yaml.v3 into the project just for a one-shot tool.

func readFrontmatter(path string) (frontmatter, error) {
	var fm frontmatter
	data, err := os.ReadFile(path)
	if err != nil {
		return fm, err
	}
	rd := bufio.NewReader(bytes.NewReader(data))
	first, err := rd.ReadString('\n')
	if err != nil || strings.TrimSpace(first) != "---" {
		return fm, errors.New("missing frontmatter open")
	}
	var (
		body         []string
		inMetadata   bool
		descLines    []string
		readingDesc  bool
		descIndent   int
		current      string
		closeMarker  = "---"
		sawCloseMark bool
	)
	for {
		line, err := rd.ReadString('\n')
		if err != nil && line == "" {
			break
		}
		trimmed := strings.TrimRight(line, "\n")
		if strings.TrimSpace(trimmed) == closeMarker {
			sawCloseMark = true
			break
		}
		body = append(body, trimmed)
		if err == io.EOF {
			break
		}
	}
	if !sawCloseMark {
		return fm, errors.New("missing frontmatter close")
	}

	for _, line := range body {
		// description block scalar
		if readingDesc {
			if line == "" {
				descLines = append(descLines, "")
				continue
			}
			indent := leadingSpaces(line)
			if indent >= descIndent {
				descLines = append(descLines, strings.TrimLeft(line, " "))
				continue
			}
			readingDesc = false
			fm.Description = strings.TrimSpace(strings.Join(descLines, " "))
		}
		if strings.HasPrefix(line, "metadata:") {
			inMetadata = true
			continue
		}
		if inMetadata && (strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "\t")) {
			k, v := splitKV(strings.TrimLeft(line, " \t"))
			switch k {
			case "author":
				fm.Author = v
			case "version":
				fm.Version = v
			case "category":
				fm.Category = v
			case "language":
				fm.Language = v
			case "extensions":
				fm.Extensions = v
			case "source":
				fm.Source = v
			}
			continue
		}
		inMetadata = false
		k, v := splitKV(line)
		current = k
		switch k {
		case "name":
			fm.Name = v
		case "license":
			fm.License = v
		case "description":
			if v == "|" || v == ">" || v == "" {
				readingDesc = true
				descLines = descLines[:0]
				descIndent = 2 // YAML block scalar indent for top-level keys
				continue
			}
			fm.Description = v
		}
		_ = current
	}
	if readingDesc {
		fm.Description = strings.TrimSpace(strings.Join(descLines, " "))
	}
	return fm, nil
}

func splitKV(line string) (string, string) {
	i := strings.Index(line, ":")
	if i < 0 {
		return "", ""
	}
	k := strings.TrimSpace(line[:i])
	v := strings.TrimSpace(line[i+1:])
	v = strings.TrimPrefix(v, "\"")
	v = strings.TrimSuffix(v, "\"")
	return k, v
}

func leadingSpaces(s string) int {
	n := 0
	for _, r := range s {
		if r == ' ' {
			n++
			continue
		}
		if r == '\t' {
			n += 2
			continue
		}
		break
	}
	return n
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func nonEmpty(a, b string) string {
	if strings.TrimSpace(a) == "" {
		return b
	}
	return a
}

// --- file helpers --------------------------------------------------------

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrate-v1-skills: "+format+"\n", args...)
	os.Exit(1)
}
