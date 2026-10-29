package postgres_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ledger_entries is read-only until a later migration drops it; no code
// outside migrations and tests may name it.
func TestNoCodeUsesLedgerEntries(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	var offenders []string
	scanned := 0
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			scanned++
			if strings.Contains(string(b), "ledger_entries") {
				offenders = append(offenders, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if scanned < 50 {
		t.Fatalf("scanned only %d files; is the root right?", scanned)
	}
	if len(offenders) > 0 {
		t.Fatalf("code still uses ledger_entries: %v", offenders)
	}
}
