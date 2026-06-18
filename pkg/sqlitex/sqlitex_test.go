package sqlitex

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpen_AppliesPragmas(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	var journal string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journal != "wal" {
		t.Errorf("journal_mode = %q, want wal", journal)
	}
	var busy int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busy != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", busy)
	}
	// Single-writer: the pool must cap at one open connection.
	if got := db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}
	// Usable for DDL/DML.
	if _, err := db.ExecContext(context.Background(), `CREATE TABLE x (a INTEGER)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
}

func TestOpen_BadPath(t *testing.T) {
	if _, err := Open(context.Background(), "/nonexistent-dir/sub/t.db"); err == nil {
		t.Fatal("expected error opening under a nonexistent directory")
	}
}
