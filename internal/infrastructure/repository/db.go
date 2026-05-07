// Package repository contains SQLite-backed repository implementations.
package repository

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type DB struct{ conn *sql.DB }

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path+"?_journal=WAL&_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("sqlite open: %w", err)
	}
	conn.SetMaxOpenConns(1)

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite migrate: %w", err)
	}
	return db, nil
}

func (db *DB) Close() error { return db.conn.Close() }
