// Package sqlite contains SQLite-backed repository implementations.
package sqlite

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ conn *sql.DB }

func Open(path string) (*DB, error) {
	return open(path, migrations, time.Now)
}

func open(path string, migs []migration, now func() time.Time) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	conn.SetMaxOpenConns(1)

	if err := migrate(conn, path, migs, now); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite migrate: %w", err)
	}
	return &DB{conn: conn}, nil
}

func (db *DB) Close() error { return db.conn.Close() }
