package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rdborg/mediarium/internal/store"
)

func TestLoadSecretKeyCreatesAKeyForAnEmptyDatabase(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keyPath := filepath.Join(dir, "secret.key")
	if _, err := loadSecretKey(db, keyPath); err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("key file was not written: %v", err)
	}
}

func TestLoadSecretKeyRefusesToReplaceAMissingKey(t *testing.T) {
	for name, stmt := range map[string]string{
		"encrypted setting": `INSERT INTO settings (key, value, encrypted) VALUES ('tmdb.api_key', 'abc', 1)`,
		"user":              `INSERT INTO users (username, password_hash) VALUES ('ryan', 'x')`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := store.Open(filepath.Join(dir, "app.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err := db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
			keyPath := filepath.Join(dir, "secret.key")
			_, err = loadSecretKey(db, keyPath)
			if err == nil || !strings.Contains(err.Error(), "put your old secret.key back") {
				t.Fatalf("want the missing-key message, got %v", err)
			}
			if _, statErr := os.Stat(keyPath); statErr == nil {
				t.Fatal("a new key was written anyway")
			}
		})
	}
}
