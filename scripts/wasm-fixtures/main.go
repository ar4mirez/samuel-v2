// scripts/wasm-fixtures rebuilds the committed binary fixture at
// testdata/wasm-fixture/plugin.wasm using the hand-encoded helper in
// internal/plugin/wasm. The committed binary lets the hermetic e2e
// tier exercise the wasm install path without a TinyGo toolchain in
// CI. Run via `make wasm-fixtures` after touching the encoder.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/samuelpkg/samuel/internal/plugin/wasm"
)

func main() {
	out := flag.String("out", "testdata/wasm-fixture/plugin.wasm", "destination for the precompiled fixture")
	health := flag.Int("health", 0, "value health() returns (0 = healthy)")
	protocol := flag.Int("protocol", 1, "value samuel_protocol_version() returns")
	flag.Parse()

	body := wasm.BuildFixtureWasm(int32(*health), int32(*protocol))
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, body, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d bytes to %s\n", len(body), *out)
}
