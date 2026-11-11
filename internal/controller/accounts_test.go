package controller

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/christian-lee/livesub/internal/bot"
	"github.com/christian-lee/livesub/internal/config"
)

// fakeBot is a bot.Bot whose availability and send result can be flipped.
type fakeBot struct {
	name string

	mu    sync.Mutex
	avail bool
	err   error
	sent  []string
	calls int
}

func (f *fakeBot) Send(_ context.Context, _ int64, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, msg)
	return nil
}
func (f *fakeBot) Platform() string   { return "bilibili" }
func (f *fakeBot) Name() string       { return f.name }
func (f *fakeBot) MaxMessageLen() int { return 30 }
func (f *fakeBot) Available() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.avail
}
func (f *fakeBot) setAvail(v bool) {
	f.mu.Lock()
	f.avail = v
	f.mu.Unlock()
}
func (f *fakeBot) sentCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}
func (f *fakeBot) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// captureLogs redirects slog to a buffer for the duration of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	var mu sync.Mutex
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func newTestController(pool *bot.Pool, accounts ...string) *Controller {
	return New(pool, []config.OutputConfig{{
		Name: "jp", Platform: "bilibili", TargetLang: "zh-CN",
		Accounts: accounts, Prefix: "【", Suffix: "】",
	}}, nil, 1001)
}

func send(c *Controller, text string) {
	c.sendMessage(context.Background(), delayedMsg{output: "jp", text: text})
}

func stateOf(t *testing.T, c *Controller) OutputState {
	t.Helper()
	st := c.OutputStates()
	if len(st) != 1 {
		t.Fatalf("states = %d", len(st))
	}
	return st[0]
}

// The production shape on 2026-11-11: an output lists four accounts, two of
// them dead (one blanked in the pool, one never loaded). Every line must still
// go out through the two live ones.
func TestSendSkipsDeadAccounts(t *testing.T) {
	logs := captureLogs(t)
	pool := bot.NewPool()
	blanked := &fakeBot{name: "blanked", avail: false, err: errors.New("cookie required")}
	live1 := &fakeBot{name: "live-1", avail: true}
	live2 := &fakeBot{name: "live-2", avail: true}
	pool.Add(blanked)
	pool.Add(live1)
	pool.Add(live2)
	// "missing" is configured on the output but absent from the pool.
	c := newTestController(pool, "blanked", "live-1", "missing", "live-2")

	const n = 8
	for i := 0; i < n; i++ {
		send(c, "字幕")
	}
	if blanked.callCount() != 0 {
		t.Errorf("dead account was asked to send %d times", blanked.callCount())
	}
	if live1.sentCount()+live2.sentCount() != n {
		t.Fatalf("delivered %d+%d of %d lines", live1.sentCount(), live2.sentCount(), n)
	}
	if live1.sentCount() != n/2 || live2.sentCount() != n/2 {
		t.Errorf("rotation uneven: %d / %d", live1.sentCount(), live2.sentCount())
	}

	st := stateOf(t, c)
	if st.NoAccounts {
		t.Error("output with two live accounts reported as having none")
	}
	if strings.Join(st.UsableBots, ",") != "live-1,live-2" || strings.Join(st.DeadBots, ",") != "blanked,missing" {
		t.Errorf("usable=%v dead=%v", st.UsableBots, st.DeadBots)
	}
	if len(st.Recent) == 0 {
		t.Error("delivered lines missing from Recent")
	}

	out := logs.String()
	if strings.Contains(out, "bot not found") {
		t.Error("per-line 'bot not found' warning is back")
	}
	if got := strings.Count(out, "skipping dead accounts"); got != 1 {
		t.Errorf("dead-account notice logged %d times, want once\n%s", got, out)
	}
}

// 2026-11-11: an output whose only account had an expired cookie dropped 343
// lines with one WARN each while the panel still said 翻译中.
func TestNoUsableAccountIsReportedOnce(t *testing.T) {
	logs := captureLogs(t)
	pool := bot.NewPool()
	c := newTestController(pool, "expired")
	changes := 0
	var mu sync.Mutex
	c.OnChange(func() { mu.Lock(); changes++; mu.Unlock() })

	// The panel must show it before a single line is attempted.
	if st := stateOf(t, c); !st.NoAccounts || strings.Join(st.DeadBots, ",") != "expired" {
		t.Fatalf("idle output not flagged: %+v", st)
	}

	for i := 0; i < 50; i++ {
		send(c, "字幕")
	}
	st := stateOf(t, c)
	if !st.NoAccounts || len(st.UsableBots) != 0 {
		t.Fatalf("state = %+v", st)
	}
	if len(st.Recent) != 0 {
		t.Errorf("undelivered lines listed as sent: %v", st.Recent)
	}
	out := logs.String()
	if got := strings.Count(out, "no usable account"); got != 1 {
		t.Errorf("logged %d times for 50 dropped lines, want once\n%s", got, out)
	}
	if strings.Contains(out, "bot not found") || strings.Contains(out, "msg=sending") {
		t.Errorf("unexpected per-line logging:\n%s", out)
	}
	time.Sleep(20 * time.Millisecond) // notifyChange fires on a goroutine
	mu.Lock()
	defer mu.Unlock()
	if changes != 1 {
		t.Errorf("panel notified %d times, want once", changes)
	}
}

// After the account is re-scanned (syncDBBots puts a live bot back in the
// pool) the output recovers by itself — no unpause, no restart.
func TestOutputRecoversWhenAccountComesBack(t *testing.T) {
	logs := captureLogs(t)
	pool := bot.NewPool()
	b := &fakeBot{name: "expired", avail: false}
	pool.Add(b)
	c := newTestController(pool, "expired")

	send(c, "第一句")
	if b.callCount() != 0 || !stateOf(t, c).NoAccounts {
		t.Fatalf("dead account used or output not flagged")
	}

	b.setAvail(true)
	send(c, "第二句")
	send(c, "第三句")
	if b.sentCount() != 2 {
		t.Fatalf("delivered %d after recovery, want 2", b.sentCount())
	}
	if st := stateOf(t, c); st.NoAccounts || len(st.DeadBots) != 0 {
		t.Errorf("state not cleared: %+v", st)
	}
	out := logs.String()
	if strings.Count(out, "no usable account") != 1 || strings.Count(out, "accounts recovered") != 1 {
		t.Errorf("want one 'no usable account' and one 'recovered':\n%s", out)
	}

	// Dying again is a new state change and is reported again.
	b.setAvail(false)
	send(c, "第四句")
	if strings.Count(logs.String(), "no usable account") != 2 {
		t.Errorf("second outage not reported:\n%s", logs.String())
	}
}

// A cookie that is revoked mid-stream (-101) must not cost the line: the next
// account takes the same chunk and the expiry is reported for the dead one.
func TestLoggedOutBotHandsChunkToNextAccount(t *testing.T) {
	captureLogs(t)
	pool := bot.NewPool()
	revoked := &fakeBot{name: "a", avail: true, err: errors.New("send danmaku: code -101: 账号未登录")}
	live := &fakeBot{name: "b", avail: true}
	pool.Add(revoked)
	pool.Add(live)
	c := newTestController(pool, "a", "b")
	expired := make(chan string, 4)
	c.OnLoginExpired(func(name string) { expired <- name })

	send(c, "字幕")
	if live.sentCount() != 1 {
		t.Fatalf("line lost: live bot sent %d", live.sentCount())
	}
	select {
	case name := <-expired:
		if name != "a" {
			t.Errorf("expired = %q", name)
		}
	case <-time.After(time.Second):
		t.Error("login expiry not reported")
	}
}

// Other failures keep their old meaning: a rate limit or generic error is not
// retried on another account (retrying is what escalated to a mute in July).
func TestGenericSendErrorIsNotRetriedOnOtherAccounts(t *testing.T) {
	captureLogs(t)
	pool := bot.NewPool()
	bad := &fakeBot{name: "a", avail: true, err: errors.New("code 500: boom")}
	other := &fakeBot{name: "b", avail: true}
	pool.Add(bad)
	pool.Add(other)
	c := newTestController(pool, "a", "b")

	send(c, "字幕")
	if other.callCount() != 0 {
		t.Errorf("generic error retried on another account")
	}
	if len(stateOf(t, c).Recent) != 0 {
		t.Errorf("failed line listed as sent")
	}
}

func TestAccountHealthNilPoolAndEmpty(t *testing.T) {
	usable, dead := AccountHealth(nil, []string{"a"})
	if len(usable) != 0 || strings.Join(dead, ",") != "a" {
		t.Errorf("nil pool: usable=%v dead=%v", usable, dead)
	}
	usable, dead = AccountHealth(bot.NewPool(), nil)
	if usable == nil || dead == nil || len(usable)+len(dead) != 0 {
		t.Errorf("empty list must give empty non-nil slices (JSON []), got %v %v", usable, dead)
	}
}
