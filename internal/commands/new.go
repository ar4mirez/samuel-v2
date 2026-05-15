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
		if err := scaffoldOciPlugin(target, name); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown plugin kind %q (expected: skill | wasm | oci)", kind)
	}
	ui.Print("Scaffolded plugin at %s", target)
	switch strings.ToLower(kind) {
	case "oci":
		ui.ListItem(1, "next: cd %s && make image", name)
	default:
		ui.ListItem(1, "next: cd %s && make wasm", name)
	}
	return nil
}

func scaffoldOciPlugin(target, name string) error {
	files := map[string]string{
		"samuel-plugin.toml":           ociManifestTemplate(name),
		"Containerfile":                ociContainerfileTemplate(),
		"Makefile":                     ociMakefileTemplate(name),
		"README.md":                    ociReadmeTemplate(name),
		".github/workflows/release.yml": ociReleaseWorkflowTemplate(),
		".gitignore":                   "*.bundle\n*.tar\n",
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

func ociManifestTemplate(name string) string {
	zeroDigest := strings.Repeat("0", 64)
	return fmt.Sprintf(`name = %q
version = "0.1.0"
kind = "oci"
summary = "TODO: one-line description"
license = "MIT"

[samuel]
framework = "^2.3.0"
protocol = "^1.0.0"

[oci]
# CI replaces the digest below on every release. Tag-only refs are
# rejected by 'samuel install' (PRD 0010 §Functional 2).
image        = "ghcr.io/your-org/%s@sha256:%s"
entrypoint   = ["/opt/%s/run"]
workdir      = "/workspace"
cpu_quota    = "1"
memory_limit = "512m"

[capabilities]
exec = false
env  = []

[capabilities.filesystem]
read  = ["/workspace"]
write = []

[capabilities.network]
# Deny-by-default. List the hosts the plugin needs here; everything
# else triggers a consent prompt at proxy time.
allowed_hosts = []
`, name, name, zeroDigest, name)
}

func ociContainerfileTemplate() string {
	return `# syntax=docker/dockerfile:1.6
FROM alpine:3.20

RUN adduser -D -u 10001 samuel \
 && mkdir -p /workspace && chown samuel:samuel /workspace

# TODO: install your plugin's runtime into /opt/<name>/.

USER samuel
WORKDIR /workspace
ENTRYPOINT ["/bin/sh", "-c", "echo 'hello from samuel oci plugin' && exec sleep infinity"]
`
}

func ociMakefileTemplate(name string) string {
	return fmt.Sprintf(`IMAGE   ?= ghcr.io/your-org/%s
TAG     ?= dev
PLATFORM ?= linux/amd64,linux/arm64

.PHONY: image push test

image:
	podman build --platform=$(PLATFORM) -t $(IMAGE):$(TAG) -f Containerfile .

push:
	podman push $(IMAGE):$(TAG)

test:
	podman run --rm $(IMAGE):$(TAG) --help || true
`, name)
}

func ociReadmeTemplate(name string) string {
	return fmt.Sprintf(`# %s

A Samuel OCI plugin scaffold.

## Build

`+"```bash\nmake image\n```"+`

## Install locally for testing

`+"```bash\nsamuel install file://$(pwd) --allow-unsigned\n```"+`

## Release

The release workflow at .github/workflows/release.yml builds the
image multi-arch (linux/amd64 + linux/arm64), signs with cosign
keyless OIDC, pushes to GHCR, and writes the digest back into
samuel-plugin.toml.

See: https://samuelpkg.github.io/samuel/docs/plugin-authors/oci
`, name)
}

func ociReleaseWorkflowTemplate() string {
	return `name: release

on:
  push:
    tags: ["v*.*.*"]

permissions:
  contents: write
  packages: write
  id-token: write

env:
  IMAGE: ghcr.io/${{ github.repository_owner }}/${{ github.event.repository.name }}

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-qemu-action@v3
      - uses: docker/setup-buildx-action@v3
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - id: build
        uses: docker/build-push-action@v6
        with:
          context: .
          file: Containerfile
          platforms: linux/amd64,linux/arm64
          push: true
          tags: |
            ${{ env.IMAGE }}:${{ github.ref_name }}
            ${{ env.IMAGE }}:latest
      - uses: sigstore/cosign-installer@v4.1.2
        with:
          # cosign-installer's v3 floating tag tops out at cosign v2.x.
          # No floating v4 tag exists yet, so pin the exact patch
          # (v4.1.2). v4.x.x is required for cosign v3 binaries, which
          # is the minimum supporting --new-bundle-format on
          # 'cosign sign' (image signing).
          cosign-release: v3.0.6
      - env:
          COSIGN_EXPERIMENTAL: "1"
        run: |
          digest="${{ steps.build.outputs.digest }}"
          cosign sign --yes --new-bundle-format \
            --bundle plugin.bundle \
            ${{ env.IMAGE }}@${digest}
          sed -i "s|@sha256:[0-9a-f]\{64\}|@${digest}|g" samuel-plugin.toml
      - uses: actions/upload-artifact@v4
        with:
          name: oci-plugin-${{ github.ref_name }}
          path: |
            samuel-plugin.toml
            plugin.bundle
`
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
      - name: Sign with cosign (keyless, sigstore protobuf-bundle format)
        env:
          COSIGN_EXPERIMENTAL: "1"
        run: |
          # --new-bundle-format emits the sigstore-go protobuf-JSON
          # bundle (mediaType: application/vnd.dev.sigstore.bundle+json)
          # the framework verifier expects. The legacy cosign --bundle
          # output isn't sigstore-go compatible.
          cosign sign-blob --yes --new-bundle-format --bundle plugin.wasm.bundle plugin.wasm
      - uses: actions/upload-artifact@v4
        with:
          name: %s-${{ github.ref_name }}
          path: |
            plugin.wasm
            plugin.wasm.bundle
            samuel-plugin.toml
`, name)
}
