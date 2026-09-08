package event

import (
	"testing"
	"time"
)

// TestDisconnectActorClosesSubscriber：撤销断流语义（security.md）——
// DisconnectActor 关闭目标 actor 的订阅 channel，流循环据此退出。
func TestDisconnectActorClosesSubscriber(t *testing.T) {
	hub := NewHub()
	events, unsub := hub.Subscribe(Subscription{ActorID: "agt_a1", Filter: WorkspaceFilter("ws1")})
	defer unsub()
	other, unsubOther := hub.Subscribe(Subscription{ActorID: "agt_a2", Filter: WorkspaceFilter("ws1")})
	_ = unsubOther

	hub.Publish(Envelope{ID: "evt_1", WorkspaceID: "ws1"})
	if env, ok := readEvent(events, time.Second); !ok || env.ID != "evt_1" {
		t.Fatalf("subscriber should receive matching event, got %v ok=%v", env, ok)
	}
	// 排干 other 的缓冲（同样收到了 evt_1），后续只断言新事件。
	if env, ok := readEvent(other, time.Second); !ok || env.ID != "evt_1" {
		t.Fatalf("other should have buffered evt_1, got %v ok=%v", env, ok)
	}

	if n := hub.DisconnectActor("agt_a1"); n != 1 {
		t.Fatalf("disconnected = %d, want 1", n)
	}
	if _, ok := readEvent(events, 100*time.Millisecond); ok {
		t.Fatal("channel must be closed after DisconnectActor")
	}
	// 幂等：重复断开返回 0。
	if n := hub.DisconnectActor("agt_a1"); n != 0 {
		t.Fatalf("second disconnect = %d, want 0", n)
	}
	// 其他订阅者不受影响。
	hub.Publish(Envelope{ID: "evt_2", WorkspaceID: "ws1"})
	if env, ok := readEvent(other, time.Second); !ok || env.ID != "evt_2" {
		t.Fatalf("other subscriber must keep receiving, got %v ok=%v", env, ok)
	}
}

func readEvent(ch <-chan Envelope, timeout time.Duration) (Envelope, bool) {
	select {
	case env, ok := <-ch:
		return env, ok
	case <-time.After(timeout):
		return Envelope{}, false
	}
}
