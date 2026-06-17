package chain

import (
	"reflect"
	"testing"
)

func TestNormalizeAddress(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"already lower", "0xf0318c32136c2db7fec88b84869aee6a1106c80c", "0xf0318c32136c2db7fec88b84869aee6a1106c80c", true},
		{"checksummed mixed case", "0xE111180000d2663C0091e4f400237545B87B996B", "0xe111180000d2663c0091e4f400237545b87b996b", true},
		{"surrounding whitespace", "  0xF0318C32136C2DB7FEC88B84869AEE6A1106C80C\n", "0xf0318c32136c2db7fec88b84869aee6a1106c80c", true},
		{"empty", "", "", false},
		{"missing 0x prefix", "f0318c32136c2db7fec88b84869aee6a1106c80c", "", false},
		{"too short", "0xf0318c32136c2db7fec88b84869aee6a1106c80", "", false},
		{"too long", "0xf0318c32136c2db7fec88b84869aee6a1106c80cad", "", false},
		{"non-hex char", "0xf0318c32136c2db7fec88b84869aee6a1106c80g", "", false},
		{"just prefix", "0x", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NormalizeAddress(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("NormalizeAddress(%q) = (%q, %v), want (%q, %v)", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestNormalizeAddresses(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, nil},
		{
			name: "dedup preserves first-seen order, case-insensitive",
			in: []string{
				"0xE111180000d2663C0091e4f400237545B87B996B",
				"0xf0318c32136c2db7fec88b84869aee6a1106c80c",
				"0xe111180000d2663c0091e4f400237545b87b996b", // dup of #1
			},
			want: []string{
				"0xe111180000d2663c0091e4f400237545b87b996b",
				"0xf0318c32136c2db7fec88b84869aee6a1106c80c",
			},
		},
		{
			name: "drops invalid entries",
			in:   []string{"not-an-address", "0xf0318c32136c2db7fec88b84869aee6a1106c80c", ""},
			want: []string{"0xf0318c32136c2db7fec88b84869aee6a1106c80c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeAddresses(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("NormalizeAddresses(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
