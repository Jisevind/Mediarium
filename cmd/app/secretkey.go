package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/rdborg/mediarium/internal/crypto"
)

// loadSecretKey loads the encryption key. A missing key file is only replaced
// by a new one when the database holds nothing that was encrypted with the old
// one: otherwise a new key would be written, nothing could be decrypted and
// every later start would fail the same way.
func loadSecretKey(db *sql.DB, path string) (*crypto.Box, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		used, err := databaseHoldsSecrets(db)
		if err != nil {
			return nil, fmt.Errorf("check the database for stored secrets: %w", err)
		}
		if used {
			return nil, errors.New("app.db has stored secrets but secret.key is missing: put your old secret.key back in /config")
		}
	}
	return crypto.LoadOrCreateKey(path)
}

// databaseHoldsSecrets reports whether the database already has encrypted
// settings, indexers or download clients, or any user.
func databaseHoldsSecrets(db *sql.DB) (bool, error) {
	for _, q := range []string{
		`SELECT EXISTS(SELECT 1 FROM settings WHERE encrypted = 1)`,
		`SELECT EXISTS(SELECT 1 FROM users)`,
		`SELECT EXISTS(SELECT 1 FROM indexers)`,
		`SELECT EXISTS(SELECT 1 FROM download_clients)`,
	} {
		var found bool
		if err := db.QueryRow(q).Scan(&found); err != nil {
			return false, err
		}
		if found {
			return true, nil
		}
	}
	return false, nil
}
