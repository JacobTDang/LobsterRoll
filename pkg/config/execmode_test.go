package config

import "testing"

func TestParseExecutionMode(t *testing.T) {
	tests := []struct {
		in      string
		wantErr bool
		mode    ExecutionMode
		ceiling float64
	}{
		{"approval", false, ModeApproval, 0},
		{"auto", false, ModeAuto, 0},
		{"AUTO", false, ModeAuto, 0},
		{"  approval  ", false, ModeApproval, 0},
		{"auto_below:50", false, ModeAutoBelow, 50},
		{"auto_below:12.5", false, ModeAutoBelow, 12.5},
		{"auto_below:0", true, 0, 0},
		{"auto_below:abc", true, 0, 0},
		{"nonsense", true, 0, 0},
		{"", true, 0, 0},
	}
	for _, tc := range tests {
		got, err := ParseExecutionMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseExecutionMode(%q): want error, got nil", tc.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseExecutionMode(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got.Mode != tc.mode {
			t.Errorf("ParseExecutionMode(%q): mode=%v want %v", tc.in, got.Mode, tc.mode)
		}
		if got.CeilingUSD != tc.ceiling {
			t.Errorf("ParseExecutionMode(%q): ceiling=%v want %v", tc.in, got.CeilingUSD, tc.ceiling)
		}
	}
}

func TestRequiresApproval(t *testing.T) {
	tests := []struct {
		policy  ExecutionPolicy
		sizeUSD float64
		want    bool
	}{
		{ExecutionPolicy{Mode: ModeApproval}, 10, true},
		{ExecutionPolicy{Mode: ModeAuto}, 1000, false},
		{ExecutionPolicy{Mode: ModeAutoBelow, CeilingUSD: 50}, 49, false},
		{ExecutionPolicy{Mode: ModeAutoBelow, CeilingUSD: 50}, 50, true},
		{ExecutionPolicy{Mode: ModeAutoBelow, CeilingUSD: 50}, 75, true},
	}
	for _, tc := range tests {
		if got := tc.policy.RequiresApproval(tc.sizeUSD); got != tc.want {
			t.Errorf("RequiresApproval(mode=%v ceil=%v, $%v)=%v want %v",
				tc.policy.Mode, tc.policy.CeilingUSD, tc.sizeUSD, got, tc.want)
		}
	}
}
