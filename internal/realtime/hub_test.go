package realtime

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func waitForSubscribers(t *testing.T, hub *Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.Subscribers() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("subscribers = %d, want %d", hub.Subscribers(), want)
}

func TestServeHTTPStreamsEvent(t *testing.T) {
	hub := NewHub()
	srv := httptest.NewServer(hub)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	for header, want := range map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"Connection":        "keep-alive",
		"X-Accel-Buffering": "no",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}

	waitForSubscribers(t, hub, 1)

	payload := []byte(`{"id":42,"title":"hello"}`)
	hub.Publish(Event{Name: "entry.created", Data: payload})

	type frameResult struct {
		lines []string
		err   error
	}
	frames := make(chan frameResult, 1)
	go func() {
		var lines []string
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				// A blank line terminates a frame.
				if strings.Contains(strings.Join(lines, "\n"), "event:") {
					frames <- frameResult{lines: lines}
					return
				}
				continue
			}
			lines = append(lines, line)
		}
		frames <- frameResult{lines: lines, err: scanner.Err()}
	}()

	select {
	case got := <-frames:
		if got.err != nil {
			t.Fatalf("read stream: %v", got.err)
		}
		joined := strings.Join(got.lines, "\n")
		if !strings.Contains(joined, "retry: 3000") {
			t.Errorf("missing retry field:\n%s", joined)
		}
		if !strings.Contains(joined, "event: entry.created") {
			t.Errorf("missing event field:\n%s", joined)
		}
		if !strings.Contains(joined, "id: 1") {
			t.Errorf("missing monotonic id field:\n%s", joined)
		}
		if !strings.Contains(joined, `data: {"id":42,"title":"hello"}`) {
			t.Errorf("missing data field:\n%s", joined)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event frame")
	}

	cancel()
	waitForSubscribers(t, hub, 0)
}

func TestFramesSplitMultilineData(t *testing.T) {
	ev := Event{ID: "7", Name: "multi", Data: []byte("{\n\"a\":1\n}")}
	got := string(frame(ev))

	if !strings.HasPrefix(got, "id: 7\nevent: multi\ndata: {\ndata: \"a\":1\ndata: }\n\n") {
		t.Fatalf("unexpected frame:\n%q", got)
	}
}

func TestPublishAssignsMonotonicIDs(t *testing.T) {
	hub := NewHub()
	sub := hub.register()
	defer hub.unregister(sub)

	hub.Publish(Event{Name: "a"})
	hub.Publish(Event{Name: "b"})
	hub.Publish(Event{ID: "custom", Name: "c"})
	hub.Publish(Event{Name: "d"})

	var ids []string
	for i := 0; i < 4; i++ {
		select {
		case ev := <-sub.ch:
			ids = append(ids, ev.ID)
		default:
			t.Fatalf("expected 4 buffered events, got %d", i)
		}
	}
	want := []string{"1", "2", "custom", "3"}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("id[%d] = %q, want %q", i, ids[i], want[i])
		}
	}
}

func TestPublishDropsSlowSubscriber(t *testing.T) {
	hub := NewHub()
	sub := hub.register()
	defer hub.unregister(sub)

	// Fill the buffer, then one more publish must drop the subscriber instead
	// of blocking.
	for i := 0; i < subscriberBuffer; i++ {
		hub.Publish(Event{Name: "fill"})
	}
	if got := hub.Subscribers(); got != 1 {
		t.Fatalf("subscribers = %d, want 1", got)
	}

	done := make(chan struct{})
	go func() {
		hub.Publish(Event{Name: "overflow"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}

	if got := hub.Subscribers(); got != 0 {
		t.Errorf("subscribers = %d, want 0 after drop", got)
	}
	// The channel is closed; draining it yields the buffered events and then
	// reports closed.
	remaining := 0
	for range sub.ch {
		remaining++
		if remaining > subscriberBuffer {
			break
		}
	}
	if remaining != subscriberBuffer {
		t.Errorf("drained %d events, want %d", remaining, subscriberBuffer)
	}
}

func TestFrameTrimsFieldNewlines(t *testing.T) {
	got := string(frame(Event{ID: "1\ninjected", Name: "x\ny", Data: []byte(`{}`)}))
	if strings.Contains(got, "\r") || strings.Contains(got, "\ninjected") {
		t.Fatalf("field newlines not sanitized:\n%q", got)
	}
	if !strings.Contains(got, "id: 1 injected") || !strings.Contains(got, "event: x y") {
		t.Fatalf("unexpected frame:\n%q", got)
	}
}

func TestEventDataIsJSONObject(t *testing.T) {
	raw := []byte(`{"k":"v"}`)
	if !json.Valid(raw) {
		t.Fatal("test fixture is not valid JSON")
	}
	ev := Event{Name: "x", Data: raw}
	if !strings.Contains(string(frame(ev)), string(raw)) {
		t.Fatal("frame dropped the data payload")
	}
}
