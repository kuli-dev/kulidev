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
	res, err := p.Parse(g, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return res.Symbols, res.Imports
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

func docs(t *testing.T, g, src string) (map[string]string, string) {
	t.Helper()
	p, err := NewParser()
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	res, err := p.Parse(g, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, s := range res.Symbols {
		out[s.QualifiedName()] = s.Doc
	}
	return out, res.Doc
}

func TestDocs(t *testing.T) {
	got, file := docs(t, Go, `// Package pay charges customers.
//
// Details nobody reads.
package pay

// Service charges cards
// via the gateway.
type Service struct{}

// unrelated comment

func Bare() {}

// Charge takes money.
func (s *Service) Charge() {}

// ErrDeclined means the card was refused.
var ErrDeclined = errors.New("declined")

const (
	// MaxRetries bounds gateway retries.
	MaxRetries = 3
)

func local() { var notIndexed = 1; _ = notIndexed }
`)
	if file != "Package pay charges customers." {
		t.Errorf("go file doc = %q", file)
	}
	want := map[string]string{"Service": "Service charges cards via the gateway.", "Bare": "", "Service.Charge": "Charge takes money.",
		"ErrDeclined": "ErrDeclined means the card was refused.", "MaxRetries": "MaxRetries bounds gateway retries."}
	if _, ok := got["notIndexed"]; ok {
		t.Error("function-local var must not be indexed")
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("go doc %s = %q, want %q", k, got[k], v)
		}
	}

	got, file = docs(t, Python, `"""Payment module.

More text."""

class Payment:
    """Holds a payment."""
    def refund(self):
        r"""Refund it
        fully."""
        pass

def plain():
    x = 1
`)
	if file != "Payment module." {
		t.Errorf("py file doc = %q", file)
	}
	want = map[string]string{"Payment": "Holds a payment.", "Payment.refund": "Refund it fully.", "plain": ""}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("py doc %s = %q, want %q", k, got[k], v)
		}
	}

	got, _ = docs(t, TypeScript, `/**
 * Loads orders for the dashboard.
 * @param id the customer
 */
export async function loadOrders(id: string) {}

/** Formats money. */
export const fmt = (n: number) => String(n);

class A {
  /** Saves A. */
  save() {}
}
`)
	want = map[string]string{"loadOrders": "Loads orders for the dashboard.", "fmt": "Formats money.", "A.save": "Saves A."}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("ts doc %s = %q, want %q", k, got[k], v)
		}
	}
}

func TestJava(t *testing.T) {
	got, _ := docs(t, Java, `package com.acme.billing;

import java.util.List;
import static com.acme.util.Money.cents;
import com.acme.model.*;

/**
 * Bills customers.
 * @author someone
 */
public class InvoiceService implements Billing {
    /** Creates the service. */
    public InvoiceService() {}

    /** Charges one invoice. */
    public long charge(Invoice inv) { return cents(inv.total()); }

    record Line(String sku, long amount) {}
}

interface Billing { long charge(Invoice inv); }
enum Status { OPEN, PAID }
`)
	want := map[string]string{
		"InvoiceService": "Bills customers.", "InvoiceService.InvoiceService": "Creates the service.",
		"InvoiceService.charge": "Charges one invoice.", "Billing": "", "Status": "",
	}
	for k, v := range want {
		if d, ok := got[k]; !ok || d != v {
			t.Errorf("java %s: doc %q (present %v), want %q — all: %v", k, d, ok, v, got)
		}
	}
	syms, imps := parse(t, Java, `import java.util.List;
import static com.acme.util.Money.cents;
import com.acme.model.*;
class A { void b() {} }
`)
	expect(t, names(syms), map[string]string{"A": "class", "A.b": "method"})
	expectImports(t, specs(imps), "java.util.List", "com.acme.util.Money.cents", "com.acme.model")
}

func TestPHP(t *testing.T) {
	src := `<?php
namespace App\Http\Controllers;

use App\Models\User;
use Illuminate\Http\Request as Req;
require_once __DIR__ . '/helpers.php';
require_once ABSPATH . 'wp-admin/includes/upgrade.php';
include 'legacy/old.php';

/**
 * Handles invoices.
 */
class InvoiceController extends Controller
{
    /** Show one invoice. */
    public function show(Req $r, int $id) { return User::find($id); }
}

trait Auditable { public function audit() {} }
interface Payable { public function pay(); }

/** Format money. */
function money(int $cents): string { return number_format($cents / 100, 2); }
`
	syms, imps := parse(t, PHP, src)
	expect(t, names(syms), map[string]string{
		"InvoiceController": "class", "InvoiceController.show": "method", "Auditable": "class",
		"Auditable.audit": "method", "Payable": "interface", "money": "function",
	})
	expectImports(t, specs(imps), "App\\Models\\User", "Illuminate\\Http\\Request", "/helpers.php", "legacy/old.php", "@wp-admin/includes/upgrade.php")
	got, _ := docs(t, PHP, src)
	if got["InvoiceController"] != "Handles invoices." || got["InvoiceController.show"] != "Show one invoice." || got["money"] != "Format money." {
		t.Errorf("php docs = %v", got)
	}
}
