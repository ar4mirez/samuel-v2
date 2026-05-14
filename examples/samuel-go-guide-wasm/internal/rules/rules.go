// Package rules holds the shared lint rules used by both
// samuel-go-guide (skill) and samuel-go-guide-wasm (TinyGo plugin).
// Keeping the rules in one package guarantees the skill and the wasm
// port stay in lockstep — there is no host-side concession that would
// prevent TinyGo from compiling this code for target=wasi.
//
// PRD 0009 risk mitigation: "Reference plugin lags the skill version's
// lint rules → shared rules package both can import."
package rules

import (
	"strings"
)

// Diagnostic is a single lint finding.
type Diagnostic struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// Lint applies every rule to body and returns the union of findings,
// in stable iteration order. path is reported back inside each
// diagnostic so downstream display code can render `path:line: rule:
// message`.
func Lint(path, body string) []Diagnostic {
	var out []Diagnostic
	for i, line := range strings.Split(body, "\n") {
		lineNo := i + 1
		if d, ok := checkPanic(path, lineNo, line); ok {
			out = append(out, d)
		}
		if d, ok := checkTODO(path, lineNo, line); ok {
			out = append(out, d)
		}
		if d, ok := checkPrintln(path, lineNo, line); ok {
			out = append(out, d)
		}
	}
	return out
}

func checkPanic(path string, lineNo int, line string) (Diagnostic, bool) {
	if strings.Contains(line, "panic(") && !strings.Contains(line, "// nolint:panic") {
		return Diagnostic{
			Path:    path,
			Line:    lineNo,
			Rule:    "no-panic",
			Message: "prefer returning an error to panic()",
		}, true
	}
	return Diagnostic{}, false
}

func checkTODO(path string, lineNo int, line string) (Diagnostic, bool) {
	if strings.Contains(line, "TODO") {
		return Diagnostic{
			Path:    path,
			Line:    lineNo,
			Rule:    "todo-comment",
			Message: "TODO comments should reference an issue",
		}, true
	}
	return Diagnostic{}, false
}

func checkPrintln(path string, lineNo int, line string) (Diagnostic, bool) {
	if strings.Contains(line, "fmt.Println") {
		return Diagnostic{
			Path:    path,
			Line:    lineNo,
			Rule:    "no-fmt-println",
			Message: "use the project's structured logger instead of fmt.Println",
		}, true
	}
	return Diagnostic{}, false
}
