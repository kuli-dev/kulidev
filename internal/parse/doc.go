package parse

import (
	"strings"
	"unicode"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// maxDoc caps stored documentation; the first paragraph is what matters.
const maxDoc = 300

// symbolDoc returns the documentation attached to a definition: the docstring for
// Python, the comment block directly above the declaration otherwise.
func symbolDoc(grammarID string, def *ts.Node, src []byte) string {
	if grammarID == Python {
		return pyDocstring(def.ChildByFieldName("body"), src)
	}
	n := def
	switch {
	case grammarID == Go && (def.Kind() == "type_spec" || def.Kind() == "var_spec" || def.Kind() == "const_spec"):
		// "// Foo does…" sits above `type Foo struct` / `var Foo = …`, i.e. above the
		// declaration, unless the spec is inside a ( … ) group with its own comment.
		if c := leadingComment(n, src); c != "" {
			return c
		}
		for p := def.Parent(); p != nil; p = p.Parent() {
			if k := p.Kind(); k == "type_declaration" || k == "var_declaration" || k == "const_declaration" {
				n = p
				break
			}
		}
	case grammarID != Go:
		// `export const f = () => …` / `export class C` carry the comment on the wrapper.
		for p := n.Parent(); p != nil; p = p.Parent() {
			k := p.Kind()
			if k != "lexical_declaration" && k != "variable_declaration" && k != "export_statement" {
				break
			}
			n = p
		}
	}
	return leadingComment(n, src)
}

// fileDoc returns package/module documentation.
func fileDoc(grammarID string, root *ts.Node, src []byte) string {
	switch grammarID {
	case Python:
		return pyDocstring(root, src)
	case Go:
		for i := uint(0); i < root.NamedChildCount(); i++ {
			if c := root.NamedChild(i); c.Kind() == "package_clause" {
				return leadingComment(c, src)
			}
		}
	default:
		// A comment block at the very top of a JS/TS file, if it is not a directive.
		if c := root.NamedChild(0); c != nil && c.Kind() == "comment" {
			doc := cleanComment(c.Utf8Text(src))
			if !strings.HasPrefix(doc, "eslint") && !strings.HasPrefix(doc, "@ts-") {
				return doc
			}
		}
	}
	return ""
}

// leadingComment joins the contiguous comment lines that end right above n.
func leadingComment(n *ts.Node, src []byte) string {
	var parts []string
	nextRow := n.StartPosition().Row
	for p := n.PrevSibling(); p != nil && strings.HasSuffix(p.Kind(), "comment"); p = p.PrevSibling() { // comment, block_comment, line_comment
		if p.EndPosition().Row+1 < nextRow {
			break // blank line between comment and declaration: not its doc
		}
		parts = append([]string{p.Utf8Text(src)}, parts...)
		nextRow = p.StartPosition().Row
	}
	return cleanComment(strings.Join(parts, "\n"))
}

// pyDocstring returns the docstring at the start of a module or block.
func pyDocstring(block *ts.Node, src []byte) string {
	if block == nil || block.NamedChildCount() == 0 {
		return ""
	}
	first := block.NamedChild(0)
	if first.Kind() != "expression_statement" || first.NamedChildCount() == 0 {
		return ""
	}
	if s := first.NamedChild(0); s.Kind() == "string" {
		return cleanComment(s.Utf8Text(src))
	}
	return ""
}

// cleanComment strips comment syntax and returns the first paragraph, capped.
func cleanComment(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, q := range []string{`"""`, `'''`} {
		if strings.HasPrefix(raw, q) || strings.HasPrefix(strings.TrimLeft(raw, "rRuUbBfF"), q) {
			raw = strings.TrimLeft(raw, "rRuUbBfF")
			raw = strings.TrimSuffix(strings.TrimPrefix(raw, q), q)
		}
	}
	var lines []string
	for _, ln := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(ln)
		t = strings.TrimPrefix(t, "/**")
		t = strings.TrimPrefix(t, "/*")
		t = strings.TrimSuffix(t, "*/")
		t = strings.TrimPrefix(t, "//")
		t = strings.TrimLeft(t, "*#")
		t = strings.TrimSpace(t)
		if t == "" {
			if len(lines) > 0 {
				break // end of first paragraph
			}
			continue
		}
		if strings.HasPrefix(t, "@") && len(lines) > 0 {
			break // JSDoc tags (@param …) after the summary
		}
		lines = append(lines, t)
	}
	doc := strings.Join(lines, " ")
	if len(doc) > maxDoc {
		cut := strings.LastIndexFunc(doc[:maxDoc], unicode.IsSpace)
		if cut < maxDoc/2 {
			cut = maxDoc
		}
		doc = doc[:cut] + "…"
	}
	return doc
}
