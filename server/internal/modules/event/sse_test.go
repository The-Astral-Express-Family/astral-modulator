package event

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/go-chi/chi/v5"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/ptr"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

// readSSEEvents 解析 SSE 响应文本中的全部事件 envelope。
func readSSEEvents(t *testing.T, body string) []Envelope {
	t.Helper()
	var out []Envelope
	for _, block := range strings.Split(body, "\n\n") {
		for _, line := range strings.Split(block, "\n") {
			if strings.HasPrefix(line, "data: ") {
				var env Envelope
				if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &env); err == nil {
					out = append(out, env)
				}
			}
		}
	}
	return out
}

func TestSSEReplayFromLastEventID(t *testing.T) {
	db := testsupport.NewTestDB(t)
	hub := NewHub()
	handler := &SSEHandler{Hub: hub, DB: db}
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces/{workspace_id}/events", handler.stream)
	ts := httptest.NewServer(r)
	defer ts.Close()

	const ws = "ws_replay"
	emit := func(id, typ string) {
		row := model.OutboxEvent{
			ID: id, Type: typ, Payload: []byte(`{"resource_revision":0,"data":{"n":"` + id + `"}}`),
			OccurredAt: time.Now(),
		}
		wsCopy := ws
		row.WorkspaceID = &wsCopy
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	emit("evt_01", TypeTaskCreated)
	emit("evt_02", TypeTaskUpdated)
	emit("evt_03", TypeTaskReleased)
	// 模拟 dispatcher 已投递（重放只要求行在窗口内存在）。
	if err := db.Model(&model.OutboxEvent{}).Where("1=1").Update("sent_at", time.Now()).Error; err != nil {
		t.Fatal(err)
	}

	// 从 evt_01 之后续传 → 补发 02、03，不含 01；随后接实时 evt_04。
	go func() {
		time.Sleep(200 * time.Millisecond)
		// 未标记 sent_at：PollOnce 只投递未投递行。
		row := model.OutboxEvent{
			ID: "evt_04", Type: TypeMessageCreated,
			Payload:    []byte(`{"resource_revision":0,"data":{}}`),
			OccurredAt: time.Now(),
		}
		wsCopy := ws
		row.WorkspaceID = &wsCopy
		if err := db.Create(&row).Error; err != nil {
			t.Error(err)
			return
		}
		PollOnce(context.Background(), db, hub, discardLogger())
	}()

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workspaces/ws_replay/events", nil)
	req.URL.RawQuery = "last_event_id=evt_01"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type: %q", ct)
	}

	buf := make([]byte, 0, 8192)
	chunk := make([]byte, 1024)
	deadline := time.After(3 * time.Second)
	readErr := error(nil)
readLoop:
	for {
		select {
		case <-deadline:
			break readLoop
		default:
		}
		n, err := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			t.Logf("chunk (%d): %q", n, string(chunk[:min(n, 120)]))
		}
		if err != nil {
			readErr = err
			break
		}
		if strings.Contains(string(buf), "evt_04") {
			break
		}
	}
	t.Logf("readErr=%v bodyLen=%d", readErr, len(buf))
	ids := map[string]bool{}
	for _, env := range readSSEEvents(t, string(buf)) {
		ids[env.ID] = true
	}
	if !ids["evt_02"] || !ids["evt_03"] {
		t.Fatalf("missing replay events, got %v", ids)
	}
	if ids["evt_01"] {
		t.Fatal("cursor event itself must not be replayed")
	}
	if !ids["evt_04"] {
		t.Fatalf("live event after replay missing, got %v", ids)
	}
}

func TestSSESnapshotRequiredOnExpiredCursor(t *testing.T) {
	db := testsupport.NewTestDB(t)
	hub := NewHub()
	handler := &SSEHandler{Hub: hub, DB: db}
	r := chi.NewRouter()
	r.Get("/api/v1/workspaces/{workspace_id}/events", handler.stream)
	ts := httptest.NewServer(r)
	defer ts.Close()

	// 窗口内一行已知 id；客户端游标确定早于它且行不存在 → gap。
	wsID := "ws_gap"
	row := model.OutboxEvent{
		ID: "evt_ffffff", Type: TypeTaskCreated, WorkspaceID: &wsID,
		Payload:    []byte(`{"resource_revision":0,"data":{}}`),
		OccurredAt: time.Now(), SentAt: ptr.Of(time.Now()),
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	req, _ := http.NewRequest("GET", ts.URL+"/api/v1/workspaces/ws_gap/events", nil)
	req.URL.RawQuery = "last_event_id=evt_00000000-0000-7000-8000-000000000000"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	t.Logf("status=%d content-type=%q", resp.StatusCode, resp.Header.Get("Content-Type"))
	buf := make([]byte, 0, 4096)
	chunk := make([]byte, 1024)
	for i := 0; i < 10; i++ {
		n, rerr := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			t.Logf("chunk: %q", string(chunk[:n]))
		}
		if rerr != nil || n == 0 {
			t.Logf("read err: %v", rerr)
			break
		}
	}
	events := readSSEEvents(t, string(buf))
	if len(events) == 0 || events[0].Type != TypeSnapshotRequired {
		t.Fatalf("want snapshot.required, got %+v", events)
	}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
