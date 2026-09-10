// Package db opens the SQLite database and applies the schema.
package db

import (
	"database/sql"
	_ "embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

const Version = 1

// Open opens (creating if needed) the database at path and ensures the schema
// is present. Use ":memory:" for tests.
func Open(path string) (*sql.DB, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	}
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if path == ":memory:" {
		// One connection, or each pool connection gets its own empty database.
		d.SetMaxOpenConns(1)
		if _, err := d.Exec("PRAGMA foreign_keys = ON"); err != nil {
			return nil, err
		}
	}
	if err := migrate(d); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func migrate(d *sql.DB) error {
	if _, err := d.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	var v sql.NullInt64
	if err := d.QueryRow("SELECT MAX(version) FROM schema_version").Scan(&v); err != nil {
		return err
	}
	if !v.Valid {
		_, err := d.Exec("INSERT INTO schema_version(version) VALUES (?)", Version)
		return err
	}
	if v.Int64 > Version {
		return fmt.Errorf("database schema version %d is newer than this binary (%d)", v.Int64, Version)
	}
	return nil
}
