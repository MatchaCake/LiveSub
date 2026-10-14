package translate

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeGen struct {
	mu    sync.Mutex
	calls []string // "model:thinkOff"
	reply func(model string, thinkOff bool) (string, error)
}

func (f *fakeGen) gen(ctx context.Context, model, prompt string, thinkOff bool) (string, error) {
	f.mu.Lock()
	tag := model
	if thinkOff {
		tag += ":off"
	}
	f.calls = append(f.calls, tag)
	f.mu.Unlock()
	return f.reply(model, thinkOff)
}

var chain = []string{"gemini-3.7-flash", "gemini-3.6-flash"}

func TestPrimaryUsedWithThinkingOff(t *testing.T) {
	f := &fakeGen{reply: func(m string, _ bool) (string, error) { return "差一点。", nil }}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	got, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN")
	if err != nil || got != "差一点。" {
		t.Fatalf("got %q, %v", got, err)
	}
	if strings.Join(f.calls, ",") != "gemini-3.8-flash:off" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestFallsThroughChainInOrderAndCoolsDown(t *testing.T) {
	now := time.Unix(1000, 0)
	f := &fakeGen{reply: func(m string, _ bool) (string, error) {
		switch m {
		case "gemini-3.8-flash":
			return "", errors.New("Error 429, RESOURCE_EXHAUSTED")
		case "gemini-3.7-flash":
			return "", errors.New("Error 404, NOT_FOUND model")
		}
		return "差一点。", nil
	}}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	tr.now = func() time.Time { return now }

	got, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN")
	if err != nil || got != "差一点。" {
		t.Fatalf("got %q, %v", got, err)
	}
	want := "gemini-3.8-flash:off,gemini-3.7-flash:off,gemini-3.6-flash:off"
	if strings.Join(f.calls, ",") != want {
		t.Fatalf("calls = %v", f.calls)
	}

	// While cooling, the healthy model is tried first.
	f.calls = nil
	if _, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "gemini-3.6-flash:off" {
		t.Fatalf("expected healthy fallback first, calls = %v", f.calls)
	}

	// After cooldown the primary is tried first again.
	now = now.Add(defaultCooldown + time.Second)
	f.reply = func(string, bool) (string, error) { return "ok好", nil }
	f.calls = nil
	if _, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN"); err != nil {
		t.Fatal(err)
	}
	if f.calls[0] != "gemini-3.8-flash:off" {
		t.Fatalf("expected primary after cooldown, calls = %v", f.calls)
	}
}

func TestTimeoutFallsThrough(t *testing.T) {
	f := &fakeGen{}
	f.reply = func(m string, _ bool) (string, error) { return "差一点。", nil }
	tr := newTranslator(func(ctx context.Context, m, p string, off bool) (string, error) {
		if m == "gemini-3.8-flash" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return f.gen(ctx, m, p, off)
	}, "gemini-3.8-flash", chain, WithAttemptTimeout(20*time.Millisecond))
	got, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN")
	if err != nil || got != "差一点。" {
		t.Fatalf("got %q, %v", got, err)
	}
	if f.calls[0] != "gemini-3.7-flash:off" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestThinkingRejectedRetriesWithoutAndRemembers(t *testing.T) {
	f := &fakeGen{reply: func(m string, off bool) (string, error) {
		if off {
			return "", errors.New("Error 400, INVALID_ARGUMENT: thinking budget 0 is not supported")
		}
		return "差一点。", nil
	}}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	for i := 0; i < 2; i++ {
		if got, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN"); err != nil || got != "差一点。" {
			t.Fatalf("got %q, %v", got, err)
		}
	}
	want := "gemini-3.8-flash:off,gemini-3.8-flash,gemini-3.8-flash"
	if strings.Join(f.calls, ",") != want {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestUntranslatedTriesNextModelThenKeepsFirstResult(t *testing.T) {
	f := &fakeGen{reply: func(m string, _ bool) (string, error) {
		if m == "gemini-3.6-flash" {
			return "差一点。", nil
		}
		return "少しだけ待ってね", nil
	}}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	got, _ := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN")
	if got != "差一点。" {
		t.Fatalf("got %q", got)
	}

	f.reply = func(string, bool) (string, error) { return "少しだけ待ってね", nil }
	got, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN")
	if err != nil || got != "少しだけ待ってね" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestAllFailReturnsError(t *testing.T) {
	f := &fakeGen{reply: func(string, bool) (string, error) { return "", errors.New("Error 503, UNAVAILABLE") }}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	_, err := tr.Translate(context.Background(), "少し。", "ja-JP", "zh-CN")
	if err == nil || !strings.Contains(err.Error(), "gemini-3.6-flash") {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 3 {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestDefaultFallbackAndDedupe(t *testing.T) {
	tr := newTranslator(nil, "gemini-3.8-flash", nil)
	if got := strings.Join(tr.Models(), ","); got != "gemini-3.8-flash,gemini-flash-latest" {
		t.Fatalf("models = %s", got)
	}
	tr = newTranslator(nil, "a", []string{"b", "a", " ", "b", "c"})
	if got := strings.Join(tr.Models(), ","); got != "a,b,c" {
		t.Fatalf("models = %s", got)
	}
}
