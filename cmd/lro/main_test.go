package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadInvoicesFile(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "invoices.txt")
	content := "\n# comment\nlnbc1invoicea\n  \nlnbc1invoiceb\n"
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	inv, err := loadInvoicesFile(p)
	if err != nil {
		t.Fatalf("loadInvoicesFile: %v", err)
	}
	if len(inv) != 2 || inv[0] != "lnbc1invoicea" || inv[1] != "lnbc1invoiceb" {
		t.Fatalf("unexpected invoices: %#v", inv)
	}
}
