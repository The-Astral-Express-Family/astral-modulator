// Package ratelimit 实现进程内限流（TODO.md S5 / docs/protocol.md §6）：
// token bucket（仅标准库）+ map[key]*bucket + mutex + 惰性清理，时钟可注入。
//
// 桶配置（TODO.md S5-2，env 可覆盖，见 config.RateLimitConfig）：
//   - 敏感桶 SensitivePerMin/min/IP：login、register、token/refresh、
//     POST+GET /auth/device/authorizations（user_code 防枚举）、approve/deny；
//   - 轮询桶 PollPerMin/min/IP：POST /auth/device/authorizations/{code}/token
//     （CLI interval=3s 轮询必须容纳，故独立于敏感桶）；
//   - 通用桶 APIPerMin/min/actor：其余 /api/v1；
//   - SSE 桶 SSEPerMin/min/actor：GET /workspaces/{id}/events 连接建立
//     （独立桶，重连风暴不占通用桶）。
//
// 挂载语义（app.router 两段式，单个请求只进一个桶、不双计）：
//   - Public 挂 /api/v1 顶层（Authenticate 之前）——只对公共 auth 面记账：
//     敏感/轮询桶按客户端 IP；logout 与 meta/capabilities 按 IP 记入通用桶；
//     其余路径（受保护端点）放行，交由 Private 段记账。
//   - Private 挂 priv 组（Authenticate 之后）——key 才能取到 actor_id：
//     设备审批三端点（GET authorizations / approve / deny）入敏感桶（仍按
//     来源 IP——防枚举面向来源而非身份，换号不可绕过）；events 入 SSE 桶；
//     其余入通用桶，key=actor_id（未认证请求到不了这里：Authenticate 先 401）。
//
// 超限：429 RATE_LIMITED（httpx 错误 envelope，retryable=true）+
// Retry-After 秒头（ceil 到下一枚令牌恢复时刻，至少 1）。
//
// 局限（单实例 MVP）：进程内状态不跨实例共享，多实例部署见
// architecture §19；届时换集中式计数器，桶形状与本包语义不变。
package ratelimit

import (
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/httpx"
)

const (
	// idleTTL 是惰性清理阈值：key 最后访问（含被拒绝）超过该时长后删除。
	idleTTL = 10 * time.Minute
	// sweepEvery 节流清理扫描频率（每分钟至多全量扫一次，避免 O(n)/请求）。
	sweepEvery = time.Minute
)

// Limiter 是一类桶的存储：每 key 一个 token bucket，容量=perMin，
// 以 perMin/60 每秒匀速回填。并发安全；now 可注入（测试推进窗口）。
type Limiter struct {
	perMin int
	now    func() time.Time

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep time.Time
}

type bucket struct {
	tokens     float64
	lastRefill time.Time
	lastUsed   time.Time
}

// NewLimiter 构造一类桶。perMin<=0 表示该桶禁用（Allow 恒放行），
// 供测试装配与显式 env 关闭（ASTRAL_RATELIMIT_*=0）。
func NewLimiter(perMin int, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		perMin:    perMin,
		now:       now,
		buckets:   make(map[string]*bucket),
		lastSweep: now(),
	}
}

// Allow 消费一枚令牌。返回是否放行，以及超限时到下一枚令牌恢复的
// 建议等待时长（供 Retry-After）。nil 接收者或禁用桶恒放行。
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	if l == nil || l.perMin <= 0 {
		return true, 0
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)

	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(l.perMin), lastRefill: now}
		l.buckets[key] = b
	} else {
		b.refill(now, l.perMin)
	}
	// lastUsed 每次访问（含拒绝）都推进：活跃 key 不被清理回收。
	b.lastUsed = now

	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	need := 1 - b.tokens
	wait := time.Duration(need * 60 / float64(l.perMin) * float64(time.Second))
	return false, wait
}

// Len 返回当前 key 数（观测惰性清理的测试辅助）。
func (l *Limiter) Len() int {
	if l == nil {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

func (b *bucket) refill(now time.Time, perMin int) {
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed <= 0 {
		return // 时钟未推进/回拨：不回填也不后移基准
	}
	b.tokens += elapsed * float64(perMin) / 60
	if max := float64(perMin); b.tokens > max {
		b.tokens = max
	}
	b.lastRefill = now
}

// sweepLocked 惰性清理：距上次扫描超过 sweepEvery 时全量删除空闲超
// idleTTL 的 key。频率由请求驱动——无流量时不扫（进程内状态本就无害）。
func (l *Limiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.lastSweep = now
	for k, b := range l.buckets {
		if now.Sub(b.lastUsed) > idleTTL {
			delete(l.buckets, k)
		}
	}
}

// ClientIP 解析限流 key 的客户端 IP。trustedProxy=true 时采信
// X-Forwarded-For 首跳（链上最左侧=客户端自报，仅在反代已清洗 XFF 的
// 部署里可信，故由 ASTRAL_TRUSTED_PROXY 显式开关，默认 false）；
// 否则取直连 RemoteAddr 的 host（去端口，兼容 IPv6 方括号形式）。
func ClientIP(r *http.Request, trustedProxy bool) string {
	if trustedProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// Config 是四类桶的每分钟令牌数与开关（app 层从 config.Config 适配）。
// 每项 <=0 表示禁用对应桶。Now 可注入时钟（缺省 time.Now），测试推进
// 窗口用；生产装配留空。
type Config struct {
	SensitivePerMin int
	PollPerMin      int
	APIPerMin       int
	SSEPerMin       int
	TrustedProxy    bool
	Now             func() time.Time
}

// ActorKeyFunc 从请求提取认证主体标识（actor_id）。挂载层用
// auth.PrincipalFrom 接线，避免本包反向依赖 auth；返回空串时中间件
// 回退客户端 IP（防御路径：Authenticate 成功必注入 principal）。
type ActorKeyFunc func(*http.Request) string

// Middleware 聚合四类桶并按 (method, path) 分类记账。挂载形态与语义
// 见包注释（Public=鉴权前公共面 / Private=鉴权后受保护面，两段互斥分工）。
type Middleware struct {
	cfg       Config
	actorFrom ActorKeyFunc

	sensitive *Limiter
	poll      *Limiter
	api       *Limiter
	sse       *Limiter
}

// New 构造限流中间件。actorFrom 仅 Private 段使用，可为 nil
// （等价于恒回退 IP key）。
func New(cfg Config, actorFrom ActorKeyFunc) *Middleware {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &Middleware{
		cfg:       cfg,
		actorFrom: actorFrom,
		sensitive: NewLimiter(cfg.SensitivePerMin, now),
		poll:      NewLimiter(cfg.PollPerMin, now),
		api:       NewLimiter(cfg.APIPerMin, now),
		sse:       NewLimiter(cfg.SSEPerMin, now),
	}
}

type bucketClass int

const (
	// classNone 放行不记账：受保护路径在 Public 段不消费（Private 段记）。
	classNone bucketClass = iota
	classSensitive
	classPoll
	classAPI
	classSSE
)

// Public 挂 /api/v1 顶层（Authenticate 之前）：只对公共 auth 面与
// meta/capabilities 记账（key=客户端 IP）；受保护路径放行，由 Private
// 在鉴权后按 actor 记账——避免同一请求双计。
func (m *Middleware) Public(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if class := classifyPublic(r.Method, r.URL.Path); class != classNone {
			if ok, wait := m.take(class, m.key(r, class)); !ok {
				deny(w, r, wait)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Private 挂 priv 组（Authenticate 之后）：设备审批三端点入敏感桶
// （key=IP，防枚举按来源）；events 入 SSE 桶；其余入通用桶。通用/SSE
// 桶 key=actor_id（认证后方可知），取不到时回退 IP。
func (m *Middleware) Private(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		class := classifyPrivate(r.Method, r.URL.Path)
		if ok, wait := m.take(class, m.key(r, class)); !ok {
			deny(w, r, wait)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// key 按桶类别解析记账 key：敏感/轮询恒为客户端 IP（防爆破/枚举面向
// 来源，认证身份可换号绕过）；通用/SSE 优先 actor_id（actor:/ip: 前缀
// 区分两个命名空间，避免理论碰撞）。
func (m *Middleware) key(r *http.Request, class bucketClass) string {
	ip := ClientIP(r, m.cfg.TrustedProxy)
	switch class {
	case classSensitive, classPoll:
		return ip
	default:
		if m.actorFrom != nil {
			if id := m.actorFrom(r); id != "" {
				return "actor:" + id
			}
		}
		return "ip:" + ip
	}
}

func (m *Middleware) take(class bucketClass, key string) (bool, time.Duration) {
	switch class {
	case classSensitive:
		return m.sensitive.Allow(key)
	case classPoll:
		return m.poll.Allow(key)
	case classSSE:
		return m.sse.Allow(key)
	default:
		return m.api.Allow(key)
	}
}

// deny 以 httpx 契约 envelope 写出 429（CodeRateLimited 默认
// retryable=true），并附 Retry-After 秒头（ceil 到下一枚令牌恢复时刻）。
func deny(w http.ResponseWriter, r *http.Request, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	httpx.WriteError(w, r, &httpx.APIError{
		Status:  http.StatusTooManyRequests,
		Code:    httpx.CodeRateLimited,
		Message: "rate limit exceeded; honor Retry-After and back off",
	})
}

// splitPath 归一路径为段切片：去 /api/v1 挂载前缀、去首尾斜杠、按 /
// 切分。路径由本包的挂载点决定（app.router 只在 /api/v1 下挂载）。
func splitPath(path string) []string {
	p := strings.TrimPrefix(path, "/api/v1")
	if p == "" {
		return nil
	}
	return strings.Split(strings.Trim(p, "/"), "/")
}

// classifyPublic 是 Public 段的 (method, path) → 桶表。路径参数
// （device_code / id）按段位置匹配，值为任意非空段。
func classifyPublic(method, path string) bucketClass {
	segs := splitPath(path)
	if len(segs) == 0 || segs[0] != "auth" {
		if len(segs) == 2 && segs[0] == "meta" && segs[1] == "capabilities" &&
			method == http.MethodGet {
			return classAPI
		}
		return classNone // 受保护路径/未知路径：Private 段或 NotFound 处理
	}
	switch {
	case method == http.MethodPost &&
		((len(segs) == 2 && segs[1] == "login") ||
			(len(segs) == 2 && segs[1] == "register") ||
			(len(segs) == 3 && segs[1] == "token" && segs[2] == "refresh") ||
			(len(segs) == 3 && segs[1] == "device" && segs[2] == "authorizations")):
		return classSensitive
	case method == http.MethodPost &&
		len(segs) == 5 && segs[1] == "device" && segs[2] == "authorizations" &&
		segs[4] == "token":
		return classPoll
	case method == http.MethodPost && len(segs) == 2 && segs[1] == "logout":
		return classAPI // 公共端点、无认证身份：按 IP 记入通用桶
	default:
		return classNone // me、审批三端点等 priv 组路由：Private 段记账
	}
}

// classifyPrivate 是 Private 段的 (method, path) → 桶表：auth 域内仅
// 设备审批三端点是敏感桶，其余（me 等）入通用桶。
func classifyPrivate(method, path string) bucketClass {
	segs := splitPath(path)
	if len(segs) == 0 {
		return classAPI // priv 组路由必有路径段；空路径保守入通用桶
	}
	switch {
	case segs[0] == "auth" &&
		((len(segs) == 3 && segs[1] == "device" && segs[2] == "authorizations" &&
			method == http.MethodGet) ||
			(len(segs) == 5 && segs[1] == "device" && segs[2] == "authorizations" &&
				(segs[4] == "approve" || segs[4] == "deny") && method == http.MethodPost)):
		return classSensitive
	case len(segs) == 3 && segs[0] == "workspaces" && segs[2] == "events" &&
		method == http.MethodGet:
		return classSSE
	default:
		return classAPI
	}
}
