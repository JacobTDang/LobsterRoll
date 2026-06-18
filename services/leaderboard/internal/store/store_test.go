package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDiff(t *testing.T) {
	tests := []struct {
		name        string
		old, new    []string
		wantAdded   []string
		wantRemoved []string
	}{
		{"both empty", nil, nil, nil, nil},
		{"all new", nil, []string{"b", "a"}, []string{"a", "b"}, nil},
		{"all removed", []string{"a", "b"}, nil, nil, []string{"a", "b"}},
		{"identical", []string{"a", "b"}, []string{"b", "a"}, nil, nil},
		{
			name:        "partial overlap",
			old:         []string{"a", "b", "c"},
			new:         []string{"b", "c", "d"},
			wantAdded:   []string{"d"},
			wantRemoved: []string{"a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := diff(tt.old, tt.new)
			if !reflect.DeepEqual(d.Added, tt.wantAdded) {
				t.Errorf("Added = %v, want %v", d.Added, tt.wantAdded)
			}
			if !reflect.DeepEqual(d.Removed, tt.wantRemoved) {
				t.Errorf("Removed = %v, want %v", d.Removed, tt.wantRemoved)
			}
		})
	}
}

func TestDiff_Empty(t *testing.T) {
	if d := diff(nil, nil); !d.Empty() {
		t.Error("Diff(nil,nil).Empty() = false")
	}
	if d := diff(nil, []string{"a"}); d.Empty() {
		t.Error("non-empty diff reported Empty() = true")
	}
}

func openTemp(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "watch.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustList(t *testing.T, s *Store) []string {
	t.Helper()
	got, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return got
}

func TestStore_ReplaceAndList(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	if got := mustList(t, s); len(got) != 0 {
		t.Fatalf("fresh store List = %v, want empty", got)
	}

	// First population: everything is added.
	d, err := s.Replace(ctx, []string{"0xb", "0xa"})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if !reflect.DeepEqual(d.Added, []string{"0xa", "0xb"}) || d.Removed != nil {
		t.Fatalf("first Replace diff = %+v", d)
	}
	if got := mustList(t, s); !reflect.DeepEqual(got, []string{"0xa", "0xb"}) {
		t.Fatalf("List after first Replace = %v", got)
	}

	// Change: one added, one removed.
	d, err = s.Replace(ctx, []string{"0xb", "0xc"})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if !reflect.DeepEqual(d.Added, []string{"0xc"}) || !reflect.DeepEqual(d.Removed, []string{"0xa"}) {
		t.Fatalf("second Replace diff = %+v", d)
	}
	if got := mustList(t, s); !reflect.DeepEqual(got, []string{"0xb", "0xc"}) {
		t.Fatalf("List after second Replace = %v", got)
	}

	// No-op: empty diff, set unchanged.
	d, err = s.Replace(ctx, []string{"0xc", "0xb"})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if !d.Empty() {
		t.Fatalf("no-op Replace diff = %+v, want empty", d)
	}
}

func TestStore_ReplaceEmptyIsNoOp(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)
	if _, err := s.Replace(ctx, []string{"0xa", "0xb"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// An empty replace must NOT wipe the watchset.
	d, err := s.Replace(ctx, nil)
	if err != nil {
		t.Fatalf("Replace(nil): %v", err)
	}
	if !d.Empty() {
		t.Fatalf("empty replace returned a diff: %+v", d)
	}
	if got := mustList(t, s); len(got) != 2 {
		t.Fatalf("watchset = %v, want [0xa 0xb] preserved", got)
	}
}

func TestStore_LastSync(t *testing.T) {
	ctx := context.Background()
	s := openTemp(t)

	// Absent -> 0, no error.
	got, err := s.LastSync(ctx)
	if err != nil {
		t.Fatalf("LastSync: %v", err)
	}
	if got != 0 {
		t.Fatalf("fresh LastSync = %d, want 0", got)
	}

	// Round-trip, and overwrite.
	for _, want := range []int64{1718500000, 1718599999} {
		if err := s.SetLastSync(ctx, want); err != nil {
			t.Fatalf("SetLastSync(%d): %v", want, err)
		}
		got, err := s.LastSync(ctx)
		if err != nil {
			t.Fatalf("LastSync: %v", err)
		}
		if got != want {
			t.Fatalf("LastSync = %d, want %d", got, want)
		}
	}
}

func TestStore_WALEnabled(t *testing.T) {
	s := openTemp(t)
	var mode string
	if err := s.db.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("PRAGMA journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}

func TestStore_Persists(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "watch.db")

	s1, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s1.Replace(ctx, []string{"0xa", "0xb"}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	s1.Close()

	s2, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s2.Close()
	if got := mustList(t, s2); !reflect.DeepEqual(got, []string{"0xa", "0xb"}) {
		t.Fatalf("after reopen List = %v", got)
	}
}
