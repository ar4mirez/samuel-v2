package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/plugin/manifest"
	"github.com/samuelpkg/samuel/internal/ui"
)

// `samuel new plugin --kind=wasm|skill|oci --name=<name>` scaffolds a
// publishable plugin tree per PRD 0009 §Functional 5. The skill +
// wasm scaffolds are landed in v2.2; the oci scaffold is deferred to
// PRD 0010 (v2.3.0) and prints a one-line "not yet implemented" note
// rather than producing a half-formed tree.

var newCmd = &cobra.Command{
	Use:   "new",
	Short: "Scaffold a new plugin",
	Long:  `Scaffold a new plugin (skill, wasm, or oci) under the current directory.`,
}

var newPluginCmd = &cobra.Command{
	Use:   "plugin",
	Short: "Scaffold a new plugin tree",
	Long: `Scaffold a new plugin under the current directory.

Examples:
  samuel new plugin --kind=wasm --name=my-translator
  samuel new plugin --kind=skill --name=go-guide-lite`,
	RunE: runNewPlugin,
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.AddCommand(newPluginCmd)
	newPluginCmd.Flags().String("name", "", "Plugin name (lowercase, dash-separated)")
	newPluginCmd.Flags().String("kind", "wasm", "Plugin kind: skill | wasm | oci")
	newPluginCmd.Flags().Bool("force", false, "Overwrite an existing directory")
}

func runNewPlugin(cmd *cobra.Command, _ []string) error {
	name, _ := cmd.Flags().GetString("name")
	kind, _ := cmd.Flags().GetString("kind")
	force, _ := cmd.Flags().GetBool("force")
	if name == "" {
		return errors.New("--name is required")
	}
	if !manifest.ValidName(name) {
		return fmt.Errorf("invalid plugin name %q: must match [a-z0-9][a-z0-9-]*, 2-64 chars", name)
	}
	dir, err := os.Getwd()
	if err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	if !force {
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("directory %s already exists; pass --force to overwrite", target)
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}

	switch strings.ToLower(kind) {
	case "wasm":
		if err := scaffoldWasmPlugin(target, name); err != nil {
			return err
		}
	case "skill":
		if err := scaffoldSkillPlugin(target, name); err != nil {
			return err
		}
	case "oci":
		ui.Warn("oci-tier scaffolding lands in PRD 0010 (v2.3.0). Skeleton not generated.")
		return nil
	default:
		return fmt.Errorf("unknown plugin kind %q (expected: skill | wasm | oci)", kind)
	}
	ui.Print("Scaffolded plugin at %s", target)
	ui.ListItem(1, "next: cd %s && make wasm", name)
	return nil
}

func scaffoldWasmPlugin(target, name string) error {
	files := map[string]string{
		"samuel-plugin.toml": wasmManifestTemplate(name),
		"cmd/main.go":        wasmHelloMain(),
		"go.mod":             wasmGoMod(name),
		"Makefile":           wasmMakefile(),
		"README.md":          wasmReadme(name),
		".github/workflows/release.yml": wasmReleaseWorkflow(name),
		".gitignore":         "plugin.wasm\n",
	}
	for rel, content := range files {
		dst := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func scaffoldSkillPlugin(target, name string) error {
	files := map[string]string{
		"samuel-plugin.toml": fmt.Sprintf(`name = %q
version = "0.1.0"
kind = "skill"
summary = "TODO: one-line description"

[capabilities]
filesystem = { read = ["/workspace"], write = [] }
`, name),
		"SKILL.md": fmt.Sprintf("---\nname: %s\ndescription: TODO\n---\n\n# %s\n\nReplace this body with your skill content.\n", name, name),
		"README.md": fmt.Sprintf("# %s\n\nSamuel skill plugin scaffold. Customize SKILL.md and samuel-plugin.toml.\n", name),
	}
	for rel, content := range files {
		dst := filepath.Join(target, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func wasmManifestTemplate(name string) string {
	return fmt.Sprintf(`name = %q
version = "0.1.0"
kind = "wasm"
summary = "TODO: one-line description"
license = "MIT"

[samuel]
framework = "^2.2.0"
protocol = "^1.0.0"

[wasm]
module = "plugin.wasm"
exports = ["hello"]

[runtime]
max_memory  = 64
timeout     = "5s"
hard_timeout = "30s"
exports     = ["hello"]

[capabilities]
filesystem = { read = ["/workspace"], write = [] }
env = []

[capabilities.network]
hosts = []
`, name)
}

func wasmHelloMain() string {
	return `package main

// Minimal TinyGo plugin. Build with:
//
//   tinygo build -o plugin.wasm -target=wasi -no-debug -opt=2 ./cmd
//
// The framework calls "hello" once per invocation. Return value
// 0 = OK; non-zero is surfaced as a structured error.

//export samuel_protocol_version
func samuel_protocol_version() int32 { return 1 }

//export health
func health() int32 { return 0 }

//export hello
func hello() int32 { return 0 }

func main() {} // TinyGo requires main; never executed under wasi
`
}

func wasmGoMod(name string) string {
	return fmt.Sprintf(`module github.com/%s/%s

go 1.22
`, "your-org", name)
}

func wasmMakefile() string {
	return `# Sample plugin Makefile. Customize as needed.

PLUGIN := plugin.wasm

.PHONY: wasm test clean

wasm:
	tinygo build -o $(PLUGIN) -target=wasi -no-debug -opt=2 ./cmd

test:
	go test ./...

clean:
	rm -f $(PLUGIN)
`
}

func wasmReadme(name string) string {
	return fmt.Sprintf(`# %s

A Samuel WASM plugin scaffold.

## Build

`+"```bash\n"+`make wasm
`+"```\n"+`

## Install locally for testing

`+"```bash\n"+`samuel install file://$(pwd)
`+"```\n"+`

## Release

The release workflow at .github/workflows/release.yml builds, signs
(cosign keyless OIDC), and publishes to the configured registry on
each tag.

See: https://samuelpkg.github.io/samuel/docs/plugin-authors/wasm
`, name)
}

func wasmReleaseWorkflow(name string) string {
	return fmt.Sprintf(`name: release

on:
  push:
    tags: ["v*.*.*"]

permissions:
  contents: write   # softprops/action-gh-release needs write to publish the release
  id-token: write   # keyless cosign signing via OIDC

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: acifani/setup-tinygo@v2
        with:
          tinygo-version: 0.31.2
      - name: Build plugin.wasm
        run: tinygo build -o plugin.wasm -target=wasi -no-debug -opt=2 ./cmd
      - uses: sigstore/cosign-installer@v3
      - name: Sign with cosign (keyless)
        env:
          COSIGN_EXPERIMENTAL: "1"
        run: |
          cosign sign-blob --yes --output-signature plugin.wasm.sig --output-certificate plugin.wasm.pem plugin.wasm
      - uses: actions/upload-artifact@v4
        with:
          name: %s-${{ github.ref_name }}
          path: |
            plugin.wasm
            plugin.wasm.sig
            plugin.wasm.pem
            samuel-plugin.toml
`, name)
}
