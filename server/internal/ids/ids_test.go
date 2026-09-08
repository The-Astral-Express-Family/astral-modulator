package ids

import (
	"regexp"
	"strings"
	"testing"
)

// idShape 是公网 ID 形状契约（api/openapi.yaml Id schema）的本地快照，
// 用于钉住 New 的输出；服务端不在请求路径上校验 ID（客户端只透传）。
var idShape = regexp.MustCompile(`^[a-z]{2,3}_[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func validShape(id string) bool { return idShape.MatchString(strings.ToLower(id)) }

func TestNewShape(t *testing.T) {
	for _, p := range []Prefix{User, Agent, Workspace, Task, TagProposal, Event, Message} {
		id := New(p)
		if !strings.HasPrefix(id, string(p)+"_") {
			t.Errorf("%s: bad prefix in %q", p, id)
		}
		if !validShape(id) {
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

func TestShapeContract(t *testing.T) {
	for _, bad := range []string{"", "tsk", "tsk_", "NOT_A_ID", "tsk_not-a-uuid", "../etc/passwd"} {
		if validShape(bad) {
			t.Errorf("shape %q must not match id contract", bad)
		}
	}
}
