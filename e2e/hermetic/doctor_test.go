//go:build e2e

package hermetic

import (
	"path/filepath"
	"strings"
	"testing"
)

// Doctor block — covers rc.10 (stub-verifier advisory), rc.11 (plugin
// health), rc.14 (doctor --fix actually repairs plugin: failures).

func TestDoctor_FrameworkChecksPass(t *testing.T) {
	p := newProject(t)
	out := p.mustSamuel("doctor")
	assertContains(t, out, "samuel-builtins", "doctor must run framework check")
	assertContains(t, out, "project-layout", "doctor must run layout check")
	assertContains(t, out, "Summary: 2 passed", "fresh project should report 2 framework checks pass")
}

func TestDoctor_PrintsVerifierAdvisory(t *testing.T) {
	// v2.1 follow-up to rc.10: by default the production sigstore
	// verifier is active and the advisory line confirms that. When
	// SAMUEL_VERIFY_STUB=1 is set the stub-mode banner surfaces
	// instead; both branches MUST keep an Advisories section.
	p := newProject(t)
	out := p.mustSamuel("doctor")
	assertContains(t, out, "Advisories:", "doctor must render advisories section")
	if !strings.Contains(out, "sigstore-go (production)") && !strings.Contains(out, "stub (test mode") {
		t.Errorf("expected advisory naming the verifier (sigstore-go production or stub test mode); got:\n%s", out)
	}
}

func TestDoctor_AddsPluginCheckWhenPluginInstalled(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")
	out := p.mustSamuel("doctor")
	assertContains(t, out, "plugin:sample-skill", "doctor must add plugin:<name> check post-install")
	assertContains(t, out, "1.0.0 (skill) — manifest + artifact intact", "healthy plugin renders intact line")
}

func TestDoctor_DetectsCorruptedPlugin(t *testing.T) {
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")
	p.rmFile(filepath.Join(".samuel", "plugins", "sample-skill", "SKILL.md"))

	out := p.mustSamuel("doctor")
	assertContains(t, out, "✗ plugin:sample-skill", "corrupted plugin must render as failed")
	assertContains(t, out, "SKILL.md missing", "failure message must surface the missing artifact")
	assertContains(t, out, "fix: samuel install sample-skill --force", "fix hint must point at recovery command")
}

func TestDoctor_FixRepairsCorruptedPlugin(t *testing.T) {
	// rc.14 regression: doctor --fix used to error with
	// `no plugin matches plugin:foo` for installed-plugin failures
	// because attemptFix only knew about the orchestrator.
	p := newProject(t)
	p.setupRegistry("sample-skill", "1.0.0")
	p.mustSamuel("install", "sample-skill")
	p.rmFile(filepath.Join(".samuel", "plugins", "sample-skill", "SKILL.md"))

	out := p.mustSamuel("doctor", "--fix")
	assertNotContains(t, out, "no plugin matches", "doctor --fix must not fall through to the no-plugin error")
	assertContains(t, out, "(repaired this run)", "doctor --fix must mark the repair")
	if !p.fileExists(filepath.Join(".samuel", "plugins", "sample-skill", "SKILL.md")) {
		t.Error("SKILL.md was not restored by --fix")
	}
	// Post-fix re-check should render the plugin as healthy.
	if strings.Contains(out, "✗ plugin:sample-skill") {
		t.Errorf("post-fix render still shows plugin as failed:\n%s", out)
	}
}

func TestDoctor_JSONEnvelopeShapeStable(t *testing.T) {
	p := newProject(t)
	out := p.mustSamuel("doctor", "--json")
	// We're not parsing the JSON here — the unit tests in
	// internal/commands cover envelope semantics. What we assert is
	// that the JSON-mode output keeps its top-level keys, since
	// machine consumers (CI scripts, IDE integrations) read these.
	for _, key := range []string{
		`"schemaVersion"`,
		`"command"`,
		`"data"`,
		`"checks"`,
		`"summary"`,
		`"advisories"`,
	} {
		assertContains(t, out, key, "doctor --json envelope must include "+key)
	}
}
