package event

import (
	"sync"
)

// Hub 是进程内事件订阅器。SSE handler 从 Hub 订阅，业务模块通过 Hub 发布
// （MVP 单实例直接内存转发）。
//
// TODO(phase-4): 接 transactional outbox ——
//  1. 业务事务内 INSERT outbox（同事务保证不丢）；
//  2. dispatcher 轮询/LISTEN outbox 未投递行 → hub.Publish → 置 sent_at；
//  3. SSE resume：handler 启动时先从 outbox 重放 Last-Event-ID 之后的保留窗口，
//     再接入实时流。当前 Hub 只支持实时，不重放——CLI/GUI 断线期间的事件会缺失，
//     联调时需接受“拉快照 + 增量”的最终一致性模型。
type Hub struct {
	mu   sync.RWMutex
	subs map[uint64]*subscriber
	next uint64
}

type subscriber struct {
	id     uint64
	filter func(Envelope) bool // workspace 过滤等
	ch     chan Envelope
	closed bool
}

func NewHub() *Hub {
	return &Hub{subs: make(map[uint64]*subscriber)}
}

// Subscribe 注册订阅者。filter 为 nil 表示接收全部事件。
// 返回的 channel 由 Hub 在 Unsubscribe 后关闭；消费方必须持续读取或及时退出。
func (h *Hub) Subscribe(filter func(Envelope) bool) (<-chan Envelope, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	sub := &subscriber{id: h.next, filter: filter, ch: make(chan Envelope, 64)}
	h.subs[sub.id] = sub
	unsub := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if !sub.closed {
			sub.closed = true
			delete(h.subs, sub.id)
			close(sub.ch)
		}
	}
	return sub.ch, unsub
}

// Publish 非阻塞分发：订阅者缓冲满时丢弃该事件并保持连接（SSE 客户端靠
// resume/快照补偿）。backpressure 策略见 architecture §6.9。
func (h *Hub) Publish(env Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, sub := range h.subs {
		if sub.filter != nil && !sub.filter(env) {
			continue
		}
		select {
		case sub.ch <- env:
		default:
			// TODO(phase-4): 记录 dropped 计数指标；连续丢弃达到阈值时断开慢订阅者。
		}
	}
}

// WorkspaceFilter 只放行指定 workspace 的事件（含 workspace_id 为空的服务器级事件）。
func WorkspaceFilter(workspaceID string) func(Envelope) bool {
	return func(env Envelope) bool {
		return env.WorkspaceID == "" || env.WorkspaceID == workspaceID
	}
}
