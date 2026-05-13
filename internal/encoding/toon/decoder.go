package toon

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Warning records a non-fatal parsing event recovered by the decoder.
// Examples: minor-version drift, a malformed tabular row that was
// skipped, a duplicate key that overrode an earlier value.
type Warning struct {
	Line    int    // 1-indexed source line
	Message string // human-readable summary
}

// Decoder parses TOON. Use Unmarshal for one-shot decoding.
type Decoder struct {
	warnings []Warning
}

// Warnings returns all non-fatal events accumulated during Decode.
// The slice is owned by the Decoder; callers should treat it as
// read-only.
func (d *Decoder) Warnings() []Warning { return d.warnings }

// Unmarshal parses src as TOON and returns the root object together
// with any accumulated warnings. Fatal parse errors return ok=false
// and err.
func Unmarshal(src []byte) (*Object, []Warning, error) {
	d := &Decoder{}
	root, err := d.Decode(src)
	return root, d.warnings, err
}

// Decode parses src and returns the root object.
func (d *Decoder) Decode(src []byte) (*Object, error) {
	lines, err := splitLines(src)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("toon: empty document")
	}
	headerIdx := firstNonBlank(lines)
	if headerIdx < 0 {
		return nil, fmt.Errorf("toon: empty document")
	}
	observed, vErr := checkVersionLine(lines[headerIdx].text)
	if vErr != nil {
		return nil, &decodeError{Line: lines[headerIdx].num, Cause: vErr}
	}
	if observed != SpecVersion {
		d.warnings = append(d.warnings, Warning{
			Line:    lines[headerIdx].num,
			Message: fmt.Sprintf("TOON minor-version drift: file=%s, parser=%s", observed, SpecVersion),
		})
	}
	body := lines[headerIdx+1:]
	p := &parser{lines: body, dec: d}
	return p.parseObject(0)
}

// parser is a recursive-descent parser over a slice of source lines.
type parser struct {
	lines []sourceLine
	i     int // index of next unconsumed line
	dec   *Decoder
}

type sourceLine struct {
	num    int    // 1-indexed line number in source
	text   string // raw text (no trailing newline)
	indent int    // count of leading spaces; tabs are rejected by splitLines
}

func (p *parser) peek() (sourceLine, bool) {
	for p.i < len(p.lines) {
		line := p.lines[p.i]
		if isBlankOrComment(line.text) {
			p.i++
			continue
		}
		return line, true
	}
	return sourceLine{}, false
}

// parseObject reads keyed entries at exactly `depth` indentation,
// stopping when the next non-blank line is dedented.
func (p *parser) parseObject(depth int) (*Object, error) {
	obj := NewObject()
	wantIndent := depth * len(indentUnit)
	for {
		line, ok := p.peek()
		if !ok {
			break
		}
		if line.indent < wantIndent {
			break
		}
		if line.indent > wantIndent {
			return nil, &decodeError{Line: line.num, Cause: fmt.Errorf("unexpected indent: got %d spaces, want %d", line.indent, wantIndent)}
		}
		p.i++
		key, rest, err := parseKey(line.text[wantIndent:])
		if err != nil {
			return nil, &decodeError{Line: line.num, Cause: err}
		}
		v, err := p.parseValue(line.num, depth, rest)
		if err != nil {
			return nil, err
		}
		if _, exists := obj.values[key]; exists {
			p.dec.warnings = append(p.dec.warnings, Warning{
				Line:    line.num,
				Message: fmt.Sprintf("duplicate key %q (overwriting previous value)", key),
			})
		}
		obj.Set(key, v)
	}
	return obj, nil
}

// parseValue resolves the value half of a "key:" or "key[...]:" line.
// rest is the text after the key (starting with the syntax marker —
// usually ":" or "[N]" or "[N]{...}").
func (p *parser) parseValue(lineNum, depth int, rest string) (Value, error) {
	rest = strings.TrimLeft(rest, " \t")
	switch {
	case strings.HasPrefix(rest, ":"):
		body := strings.TrimSpace(rest[1:])
		if body == "" {
			// Nested object on next line.
			return p.parseObject(depth + 1)
		}
		if body == "{}" {
			return NewObject(), nil
		}
		if body == "[]" {
			return []any{}, nil
		}
		return decodeScalar(body)
	case strings.HasPrefix(rest, "["):
		return p.parseArray(lineNum, depth, rest)
	default:
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("expected ':' or '[' after key, got %q", rest)}
	}
}

// parseArray handles both scalar arrays (`key[N]: a,b,c`) and tabular
// arrays (`key[N]{c1,c2}:` followed by N rows).
func (p *parser) parseArray(lineNum, depth int, rest string) (Value, error) {
	if !strings.HasPrefix(rest, "[") {
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("expected '[' at array marker")}
	}
	closeIdx := strings.IndexByte(rest, ']')
	if closeIdx < 0 {
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("unterminated array length")}
	}
	nStr := rest[1:closeIdx]
	n, err := strconv.Atoi(nStr)
	if err != nil || n < 0 {
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("invalid array length %q", nStr)}
	}
	tail := rest[closeIdx+1:]
	if strings.HasPrefix(tail, "{") {
		// Tabular form: [N]{c1,c2,...}:
		braceClose := strings.IndexByte(tail, '}')
		if braceClose < 0 {
			return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("unterminated column list")}
		}
		cols := splitCSV(tail[1:braceClose])
		afterCols := strings.TrimSpace(tail[braceClose+1:])
		if afterCols != ":" {
			return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("expected ':' after column list, got %q", afterCols)}
		}
		return p.parseTabularRows(depth+1, n, cols)
	}
	tail = strings.TrimSpace(tail)
	if !strings.HasPrefix(tail, ":") {
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("expected ':' after array length, got %q", tail)}
	}
	body := strings.TrimSpace(tail[1:])
	if n == 0 {
		return []any{}, nil
	}
	if body == "" {
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("expected %d inline values after ':' for scalar array", n)}
	}
	cells := splitCSV(body)
	if len(cells) != n {
		return nil, &decodeError{Line: lineNum, Cause: fmt.Errorf("scalar array length mismatch: header=%d, found=%d", n, len(cells))}
	}
	out := make([]any, 0, len(cells))
	for _, c := range cells {
		val, err := decodeScalar(c)
		if err != nil {
			return nil, &decodeError{Line: lineNum, Cause: err}
		}
		out = append(out, val)
	}
	return out, nil
}

func (p *parser) parseTabularRows(depth, n int, cols []string) (*TableArray, error) {
	t := NewTableArray(cols...)
	if err := t.validate(); err != nil {
		return nil, err
	}
	rowIndent := depth * len(indentUnit)
	for read := 0; read < n; {
		line, ok := p.peek()
		if !ok || line.indent < rowIndent {
			// Header claimed N rows but we ran out — record and stop.
			p.dec.warnings = append(p.dec.warnings, Warning{
				Line:    -1,
				Message: fmt.Sprintf("tabular array truncated: header=%d, read=%d", n, read),
			})
			break
		}
		if line.indent > rowIndent {
			return nil, &decodeError{Line: line.num, Cause: fmt.Errorf("unexpected indent in table row: got %d, want %d", line.indent, rowIndent)}
		}
		p.i++
		cells := splitCSV(line.text[rowIndent:])
		if len(cells) != len(cols) {
			// Per-row malformation recovery: skip this row, record a
			// warning, keep parsing the rest of the table.
			p.dec.warnings = append(p.dec.warnings, Warning{
				Line:    line.num,
				Message: fmt.Sprintf("tabular row column count mismatch: want %d, got %d (row skipped)", len(cols), len(cells)),
			})
			t.Rows = append(t.Rows, nil)
			read++
			continue
		}
		row := make(map[string]Value, len(cols))
		bad := false
		for i, c := range cols {
			v, err := decodeScalar(cells[i])
			if err != nil {
				p.dec.warnings = append(p.dec.warnings, Warning{
					Line:    line.num,
					Message: fmt.Sprintf("tabular cell %q parse failed: %v (row skipped)", c, err),
				})
				bad = true
				break
			}
			row[c] = v
		}
		if bad {
			t.Rows = append(t.Rows, nil)
		} else {
			t.Rows = append(t.Rows, row)
		}
		read++
	}
	return t, nil
}

// parseKey splits "key: rest" or "key[..]..." into (key, rest). The
// returned rest still starts with the syntax marker so callers can
// dispatch on it.
func parseKey(s string) (key, rest string, err error) {
	for i, r := range s {
		switch r {
		case ':', '[':
			return strings.TrimSpace(s[:i]), s[i:], nil
		case ' ', '\t':
			// allow whitespace between key and marker
			continue
		}
	}
	return "", "", fmt.Errorf("missing ':' or '[' after key")
}

func decodeScalar(s string) (Value, error) {
	if strings.HasPrefix(s, `"`) {
		return decodeQuoted(s)
	}
	switch s {
	case "null":
		return nil, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f, nil
	}
	return s, nil
}

func decodeQuoted(s string) (string, error) {
	if len(s) < 2 || s[len(s)-1] != '"' {
		return "", fmt.Errorf("unterminated quoted string: %q", s)
	}
	body := s[1 : len(s)-1]
	var b strings.Builder
	b.Grow(len(body))
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(body) {
			return "", fmt.Errorf("dangling escape in %q", s)
		}
		i++
		switch body[i] {
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		default:
			return "", fmt.Errorf("unsupported escape \\%c in %q", body[i], s)
		}
	}
	return b.String(), nil
}

// splitCSV splits a comma-separated row, honouring double-quoted cells
// that may contain commas. JSON-style escapes are honoured inside
// quotes. Whitespace around bare cells is trimmed.
func splitCSV(s string) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	escape := false
	flush := func() {
		if !inQuote {
			out = append(out, strings.TrimSpace(b.String()))
		} else {
			out = append(out, b.String())
		}
		b.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escape {
			b.WriteByte('\\')
			b.WriteByte(c)
			escape = false
			continue
		}
		if c == '\\' && inQuote {
			escape = true
			continue
		}
		if c == '"' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if c == ',' && !inQuote {
			flush()
			continue
		}
		b.WriteByte(c)
	}
	flush()
	return out
}

// splitLines splits src on '\n', records 1-indexed line numbers, and
// rejects lines containing tabs in the indentation (TOON v3 mandates
// spaces).
func splitLines(src []byte) ([]sourceLine, error) {
	scanner := bufio.NewScanner(bytes.NewReader(src))
	// Allow long lines — TOON tabular rows can be wide.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var out []sourceLine
	num := 0
	for scanner.Scan() {
		num++
		txt := scanner.Text()
		indent := 0
		for indent < len(txt) {
			c := txt[indent]
			if c == ' ' {
				indent++
				continue
			}
			if c == '\t' {
				return nil, &decodeError{Line: num, Cause: fmt.Errorf("tab in indentation (TOON v3 mandates spaces)")}
			}
			break
		}
		out = append(out, sourceLine{num: num, text: txt, indent: indent})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func firstNonBlank(lines []sourceLine) int {
	for i, l := range lines {
		if !isBlank(l.text) {
			return i
		}
	}
	return -1
}

func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

func isBlankOrComment(s string) bool {
	t := strings.TrimSpace(s)
	return t == "" || strings.HasPrefix(t, "#")
}

// decodeError attaches source line context to a parse error.
type decodeError struct {
	Line  int
	Cause error
}

func (e *decodeError) Error() string {
	if e == nil || e.Cause == nil {
		return ""
	}
	if e.Line <= 0 {
		return e.Cause.Error()
	}
	return fmt.Sprintf("line %d: %s", e.Line, e.Cause.Error())
}

func (e *decodeError) Unwrap() error { return e.Cause }
