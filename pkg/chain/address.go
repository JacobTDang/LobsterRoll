package chain

import "strings"

// NormalizeAddress validates an Ethereum-style address and returns its
// canonical lowercase, 0x-prefixed form. It trims surrounding whitespace and
// reports ok=false for anything that is not exactly "0x" followed by 40 hex
// digits. Lowercasing makes addresses comparable regardless of EIP-55 casing.
func NormalizeAddress(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 42 || s[0] != '0' || (s[1] != 'x' && s[1] != 'X') {
		return "", false
	}
	for _, c := range s[2:] {
		if !isHexDigit(c) {
			return "", false
		}
	}
	return strings.ToLower(s), true
}

// NormalizeAddresses normalizes each input address, dropping any that are
// invalid and de-duplicating (case-insensitively) while preserving first-seen
// order. Returns nil for empty input.
func NormalizeAddresses(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	var out []string
	for _, raw := range in {
		addr, ok := NormalizeAddress(raw)
		if !ok {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}

func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
