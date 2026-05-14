package service

// BuildFixtureWasm + helpers mirror internal/plugin/wasm/fixture_test.go.
// Duplicated here so the integration test can build wasm fixtures
// without exposing the encoder publicly.

func BuildFixtureWasm(healthVal, protocolVal int32) []byte {
	out := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	out = appendSection(out, 0x01, []byte{0x01, 0x60, 0x00, 0x01, 0x7f})
	out = appendSection(out, 0x03, []byte{0x02, 0x00, 0x00})
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
		expBody = append(expBody, 0x00, e.idx)
	}
	out = appendSection(out, 0x07, expBody)
	codeBody := []byte{0x02}
	codeBody = append(codeBody, encodeFnBody(healthVal)...)
	codeBody = append(codeBody, encodeFnBody(protocolVal)...)
	out = appendSection(out, 0x0a, codeBody)
	return out
}

func encodeFnBody(v int32) []byte {
	insts := []byte{0x00}
	insts = append(insts, 0x41)
	insts = append(insts, sleb128(int64(v))...)
	insts = append(insts, 0x0b)
	return append(uleb128(uint64(len(insts))), insts...)
}

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
