// Package ids 统一生成带前缀的不透明 ID（docs/protocol.md §3）。
//
// 格式：`<prefix>_<uuidv7>`。前缀只服务可读性，客户端不得解析 ID 内部结构。
// 此文件是前缀唯一事实来源；新增前缀必须同步 api/openapi.yaml 的 Id 模式说明
// 与 TODO.md 的契约登记。
package ids

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

// 前缀注册表。protocol.md §3 已列 usr/agt/svc/ws/tsk/msg/evt，
// 以下为服务端内部资源补充的前缀（已在 TODO.md「新增契约登记」处登记）：
//
//	srv req dev ses cred tgp tag doc cfl prs aud obx apv
type Prefix string

const (
	User        Prefix = "usr"  // Human actor
	Agent       Prefix = "agt"  // LLM agent actor
	Service     Prefix = "svc"  // service/integration actor
	Server      Prefix = "srv"  // server 实例身份（.well-known 返回）
	Request     Prefix = "req"  // 请求链路 ID
	Device      Prefix = "dev"  // device flow authorization
	Session     Prefix = "ses"  // human web/cli session
	Credential  Prefix = "cred" // agent/service credential
	Workspace   Prefix = "ws"
	Task        Prefix = "tsk"
	TagProposal Prefix = "tgp"
	Tag         Prefix = "tag"
	Document    Prefix = "doc"
	Conflict    Prefix = "cfl" // document conflict artifact
	Presence    Prefix = "prs"
	Message     Prefix = "msg"
	Event       Prefix = "evt"
	Audit       Prefix = "aud"
	Approval    Prefix = "apv" // human approval request（architecture §22）
)

// 前缀长度 2~3（现存最短为 ws_）；UUIDv7 小写十六进制。
var valid = regexp.MustCompile(`^[a-z]{2,3}_[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// New 生成 `<prefix>_<uuidv7>`。
func New(p Prefix) string {
	return string(p) + "_" + uuid.Must(uuid.NewV7()).String()
}

// Validate 校验 ID 形状（前缀 + UUIDv7）。用于入参快速失败，避免脏数据入库。
func Validate(id string) bool { return valid.MatchString(strings.ToLower(id)) }

// MustValidate 同 Validate，但返回带前缀的错误信息便于排查。
func MustValidate(id string, expect Prefix) error {
	if !strings.HasPrefix(id, string(expect)+"_") || !Validate(id) {
		return fmt.Errorf("invalid %s id: %q", expect, id)
	}
	return nil
}
