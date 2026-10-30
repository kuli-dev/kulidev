package parse

import (
	"testing"

	"github.com/kuli-dev/kulidev/internal/model"
)

func parse(t *testing.T, g, src string) ([]model.Symbol, []model.Import) {
	t.Helper()
	p, err := NewParser()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	syms, imps, err := p.Parse(g, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return syms, imps
}

func names(syms []model.Symbol) map[string]string {
	out := map[string]string{}
	for _, s := range syms {
		out[s.QualifiedName()] = s.Kind
	}
	return out
}

func specs(imps []model.Import) map[string]bool {
	out := map[string]bool{}
	for _, i := range imps {
		out[i.Spec] = true
	}
	return out
}

func expect(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	for k, v := range want {
		if got[k] != v {
			t.Errorf("symbol %q: got kind %q, want %q (all: %v)", k, got[k], v, got)
		}
	}
}

func expectImports(t *testing.T, got map[string]bool, want ...string) {
	t.Helper()
	for _, w := range want {
		if !got[w] {
			t.Errorf("missing import %q (all: %v)", w, got)
		}
	}
}

func TestGo(t *testing.T) {
	syms, imps := parse(t, Go, `package x
import (
	"fmt"
	str "strings"
)
type Server[T any] struct{}
type Store interface{ Get() }
type ID string
func New() *Server[int] { return nil }
func (s *Server[T]) Run() { fmt.Println(str.ToUpper("x")) }
`)
	expect(t, names(syms), map[string]string{
		"Server": "struct", "Store": "interface", "ID": "type",
		"New": "function", "Server.Run": "method",
	})
	expectImports(t, specs(imps), "fmt", "strings")
}

func TestPython(t *testing.T) {
	syms, imps := parse(t, Python, `import os, json as j
from .models import User
from . import prompts, utils as u
from app.db import session

class PaymentService:
    def charge(self, amount):
        def helper(): pass
        return helper()

def main():
    pass
`)
	expect(t, names(syms), map[string]string{
		"PaymentService": "class", "PaymentService.charge": "method",
		"PaymentService.helper": "method", "main": "function",
	})
	expectImports(t, specs(imps), "os", "json", ".models", "app.db", ".prompts", ".utils")
}

func TestTypeScript(t *testing.T) {
	syms, imps := parse(t, TypeScript, `import { a } from "./a";
import type { B } from '@scope/pkg/sub';
export * from "./reexport";
const fs = require("fs");
interface Props { x: number }
type Id = string;
enum Color { Red }
export abstract class Base {}
export class UserService extends Base {
  async find(id: Id) { return id; }
}
export function handler() {}
export const fetchUser = async (id: string) => id;
`)
	expect(t, names(syms), map[string]string{
		"Props": "interface", "Id": "type", "Color": "enum", "Base": "class",
		"UserService": "class", "UserService.find": "method",
		"handler": "function", "fetchUser": "function",
	})
	expectImports(t, specs(imps), "./a", "@scope/pkg/sub", "./reexport", "fs")
}

func TestTSXAndJavaScript(t *testing.T) {
	syms, _ := parse(t, TSX, `export function App() { return <div/>; }`)
	expect(t, names(syms), map[string]string{"App": "function"})

	syms, imps := parse(t, JavaScript, `const x = require('express'); console.log('not-an-import');
class A { b() {} }
`)
	expect(t, names(syms), map[string]string{"A": "class", "A.b": "method"})
	got := specs(imps)
	expectImports(t, got, "express")
	if got["not-an-import"] {
		t.Errorf("non-require call captured as import: %v", got)
	}
}
