package wasm

import (
	"encoding/binary"
)

// BuildFixtureWasm hand-encodes a minimal WebAssembly module that
// exports two i32-returning functions:
//
//	"health"                  -> i32  (returns healthVal)
//	"samuel_protocol_version" -> i32  (returns protocolVal)
//
// Used by wasm tests and by `scripts/wasm-fixtures` to materialize the
// committed binary fixture for the e2e suite (testdata/wasm-fixture/
// plugin.wasm). The encoding follows wasm core spec §5 (binary format).
func BuildFixtureWasm(healthVal, protocolVal int32) []byte {
	var out []byte
	// Magic + version.
	out = append(out, 0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00)
	// Type section: one functype () -> i32.
	out = appendSection(out, 0x01, []byte{
		0x01,             // count
		0x60, 0x00, 0x01, // form, paramCount=0, resultCount=1
		0x7f, // i32
	})
	// Function section: 2 functions of type 0.
	out = appendSection(out, 0x03, []byte{0x02, 0x00, 0x00})
	// Export section.
	exports := []struct {
		name string
		idx  byte
	}{
		{"health", 0},
		{"samuel_protocol_version", 1},
	}
	expBody := []byte{byte(len(exports))}
	for _, e := range exports {
		expBody = append(expBody, byte(len(e.name)))
		expBody = append(expBody, []byte(e.name)...)
		expBody = append(expBody, 0x00) // kind=func
		expBody = append(expBody, e.idx)
	}
	out = appendSection(out, 0x07, expBody)

	// Code section.
	codeBody := []byte{0x02} // 2 function bodies
	codeBody = append(codeBody, encodeFnBody(healthVal)...)
	codeBody = append(codeBody, encodeFnBody(protocolVal)...)
	out = appendSection(out, 0x0a, codeBody)
	return out
}

// encodeFnBody emits the locals + instructions for a function that
// returns a single i32 constant.
func encodeFnBody(v int32) []byte {
	// instructions: 0 locals, i32.const v (LEB128 signed), end (0x0b)
	insts := []byte{0x00} // 0 locals
	insts = append(insts, 0x41)
	insts = append(insts, sleb128(int64(v))...)
	insts = append(insts, 0x0b)
	// prefix with body size.
	return append(uleb128(uint64(len(insts))), insts...)
}

// uleb128 encodes an unsigned integer as variable-length LEB128.
func uleb128(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		v >>= 7
		if v != 0 {
			b |= 0x80
		}
		out = append(out, b)
		if v == 0 {
			return out
		}
	}
}

// sleb128 encodes a signed integer as variable-length LEB128.
func sleb128(v int64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7f)
		sign := b & 0x40
		v >>= 7
		more := !((v == 0 && sign == 0) || (v == -1 && sign != 0))
		if more {
			b |= 0x80
		}
		out = append(out, b)
		if !more {
			return out
		}
	}
}

func appendSection(out []byte, id byte, body []byte) []byte {
	out = append(out, id)
	out = append(out, uleb128(uint64(len(body)))...)
	return append(out, body...)
}

// silence unused import warnings during tooling churn.
var _ = binary.LittleEndian
