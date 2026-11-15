// Copyright (c) 2026 Neomantra Corp.

package nt3dcamspec

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// MaxSpecBytes bounds raw spec input size (1 MiB) to prevent memory exhaustion on untrusted inputs.
const MaxSpecBytes = 1 << 20

const maxYAMLDepth = 32

type yamlLine struct {
	num    int
	indent int
	text   string
}

// parseYAMLValue parses a bounded YAML 1.2 document subset (block & flow
// mappings, block & flow sequences, comments, strings, numbers, booleans, null)
// into JSON-compatible Go values (map[string]any, []any, string, float64, bool, nil).
func parseYAMLValue(data []byte) (any, error) {
	if len(data) > MaxSpecBytes {
		return nil, fmt.Errorf("nt3dcamspec: spec exceeds %d byte limit", MaxSpecBytes)
	}
	raw := strings.ReplaceAll(string(data), "\r\n", "\n")
	raw = strings.TrimPrefix(raw, "\uFEFF")

	lines, err := tokenizeYAMLLines(raw)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("nt3dcamspec: empty YAML document")
	}
	p := &yamlParser{lines: lines}
	val, err := p.parseBlock(0, lines[0].indent)
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		return nil, fmt.Errorf("nt3dcamspec: line %d: unexpected trailing content %q", ln.num, ln.text)
	}
	return val, nil
}

func tokenizeYAMLLines(src string) ([]yamlLine, error) {
	rawLines := strings.Split(src, "\n")
	out := make([]yamlLine, 0, len(rawLines))
	for idx, line := range rawLines {
		lineNum := idx + 1
		cleaned, err := stripYAMLComment(line)
		if err != nil {
			return nil, fmt.Errorf("nt3dcamspec: line %d: %w", lineNum, err)
		}
		trimmedRight := strings.TrimRight(cleaned, " \t")
		if trimmedRight == "" {
			continue
		}
		indent := 0
		for indent < len(trimmedRight) {
			ch := trimmedRight[indent]
			if ch == '\t' {
				return nil, fmt.Errorf("nt3dcamspec: line %d: tabs are not allowed for YAML indentation", lineNum)
			}
			if ch != ' ' {
				break
			}
			indent++
		}
		body := trimmedRight[indent:]
		if indent == 0 && body == "---" {
			if len(out) == 0 {
				continue
			}
			return nil, fmt.Errorf("nt3dcamspec: line %d: multiple YAML documents not supported", lineNum)
		}
		if indent == 0 && body == "..." {
			break
		}
		out = append(out, yamlLine{num: lineNum, indent: indent, text: body})
	}
	return out, nil
}

func stripYAMLComment(s string) (string, error) {
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '\\' {
				if i+1 >= len(s) {
					return "", fmt.Errorf("unterminated escape sequence")
				}
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '#':
			if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
				return s[:i], nil
			}
		}
	}
	if inSingle || inDouble {
		return "", fmt.Errorf("unterminated quoted string")
	}
	return s, nil
}

type yamlParser struct {
	lines []yamlLine
	pos   int
}

func (p *yamlParser) parseBlock(depth, baseIndent int) (any, error) {
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("nt3dcamspec: maximum YAML nesting depth exceeded")
	}
	if p.pos >= len(p.lines) {
		return nil, nil
	}
	ln := p.lines[p.pos]
	if strings.HasPrefix(ln.text, "{") || strings.HasPrefix(ln.text, "[") {
		flowText, endPos, err := p.collectFlowText()
		if err != nil {
			return nil, err
		}
		p.pos = endPos
		return parseFlowValue(flowText, depth+1, ln.num)
	}
	if isSeqLine(ln.text) {
		return p.parseBlockSeq(depth, baseIndent)
	}
	if _, _, ok := splitKeyValue(ln.text); ok {
		return p.parseBlockMap(depth, baseIndent)
	}
	p.pos++
	return parseScalar(ln.text, ln.num)
}

func isSeqLine(text string) bool {
	return text == "-" || strings.HasPrefix(text, "- ")
}

func (p *yamlParser) parseBlockSeq(depth, seqIndent int) ([]any, error) {
	var items []any
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent < seqIndent {
			break
		}
		if ln.indent > seqIndent {
			return nil, fmt.Errorf("nt3dcamspec: line %d: unexpected indentation %d in sequence (expected %d)", ln.num, ln.indent, seqIndent)
		}
		if !isSeqLine(ln.text) {
			break
		}
		if ln.text == "-" {
			p.pos++
			if p.pos >= len(p.lines) || p.lines[p.pos].indent <= seqIndent {
				items = append(items, nil)
				continue
			}
			val, err := p.parseBlock(depth+1, p.lines[p.pos].indent)
			if err != nil {
				return nil, err
			}
			items = append(items, val)
			continue
		}

		rest := strings.TrimLeft(ln.text[2:], " ")
		restOffset := len(ln.text) - len(rest)
		itemIndent := seqIndent + restOffset

		if strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "[") {
			flowText, endPos, err := p.collectFlowFromRest(rest)
			if err != nil {
				return nil, err
			}
			p.pos = endPos
			val, err := parseFlowValue(flowText, depth+1, ln.num)
			if err != nil {
				return nil, err
			}
			items = append(items, val)
			continue
		}

		if _, _, ok := splitKeyValue(rest); ok {
			// Inline mapping start on sequence bullet (`- key: val`), followed by
			// optional sibling keys indented at itemIndent.
			idx := p.pos
			saved := p.lines[idx]
			p.lines[idx] = yamlLine{num: saved.num, indent: itemIndent, text: rest}
			val, err := p.parseBlockMap(depth+1, itemIndent)
			p.lines[idx] = saved
			if err != nil {
				return nil, err
			}
			items = append(items, val)
			continue
		}

		if isSeqLine(rest) {
			idx := p.pos
			saved := p.lines[idx]
			p.lines[idx] = yamlLine{num: saved.num, indent: itemIndent, text: rest}
			val, err := p.parseBlockSeq(depth+1, itemIndent)
			p.lines[idx] = saved
			if err != nil {
				return nil, err
			}
			items = append(items, val)
			continue
		}

		p.pos++
		val, err := parseScalar(rest, ln.num)
		if err != nil {
			return nil, err
		}
		items = append(items, val)
	}
	return items, nil
}

func (p *yamlParser) parseBlockMap(depth, mapIndent int) (map[string]any, error) {
	out := make(map[string]any)
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent < mapIndent {
			break
		}
		if ln.indent > mapIndent {
			return nil, fmt.Errorf("nt3dcamspec: line %d: unexpected indentation %d in mapping (expected %d)", ln.num, ln.indent, mapIndent)
		}
		if isSeqLine(ln.text) {
			break
		}
		rawKey, rawVal, ok := splitKeyValue(ln.text)
		if !ok {
			return nil, fmt.Errorf("nt3dcamspec: line %d: expected key: value, got %q", ln.num, ln.text)
		}
		key, err := parseMapKey(rawKey, ln.num)
		if err != nil {
			return nil, err
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("nt3dcamspec: line %d: duplicate key %q", ln.num, key)
		}
		if rawVal == "" {
			p.pos++
			if p.pos >= len(p.lines) || p.lines[p.pos].indent < mapIndent ||
				(p.lines[p.pos].indent == mapIndent && !isSeqLine(p.lines[p.pos].text)) {
				out[key] = nil
				continue
			}
			// YAML allows block sequences under a key to share mapIndent (`key:\n- a`)
			// or be indented further (`key:\n  - a`).
			childIndent := p.lines[p.pos].indent
			val, err := p.parseBlock(depth+1, childIndent)
			if err != nil {
				return nil, err
			}
			out[key] = val
			continue
		}
		if strings.HasPrefix(rawVal, "{") || strings.HasPrefix(rawVal, "[") {
			flowText, endPos, err := p.collectFlowFromRest(rawVal)
			if err != nil {
				return nil, err
			}
			p.pos = endPos
			val, err := parseFlowValue(flowText, depth+1, ln.num)
			if err != nil {
				return nil, err
			}
			out[key] = val
			continue
		}
		p.pos++
		val, err := parseScalar(rawVal, ln.num)
		if err != nil {
			return nil, err
		}
		out[key] = val
	}
	return out, nil
}

func (p *yamlParser) collectFlowText() (string, int, error) {
	return p.collectFlowFromRest(p.lines[p.pos].text)
}

func (p *yamlParser) collectFlowFromRest(first string) (string, int, error) {
	var b strings.Builder
	b.WriteString(first)
	idx := p.pos
	startLine := p.lines[idx].num
	for {
		bal, err := flowBalance(b.String(), startLine)
		if err != nil {
			return "", 0, err
		}
		if bal == 0 {
			return b.String(), idx + 1, nil
		}
		idx++
		if idx >= len(p.lines) {
			return "", 0, fmt.Errorf("nt3dcamspec: line %d: unclosed flow collection", startLine)
		}
		b.WriteByte(' ')
		b.WriteString(p.lines[idx].text)
	}
}

func flowBalance(s string, lineNum int) (int, error) {
	var stack []byte
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '{':
			stack = append(stack, '}')
			if len(stack) > maxYAMLDepth {
				return 0, fmt.Errorf("nt3dcamspec: line %d: flow nesting depth exceeded", lineNum)
			}
		case '[':
			stack = append(stack, ']')
			if len(stack) > maxYAMLDepth {
				return 0, fmt.Errorf("nt3dcamspec: line %d: flow nesting depth exceeded", lineNum)
			}
		case '}', ']':
			if len(stack) == 0 || stack[len(stack)-1] != c {
				return 0, fmt.Errorf("nt3dcamspec: line %d: mismatched %q", lineNum, c)
			}
			stack = stack[:len(stack)-1]
		}
	}
	return len(stack), nil
}

func parseFlowValue(s string, depth, lineNum int) (any, error) {
	if depth > maxYAMLDepth {
		return nil, fmt.Errorf("nt3dcamspec: line %d: maximum flow nesting depth exceeded", lineNum)
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if s[0] == '[' {
		if s[len(s)-1] != ']' {
			return nil, fmt.Errorf("nt3dcamspec: line %d: trailing characters after ']'", lineNum)
		}
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return []any{}, nil
		}
		parts, err := splitTopLevel(inner, ',', lineNum)
		if err != nil {
			return nil, err
		}
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		out := make([]any, len(parts))
		for i, part := range parts {
			val, err := parseFlowValue(part, depth+1, lineNum)
			if err != nil {
				return nil, err
			}
			out[i] = val
		}
		return out, nil
	}
	if s[0] == '{' {
		if s[len(s)-1] != '}' {
			return nil, fmt.Errorf("nt3dcamspec: line %d: trailing characters after '}'", lineNum)
		}
		inner := strings.TrimSpace(s[1 : len(s)-1])
		if inner == "" {
			return map[string]any{}, nil
		}
		parts, err := splitTopLevel(inner, ',', lineNum)
		if err != nil {
			return nil, err
		}
		if parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		out := make(map[string]any, len(parts))
		for _, part := range parts {
			rk, rv, ok := splitFlowKeyValue(part)
			if !ok {
				return nil, fmt.Errorf("nt3dcamspec: line %d: invalid flow map entry %q", lineNum, part)
			}
			key, err := parseMapKey(rk, lineNum)
			if err != nil {
				return nil, err
			}
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("nt3dcamspec: line %d: duplicate flow key %q", lineNum, key)
			}
			val, err := parseFlowValue(rv, depth+1, lineNum)
			if err != nil {
				return nil, err
			}
			out[key] = val
		}
		return out, nil
	}
	return parseScalar(s, lineNum)
}

func splitTopLevel(s string, sep byte, lineNum int) ([]string, error) {
	var parts []string
	depth := 0
	inSingle, inDouble := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if inSingle || inDouble || depth != 0 {
		return nil, fmt.Errorf("nt3dcamspec: line %d: unbalanced flow expression", lineNum)
	}
	parts = append(parts, strings.TrimSpace(s[start:]))
	return parts, nil
}

// splitKeyValue splits a block `key: value` line. In block YAML, `:` separates
// key and value when followed by whitespace or end-of-line (or immediately after
// a quoted key).
func splitKeyValue(s string) (key, val string, ok bool) {
	return splitColon(s, false)
}

// splitFlowKeyValue splits `key: value` inside `{...}`, where JSON-style
// `"key":value` without a space after `:` is also valid.
func splitFlowKeyValue(s string) (key, val string, ok bool) {
	return splitColon(s, true)
}

func splitColon(s string, flow bool) (key, val string, ok bool) {
	depth := 0
	inSingle, inDouble := false, false
	justClosedQuote := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inSingle {
			if c == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
				justClosedQuote = true
			}
			continue
		}
		if inDouble {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				inDouble = false
				justClosedQuote = true
			}
			continue
		}
		switch c {
		case '\'':
			inSingle = true
			justClosedQuote = false
		case '"':
			inDouble = true
			justClosedQuote = false
		case '{', '[':
			depth++
			justClosedQuote = false
		case '}', ']':
			depth--
			justClosedQuote = false
		case ':':
			if depth == 0 {
				nextSpaceOrEnd := i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t'
				if nextSpaceOrEnd || (flow && justClosedQuote) {
					k := strings.TrimSpace(s[:i])
					if k == "" {
						return "", "", false
					}
					return k, strings.TrimSpace(s[i+1:]), true
				}
			}
			justClosedQuote = false
		default:
			if c != ' ' && c != '\t' {
				justClosedQuote = false
			}
		}
	}
	return "", "", false
}

func parseMapKey(raw string, lineNum int) (string, error) {
	v, err := parseScalar(raw, lineNum)
	if err != nil {
		return "", err
	}
	ks, ok := v.(string)
	if !ok || ks == "" {
		return "", fmt.Errorf("nt3dcamspec: line %d: mapping key %q must be a non-empty string", lineNum, raw)
	}
	return ks, nil
}

func parseScalar(s string, lineNum int) (any, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" || s == "~" {
		return nil, nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		uq, err := strconv.Unquote(s)
		if err != nil {
			return nil, fmt.Errorf("nt3dcamspec: line %d: invalid double-quoted string %s: %w", lineNum, s, err)
		}
		return uq, nil
	}
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		inner := s[1 : len(s)-1]
		var b strings.Builder
		b.Grow(len(inner))
		for i := 0; i < len(inner); i++ {
			if inner[i] == '\'' {
				if i+1 < len(inner) && inner[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				return nil, fmt.Errorf("nt3dcamspec: line %d: unescaped single quote in scalar %q", lineNum, s)
			}
			b.WriteByte(inner[i])
		}
		return b.String(), nil
	}
	if s == "true" {
		return true, nil
	}
	if s == "false" {
		return false, nil
	}
	if looksNumeric(s) {
		f, err := strconv.ParseFloat(s, 64)
		if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
			return f, nil
		}
	}
	// Reject stray unescaped quotes at start of unquoted scalar.
	if s[0] == '"' || s[0] == '\'' {
		return nil, fmt.Errorf("nt3dcamspec: line %d: improperly quoted scalar %q", lineNum, s)
	}
	return s, nil
}

func looksNumeric(s string) bool {
	if s == "" {
		return false
	}
	i := 0
	if s[i] == '+' || s[i] == '-' {
		i++
	}
	if i >= len(s) {
		return false
	}
	return unicode.IsDigit(rune(s[i])) || (s[i] == '.' && i+1 < len(s) && unicode.IsDigit(rune(s[i+1])))
}
