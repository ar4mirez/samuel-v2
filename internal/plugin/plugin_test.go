package plugin

import (
	"context"
	"testing"
)

func TestStubs_SatisfyInterface(t *testing.T) {
	// Compile-time guarantees in kinds.go cover this, but a runtime
	// check belt-and-braces verifies the assertions can't be silently
	// removed by a refactor.
	var ps []Plugin = []Plugin{
		&SkillPlugin{ManifestData: Manifest{Name: "s", Kind: KindSkill}},
		&WasmPlugin{ManifestData: Manifest{Name: "w", Kind: KindWasm}},
		&OciPlugin{ManifestData: Manifest{Name: "o", Kind: KindOci}},
	}
	wantNames := []string{"s", "w", "o"}
	for i, p := range ps {
		if p.Name() != wantNames[i] {
			t.Errorf("stub %d name = %q, want %q", i, p.Name(), wantNames[i])
		}
		if p.Manifest().Name != wantNames[i] {
			t.Errorf("stub %d manifest name mismatch", i)
		}
		hs := p.Check(context.Background())
		if hs.OK {
			t.Errorf("stub %d Check should report not-OK while lifecycle is unimplemented", i)
		}
	}
}

func TestKindsAreConstants(t *testing.T) {
	kinds := []Kind{KindBuiltin, KindSkill, KindWasm, KindOci}
	seen := map[Kind]bool{}
	for _, k := range kinds {
		if seen[k] {
			t.Errorf("duplicate Kind constant %q", k)
		}
		seen[k] = true
	}
}
