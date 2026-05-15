package commands

import (
	"sort"

	"github.com/spf13/cobra"

	"github.com/samuelpkg/samuel/internal/agents"
	"github.com/samuelpkg/samuel/internal/config"
	"github.com/samuelpkg/samuel/internal/lock"
	"github.com/samuelpkg/samuel/internal/plugin/oci"
	"github.com/samuelpkg/samuel/internal/ui"
)

// `samuel update --agents` re-resolves each registered agent's default
// container image digest and pins it into samuel.lock. It is the
// counterpart to the per-plugin update path and lands in PRD 0010
// §Functional 7.7.
//
// The resolver is the OCI engine: we look up each adapter's
// DefaultImage and ask the engine to Inspect or Pull. The samuel.lock
// `[agents]` block is the source of truth for which digest a given
// `samuel run` should pull.

// agentImageResolver lets tests stub the digest resolver. Production
// wires it through an oci.CLI.
type agentImageResolver func(image string) (string, error)

var testAgentResolver agentImageResolver

func runUpdateAgents(cmd *cobra.Command, projectDir string) error {
	resolve := testAgentResolver
	if resolve == nil {
		rt, err := oci.DetectRuntime()
		if err != nil {
			return renderStructuredError(err)
		}
		engine := oci.NewCLI(rt)
		resolve = func(image string) (string, error) {
			return engine.Pull(cmd.Context(), image)
		}
	}
	lf, err := lock.ReadLockfile(projectDir)
	if err != nil {
		return renderStructuredError(err)
	}
	// Snapshot every registered adapter (built-in + plugin-registered)
	// so the lockfile is deterministic regardless of which adapter the
	// user has installed.
	names := agents.List()
	sort.Strings(names)
	updated := make([]config.LockedAgent, 0, len(names))
	for _, name := range names {
		a, ok := agents.Get(name)
		if !ok {
			continue
		}
		img := a.Manifest().DefaultImage
		if img == "" {
			continue
		}
		digest, err := resolve(img)
		if err != nil {
			ui.Warn("could not resolve %s (%s): %v", name, img, err)
			continue
		}
		updated = append(updated, config.LockedAgent{Adapter: name, Image: img, Digest: digest})
	}
	lf.Agents = updated
	if err := lock.WriteLockfile(projectDir, lf); err != nil {
		return renderStructuredError(err)
	}
	if JSONMode(cmd) {
		ui.PrintJSON(commandPath(cmd), map[string]any{"agents": updated})
		return nil
	}
	ui.Bold("Pinned %d agent image(s)", len(updated))
	for _, la := range updated {
		ui.ListItem(1, "%s — %s @ %s", la.Adapter, la.Image, la.Digest)
	}
	return nil
}
