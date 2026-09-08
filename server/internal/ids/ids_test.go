package ids

import (
	"strings"
	"testing"
)

func TestNewShape(t *testing.T) {
	for _, p := range []Prefix{User, Agent, Workspace, Task, TagProposal, Event, Message} {
		id := New(p)
		if !strings.HasPrefix(id, string(p)+"_") {
			t.Errorf("%s: bad prefix in %q", p, id)
		}
		if !Validate(id) {
			t.Errorf("%s: invalid id shape %q", p, id)
		}
	}
}

func TestNewUnique(t *testing.T) {
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		id := New(Task)
		if seen[id] {
			t.Fatalf("duplicate id generated: %s", id)
		}
		seen[id] = true
	}
}

func TestValidateRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "tsk", "tsk_", "NOT_A_ID", "tsk_not-a-uuid", "../etc/passwd"} {
		if Validate(bad) {
			t.Errorf("Validate(%q) = true, want false", bad)
		}
	}
}
