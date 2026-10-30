// Package parse extracts symbols and imports from source files with Tree-sitter.
package parse

import (
	"fmt"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjs "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tspy "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsts "github.com/tree-sitter/tree-sitter-typescript/bindings/go"

	"github.com/kuli-dev/kulidev/internal/model"
)

// Grammar identifiers accepted by Parser.Parse.
const (
	Go         = "go"
	Python     = "python"
	JavaScript = "javascript"
	TypeScript = "typescript"
	TSX        = "tsx"
)

type grammar struct {
	lang  *ts.Language
	query string
}

var grammars = map[string]grammar{
	Go:         {ts.NewLanguage(tsgo.Language()), goQuery},
	Python:     {ts.NewLanguage(tspy.Language()), pythonQuery},
	JavaScript: {ts.NewLanguage(tsjs.Language()), javascriptQuery},
	TypeScript: {ts.NewLanguage(tsts.LanguageTypescript()), typescriptQuery},
	TSX:        {ts.NewLanguage(tsts.LanguageTSX()), typescriptQuery},
}

// Supported reports whether a grammar exists for the given identifier.
func Supported(g string) bool {
	_, ok := grammars[g]
	return ok
}

// Parser wraps per-goroutine Tree-sitter state. It is not safe for concurrent use;
// create one per worker.
type Parser struct {
	p       *ts.Parser
	cursor  *ts.QueryCursor
	queries map[string]*ts.Query
}

// NewParser compiles every grammar's query up front so errors surface early.
func NewParser() (*Parser, error) {
	pr := &Parser{p: ts.NewParser(), cursor: ts.NewQueryCursor(), queries: map[string]*ts.Query{}}
	for id, g := range grammars {
		q, qerr := ts.NewQuery(g.lang, g.query)
		if qerr != nil {
			pr.Close()
			return nil, fmt.Errorf("compile %s query: %s", id, qerr.Error())
		}
		pr.queries[id] = q
	}
	return pr, nil
}

// Close releases the underlying C resources.
func (pr *Parser) Close() {
	for _, q := range pr.queries {
		q.Close()
	}
	pr.cursor.Close()
	pr.p.Close()
}

// Result is what one file contains.
type Result struct {
	Symbols []model.Symbol
	Imports []model.Import
	Doc     string // file/package/module documentation, first paragraph
}

// Parse returns the symbols, imports and documentation in src.
func (pr *Parser) Parse(grammarID string, src []byte) (*Result, error) {
	g, ok := grammars[grammarID]
	if !ok {
		return nil, fmt.Errorf("unsupported grammar %q", grammarID)
	}
	if err := pr.p.SetLanguage(g.lang); err != nil {
		return nil, err
	}
	tree := pr.p.Parse(src, nil)
	if tree == nil {
		return nil, fmt.Errorf("parse failed")
	}
	defer tree.Close()

	q := pr.queries[grammarID]
	names := q.CaptureNames()
	var syms []model.Symbol
	var imps []model.Import

	matches := pr.cursor.Matches(q, tree.RootNode(), src)
	for m := matches.Next(); m != nil; m = matches.Next() {
		var def, importNode *ts.Node
		var kind, name, recv, typebody, fn, imp string
		impLine := 0
		for i := range m.Captures {
			c := &m.Captures[i]
			switch cn := names[c.Index]; {
			case strings.HasPrefix(cn, "def."):
				n := c.Node
				def, kind = &n, strings.TrimPrefix(cn, "def.")
			case cn == "name":
				name = c.Node.Utf8Text(src)
			case cn == "recv":
				recv = c.Node.Utf8Text(src)
			case cn == "typebody":
				typebody = c.Node.Kind()
			case cn == "fn":
				fn = c.Node.Utf8Text(src)
			case cn == "import":
				n := c.Node
				imp, importNode = n.Utf8Text(src), &n
				impLine = int(c.Node.StartPosition().Row) + 1
			}
		}

		if imp != "" {
			if fn != "" && fn != "require" {
				continue
			}
			spec := unquote(imp)
			if grammarID == Python && strings.Trim(spec, ".") == "" {
				// "from . import a, b": a and b are sibling modules (or names in __init__).
				if subs := pyFromNames(importNode, src); len(subs) > 0 {
					for _, sub := range subs {
						imps = append(imps, model.Import{Spec: spec + sub, Line: impLine, MaybeSymbol: true})
					}
					continue
				}
			}
			if spec != "" {
				imps = append(imps, model.Import{Spec: spec, Line: impLine})
			}
			continue
		}
		if def == nil || name == "" {
			continue
		}

		s := model.Symbol{
			Kind:      kind,
			Name:      name,
			LineStart: int(def.StartPosition().Row) + 1,
			LineEnd:   int(def.EndPosition().Row) + 1,
			Doc:       symbolDoc(grammarID, def, src),
		}
		switch {
		case grammarID == Go && kind == "method":
			s.Container = goReceiverType(recv)
		case grammarID == Go && kind == "type":
			switch typebody {
			case "struct_type":
				s.Kind = "struct"
			case "interface_type":
				s.Kind = "interface"
			}
		default:
			s.Container = enclosingClass(def, src)
			if s.Container != "" && s.Kind == "function" && grammarID == Python {
				s.Kind = "method"
			}
		}
		syms = append(syms, s)
	}
	return &Result{Symbols: syms, Imports: imps, Doc: fileDoc(grammarID, tree.RootNode(), src)}, nil
}

var classKinds = map[string]bool{
	"class_definition":           true, // python
	"class_declaration":          true, // js/ts
	"abstract_class_declaration": true, // ts
	"class":                      true, // js/ts class expression
}

// enclosingClass returns the name of the nearest class containing n, if any.
func enclosingClass(n *ts.Node, src []byte) string {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if classKinds[p.Kind()] {
			if nm := p.ChildByFieldName("name"); nm != nil {
				return nm.Utf8Text(src)
			}
			return ""
		}
	}
	return ""
}

// goReceiverType turns "(s *Server[T])" into "Server".
func goReceiverType(recv string) string {
	r := strings.Trim(strings.TrimSpace(recv), "()")
	if f := strings.Fields(r); len(f) > 0 {
		r = f[len(f)-1]
	}
	r = strings.TrimLeft(r, "*")
	if i := strings.IndexByte(r, '['); i >= 0 {
		r = r[:i]
	}
	return r
}

func unquote(s string) string {
	return strings.Trim(strings.TrimSpace(s), "\"'`")
}

// pyFromNames returns the imported names of the import_from_statement containing n.
func pyFromNames(n *ts.Node, src []byte) []string {
	stmt := n.Parent()
	if stmt == nil || stmt.Kind() != "import_from_statement" {
		return nil
	}
	var out []string
	for _, c := range stmt.ChildrenByFieldName("name", stmt.Walk()) {
		if c.Kind() == "aliased_import" {
			if nm := c.ChildByFieldName("name"); nm != nil {
				out = append(out, nm.Utf8Text(src))
			}
			continue
		}
		out = append(out, c.Utf8Text(src))
	}
	return out
}
