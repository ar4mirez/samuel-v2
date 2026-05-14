// TinyGo entry point for samuel-go-guide-wasm. Compiled via:
//
//	tinygo build -o ../plugin.wasm -target=wasi -no-debug -opt=2 ./
//
// Exports:
//   - samuel_protocol_version() i32  → 1
//   - health() i32                   → 0 (healthy)
//   - lint() i32                     → 0 (placeholder; real impl uses
//                                          samuel.fs_read + JSON I/O)
//
// The "lint" surface here is intentionally minimal — the shared
// rules package is in samuel-go-guide-wasm/internal/rules and would
// be wired up via samuel.fs_read / writeJSON helpers from the SDK in
// the production build. We keep the entry point thin so cold-start
// stays under the PRD 0009 budget.
package main

//export samuel_protocol_version
func samuel_protocol_version() int32 { return 1 }

//export health
func health() int32 { return 0 }

//export lint
func lint() int32 {
	// Real implementation reads the workspace via samuel.fs_read,
	// passes the body to rules.Lint, and writes the JSON diagnostic
	// list back via samuel.callback. The framework's runtime handles
	// the host functions; this body is intentionally a no-op so the
	// scaffold stays buildable under TinyGo without the SDK pulled
	// into go.mod yet.
	return 0
}

func main() {} // TinyGo requires main; never executed under wasi
