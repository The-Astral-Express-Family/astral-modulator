// Package ids 统一生成带前缀的不透明 ID（契约见 api/openapi.yaml 的 Id schema）。
//
// 格式：`<prefix>_<uuidv7>`。前缀只服务可读性，客户端不得解析 ID 内部结构。
// 此文件是前缀唯一事实来源；新增前缀必须同步 api/openapi.yaml 的 Id 模式说明
// 与 TODO.md 的契约登记。
package ids

import (
	"github.com/google/uuid"
)

// 前缀注册表。基础前缀 usr/agt/svc/ws/tsk/msg/evt 见 openapi Id schema；
// 以下为服务端内部资源补充的前缀（变更须经 TODO.md「契约变更登记」）：
//
//	srv req dev ses cred tgp tag doc cfl prs aud apv inv
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
	Invite      Prefix = "inv" // 一次性 workspace 邀请（docs/registration.md）
)

// New 生成 `<prefix>_<uuidv7>`。
func New(p Prefix) string {
	return string(p) + "_" + uuid.Must(uuid.NewV7()).String()
}
