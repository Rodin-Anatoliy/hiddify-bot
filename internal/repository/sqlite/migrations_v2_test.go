package sqlite

import (
	"path/filepath"
	"strings"
	"testing"
)

// Migration 2 on top of a v1 DB: existing rows stay intact, invites appears,
// and a v1 backup is taken.
func TestMigrate_V2AddsInvitesOnTopOfV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bot.db")
	db, err := open(path, migrations[:1], fixedNow)
	if err != nil {
		t.Fatalf("open v1: %v", err)
	}
	for _, q := range []string{
		`INSERT INTO users (telegram_id, hiddify_uuid, username, can_message, link_source) VALUES (1, 'u-1', 'alice', 1, 'auto')`,
		`INSERT INTO ticket_messages (telegram_id, direction, text) VALUES (1, 'in', 'hello')`,
		`INSERT INTO admin_sessions (message_id, target_tg_id, expires_at) VALUES (10, 1, '2030-01-01 00:00:00')`,
	} {
		if _, err := db.conn.Exec(q); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	if n := count(t, db.conn, `SELECT COUNT(*) FROM sqlite_master WHERE name='invites'`); n != 0 {
		t.Fatal("v1 DB must not have invites")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	db, err = open(path, migrations, fixedNow)
	if err != nil {
		t.Fatalf("open v2: %v", err)
	}
	defer db.Close()
	if v := count(t, db.conn, `SELECT MAX(version) FROM schema_version`); v != 2 {
		t.Fatalf("version = %d, want 2", v)
	}
	if n := count(t, db.conn, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='invites'`); n != 1 {
		t.Fatal("invites table missing")
	}
	for tbl, want := range map[string]int{"users": 1, "ticket_messages": 1, "admin_sessions": 1} {
		if got := count(t, db.conn, `SELECT COUNT(*) FROM `+tbl); got != want {
			t.Fatalf("%s rows = %d, want %d", tbl, got, want)
		}
	}
	if n := count(t, db.conn, `SELECT COUNT(*) FROM users WHERE telegram_id = 1 AND hiddify_uuid = 'u-1' AND username = 'alice' AND can_message = 1 AND link_source = 'auto'`); n != 1 {
		t.Fatal("user row changed by migration 2")
	}
	if b := backups(t, path); len(b) != 1 || !strings.Contains(b[0], ".bak-v1-") {
		t.Fatalf("backups = %v, want one .bak-v1-*", b)
	}

	// code_hash is unique.
	ins := `INSERT INTO invites (code_hash, kind, days, created_at, expires_at) VALUES ('h', 'own', 7, 1, 2)`
	if _, err := db.conn.Exec(ins); err != nil {
		t.Fatalf("insert invite: %v", err)
	}
	if _, err := db.conn.Exec(ins); err == nil {
		t.Fatal("duplicate code_hash must be rejected")
	}
}
