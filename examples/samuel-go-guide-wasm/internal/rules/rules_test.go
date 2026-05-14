package rules

import "testing"

func TestLint_DetectsPanic(t *testing.T) {
	got := Lint("a.go", "panic(\"nope\")")
	if len(got) == 0 || got[0].Rule != "no-panic" {
		t.Fatalf("expected no-panic finding, got %+v", got)
	}
}

func TestLint_DetectsTODO(t *testing.T) {
	got := Lint("a.go", "// TODO: ship it")
	if len(got) == 0 {
		t.Fatalf("expected TODO finding")
	}
}

func TestLint_DetectsFmtPrintln(t *testing.T) {
	got := Lint("a.go", "fmt.Println(\"hi\")")
	if len(got) == 0 || got[0].Rule != "no-fmt-println" {
		t.Fatalf("expected no-fmt-println finding, got %+v", got)
	}
}

func TestLint_QuietGoodCode(t *testing.T) {
	if got := Lint("a.go", "return nil"); len(got) != 0 {
		t.Fatalf("expected no findings, got %+v", got)
	}
}
