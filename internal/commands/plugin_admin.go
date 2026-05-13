package commands

// Admin-side `samuel plugin …` subcommands. These are the surface plugin
// authors and CI workflows hit when authoring or publishing a plugin —
// distinct from the user-facing install/uninstall/ls/etc. flat commands.
//
// The reusable plugin-release workflow (samuel-plugin-release) calls:
//
//	samuel plugin validate <path/to/samuel-plugin.toml>
//	samuel plugin validate --registry <path/to/index.toml>
//	samuel plugin info --kind <path/to/samuel-plugin.toml>
//	samuel plugin info --name <path/to/samuel-plugin.toml>
//
// to read the manifest without parsing TOML in shell. Keeping these in
// the framework keeps every plugin repo schema-aware via a single tool.

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"

	"github.com/ar4mirez/samuel/internal/plugin/manifest"
)

var (
	pluginCmd = &cobra.Command{
		Use:   "plugin",
		Short: "Plugin authoring + registry administration",
	}
	pluginValidateCmd = &cobra.Command{
		Use:   "validate <path>",
		Short: "Validate a samuel-plugin.toml manifest or a samuel-registry index.toml",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPluginValidate,
	}
	pluginInfoCmd = &cobra.Command{
		Use:   "info <path>",
		Short: "Print a single manifest field (--name, --kind, etc.)",
		Args:  cobra.ExactArgs(1),
		RunE:  runPluginInfo,
	}
)

func init() {
	pluginValidateCmd.Flags().String("registry", "", "Validate a registry index.toml instead of a manifest")
	pluginValidateCmd.Flags().Bool("json", false, "Emit machine-readable JSON when validating a registry")

	pluginInfoCmd.Flags().Bool("name", false, "Print the manifest's name and exit")
	pluginInfoCmd.Flags().Bool("kind", false, "Print the manifest's kind and exit")
	pluginInfoCmd.Flags().Bool("version", false, "Print the manifest's version and exit")

	pluginCmd.AddCommand(pluginValidateCmd, pluginInfoCmd)
	rootCmd.AddCommand(pluginCmd)
}

func runPluginValidate(cmd *cobra.Command, args []string) error {
	if registryPath, _ := cmd.Flags().GetString("registry"); registryPath != "" {
		return validateRegistry(cmd, registryPath)
	}
	if len(args) == 0 {
		return fmt.Errorf("usage: samuel plugin validate <samuel-plugin.toml>  |  samuel plugin validate --registry <index.toml>")
	}
	if _, err := manifest.Load(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ok: %s\n", args[0])
	return nil
}

func runPluginInfo(cmd *cobra.Command, args []string) error {
	m, err := manifest.Load(args[0])
	if err != nil {
		return err
	}
	if v, _ := cmd.Flags().GetBool("name"); v {
		fmt.Fprintln(cmd.OutOrStdout(), m.Name)
		return nil
	}
	if v, _ := cmd.Flags().GetBool("kind"); v {
		fmt.Fprintln(cmd.OutOrStdout(), string(m.Kind))
		return nil
	}
	if v, _ := cmd.Flags().GetBool("version"); v {
		fmt.Fprintln(cmd.OutOrStdout(), m.Version)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "name:    %s\nversion: %s\nkind:    %s\n", m.Name, m.Version, m.Kind)
	return nil
}

// registryDoc mirrors the on-disk samuel-registry/index.toml schema —
// kept private to this file so plugin/registry's runtime index type can
// evolve independently of the validator.
type registryDoc struct {
	SchemaVersion int               `toml:"schema_version"`
	Plugins       []registryDocItem `toml:"plugins"`
}

type registryDocItem struct {
	Name        string   `toml:"name"`
	Repo        string   `toml:"repo"`
	Subpath     string   `toml:"subpath,omitempty"`
	Latest      string   `toml:"latest"`
	Description string   `toml:"description,omitempty"`
	Categories  []string `toml:"categories,omitempty"`
	Tags        []string `toml:"tags,omitempty"`
	Upstream    bool     `toml:"upstream,omitempty"`
	Deprecated  bool     `toml:"deprecated,omitempty"`
}

func validateRegistry(cmd *cobra.Command, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc registryDoc
	if err := toml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if doc.SchemaVersion != 1 {
		return fmt.Errorf("%s: unsupported schema_version=%d (expected 1)", path, doc.SchemaVersion)
	}
	seen := map[string]bool{}
	for i, p := range doc.Plugins {
		if p.Name == "" || !manifest.ValidName(p.Name) {
			return fmt.Errorf("%s: entry %d has invalid name %q", path, i, p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("%s: duplicate plugin name %q", path, p.Name)
		}
		seen[p.Name] = true
		if p.Repo == "" {
			return fmt.Errorf("%s: %q missing repo", path, p.Name)
		}
		if p.Latest == "" {
			return fmt.Errorf("%s: %q missing latest", path, p.Name)
		}
	}
	if jsonOut, _ := cmd.Flags().GetBool("json"); jsonOut {
		buf, _ := json.Marshal(doc.Plugins)
		fmt.Fprintln(cmd.OutOrStdout(), string(buf))
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ok: %s (%d plugins)\n", path, len(doc.Plugins))
	return nil
}
