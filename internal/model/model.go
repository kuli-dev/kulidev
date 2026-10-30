// Package model holds the data types shared between the scanner, parsers and store.
package model

// Symbol is a named definition found in a source file.
type Symbol struct {
	Kind      string // function, method, class, struct, interface, type, enum
	Name      string
	Container string // enclosing class / receiver type, if any
	LineStart int    // 1-based
	LineEnd   int    // 1-based, inclusive
	Doc       string // doc comment / docstring, first paragraph
}

// QualifiedName returns Container.Name, or Name when there is no container.
func (s Symbol) QualifiedName() string {
	if s.Container == "" {
		return s.Name
	}
	return s.Container + "." + s.Name
}

// Import is a raw import specifier as written in the source.
type Import struct {
	Spec string
	Line int // 1-based
	// MaybeSymbol is set for Python "from . import x": x may be a module or a name
	// defined in the package's __init__.py.
	MaybeSymbol bool
}

// FileResult is everything the scanner learned about one file.
type FileResult struct {
	Path    string // slash-separated, relative to the scan root
	Lang    string // display language, "" when unknown
	Size    int64
	Lines   int
	SHA256  string
	Parsed  bool
	Doc     string // file/package/module documentation
	Symbols []Symbol
	Imports []Import
	Err     string
}

// Manifest is a build, dependency or infrastructure file worth knowing about.
type Manifest struct {
	Path string
	Kind string // e.g. "go.mod", "package.json", "Dockerfile", "Kubernetes manifest"
}
