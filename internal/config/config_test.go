package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadKeepsExplicitDatabaseURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("DATABASE_URL=postgres://from-file/example?sslmode=require\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := "postgres://from-environment/example?sslmode=disable"
	t.Setenv("DATABASE_URL", want)

	if got := Load(path).DatabaseURL; got != want {
		t.Fatalf("DatabaseURL = %q, want explicit environment value %q", got, want)
	}
}
