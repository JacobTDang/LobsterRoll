package bus

import "testing"

func TestAllSubjectsUniqueAndNonEmpty(t *testing.T) {
	subs := AllSubjects()
	if len(subs) == 0 {
		t.Fatal("AllSubjects returned no subjects")
	}
	seen := make(map[string]bool, len(subs))
	for _, s := range subs {
		if s == "" {
			t.Error("found empty subject name")
		}
		if seen[s] {
			t.Errorf("duplicate subject %q", s)
		}
		seen[s] = true
	}
}
