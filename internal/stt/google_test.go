package stt

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	speechpb "cloud.google.com/go/speech/apiv1/speechpb"
)

func result(text string, final bool, lang string) *speechpb.StreamingRecognitionResult {
	return &speechpb.StreamingRecognitionResult{
		Alternatives: []*speechpb.SpeechRecognitionAlternative{{Transcript: text, Confidence: 0.9}},
		IsFinal:      final,
		LanguageCode: lang,
	}
}

func resp(rs ...*speechpb.StreamingRecognitionResult) *speechpb.StreamingRecognizeResponse {
	return &speechpb.StreamingRecognizeResponse{Results: rs}
}

// script replays canned responses and then returns err forever.
func script(err error, rs ...*speechpb.StreamingRecognizeResponse) func() (*speechpb.StreamingRecognizeResponse, error) {
	i := 0
	return func() (*speechpb.StreamingRecognizeResponse, error) {
		if i < len(rs) {
			i++
			return rs[i-1], nil
		}
		return nil, err
	}
}

func finals(ch chan StreamResult) []StreamResult {
	close(ch)
	var out []StreamResult
	for r := range ch {
		if r.IsFinal {
			out = append(out, r)
		}
	}
	return out
}

var errRotation = errors.New("rpc error: code = OutOfRange desc = Exceeded maximum allowed stream duration of 305 seconds.")

// 2026-11-11 21:06:28: the 305s cut-off landed mid-sentence and the first half
// of 「…の翻訳を翻訳しないことでした」 was lost with the interim results.
func TestRotationKeepsUnfinishedUtterance(t *testing.T) {
	ch := make(chan StreamResult, 16)
	err := receive(context.Background(), script(errRotation,
		resp(result("何が", false, "ja-jp")),
		resp(result(" 何が？", true, "ja-jp")),
		resp(result("固有", false, "ja-jp")),
		// stable part + unstable tail of the same utterance
		resp(result("固有名詞", false, "ja-jp"), result("の翻訳", false, "ja-jp")),
	), ch, "ja-jp", lockWatch{})
	if err == nil || !strings.Contains(err.Error(), "Exceeded maximum allowed stream duration") {
		t.Fatalf("err = %v", err)
	}
	got := finals(ch)
	if len(got) != 2 || got[0].Text != " 何が？" || got[1].Text != "固有名詞の翻訳" {
		t.Fatalf("finals = %+v", got)
	}
	if got[1].Language != "ja-jp" {
		t.Errorf("lang = %q", got[1].Language)
	}
}

// A cut right after a final result has nothing pending: no duplicate line.
func TestRotationAfterFinalAddsNothing(t *testing.T) {
	ch := make(chan StreamResult, 16)
	_ = receive(context.Background(), script(errRotation,
		resp(result("こんばん", false, "ja-jp")),
		resp(result("こんばんは。", true, "ja-jp")),
		resp(), // event-only response must not resurrect anything
	), ch, "ja-jp", lockWatch{})
	if got := finals(ch); len(got) != 1 || got[0].Text != "こんばんは。" {
		t.Fatalf("finals = %+v", got)
	}
}

// A final and the next utterance's interim can share one response.
func TestFinalFollowedByInterimInSameResponse(t *testing.T) {
	ch := make(chan StreamResult, 16)
	_ = receive(context.Background(), script(errRotation,
		resp(result("ありがとう。", true, "ja-jp"), result("それで", false, "")),
	), ch, "ja-jp", lockWatch{})
	got := finals(ch)
	if len(got) != 2 || got[1].Text != "それで" {
		t.Fatalf("finals = %+v", got)
	}
	if got[1].Language != "ja-jp" {
		t.Errorf("missing language not defaulted: %q", got[1].Language)
	}
}

// Audio source ended (ffmpeg died): the server has already finalized
// everything, and nil tells the agent to restart the pipeline.
func TestAudioEOFReturnsNilWithoutFlush(t *testing.T) {
	ch := make(chan StreamResult, 16)
	err := receive(context.Background(), script(io.EOF, resp(result("途中", false, "ja-jp"))), ch, "ja-jp", lockWatch{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := finals(ch); len(got) != 0 {
		t.Fatalf("finals = %+v", got)
	}
}

// Stream going offline / service shutdown cancels the context: don't push a
// line into a pipeline that is being torn down.
func TestCancelledContextDoesNotFlush(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ch := make(chan StreamResult, 16)
	err := receive(ctx, script(context.Canceled, resp(result("途中", false, "ja-jp"))), ch, "ja-jp", lockWatch{})
	if err == nil {
		t.Fatal("want error")
	}
	if got := finals(ch); len(got) != 0 {
		t.Fatalf("finals = %+v", got)
	}
}

// --- language-lock watchdog -------------------------------------------------

const zh = "cmn-hans-cn"

// The two streams from 2026-11-11 that viewers reported as "subtitles frozen".
// Offsets are seconds after the stream started; all finals are cmn-hans-cn
// although the streamer was speaking/singing Japanese.
//
// 22:22:50 — nine empty finals, nothing else, for the full 305 seconds.
var lockedEmpty = []string{"", "", "", "", "", "", "", "", ""} // +60 +90 +149 +179 +230 +237 +243 +269 +275

// 22:07:34 — confident nonsense first (conf 0.91, +60s), then empties.
var lockedGarbageFirst = []string{
	"哎哟，总想到破费后即将来没决定彩虹的在一起去了一分美就在无限渐渐是谁。", // +60
	"", "", "", "", "", "", "", "", "", "", // +90 … +260
	"我很可爱，吃了这样的感染。", // +283
	"", // +295
}

// finalsOf turns texts into one-final-per-response in the given language.
func finalsOf(lang string, texts ...string) []*speechpb.StreamingRecognizeResponse {
	out := make([]*speechpb.StreamingRecognizeResponse, len(texts))
	for i, t := range texts {
		out[i] = resp(result(t, true, lang))
	}
	return out
}

// run feeds responses to receive and returns its error plus what was forwarded.
func run(t *testing.T, w lockWatch, rs ...*speechpb.StreamingRecognizeResponse) (error, []StreamResult) {
	t.Helper()
	ch := make(chan StreamResult, 64)
	err := receive(context.Background(), script(errRotation, rs...), ch, "ja-jp", w)
	return err, finals(ch)
}

var watch1 = lockWatch{primary: "ja", after: 1}

func TestWatchdogEndsEmptyLockedStreamAtFirstFinal(t *testing.T) {
	err, got := run(t, watch1, finalsOf(zh, lockedEmpty...)...)
	if !errors.Is(err, ErrLanguageLock) {
		t.Fatalf("err = %v, want ErrLanguageLock", err)
	}
	// Ended on the very first (empty) final: at +60s instead of +305s.
	if len(got) != 0 {
		t.Fatalf("forwarded %d finals before ending the stream: %+v", len(got), got)
	}
}

func TestWatchdogEndsGarbageLockedStreamAtFirstEmptyFinal(t *testing.T) {
	err, got := run(t, watch1, finalsOf(zh, lockedGarbageFirst...)...)
	if !errors.Is(err, ErrLanguageLock) {
		t.Fatalf("err = %v, want ErrLanguageLock", err)
	}
	// The nonsense line at +60s cannot be told from real Chinese (conf 0.91)
	// and still goes through; the stream ends at +90s, so the second nonsense
	// line (+283s) and four more minutes of silence never happen.
	if len(got) != 1 || !strings.HasPrefix(got[0].Text, "哎哟") {
		t.Fatalf("finals = %+v", got)
	}
}

// A healthy Japanese stream has empty finals too (8 that night, 156 in a
// week) — in the primary language. They must never trip the watchdog.
func TestWatchdogIgnoresPrimaryLanguageStreams(t *testing.T) {
	err, got := run(t, watch1, finalsOf("ja-jp", "こんばんは。", "", "", "いらっしゃい。", "", "", "", "")...)
	if errors.Is(err, ErrLanguageLock) {
		t.Fatal("healthy stream ended by the watchdog")
	}
	if len(got) != 8 {
		t.Fatalf("finals = %d, want 8", len(got))
	}

	// Silence from the start: empty primary-language finals only.
	if err, _ := run(t, watch1, finalsOf("ja-jp", "", "", "", "", "")...); errors.Is(err, ErrLanguageLock) {
		t.Fatal("silent primary-language stream ended by the watchdog")
	}
}

// A streamer who really speaks the alternative language keeps their lines. A
// pause costs one reconnect (Google re-detects the same language); LockGuard
// caps how often that can happen.
func TestWatchdogKeepsRealAlternativeLanguageSpeech(t *testing.T) {
	err, got := run(t, watch1, finalsOf(zh, "晚上好！", "今天也要加油哦。", "", "还有这种事。")...)
	if !errors.Is(err, ErrLanguageLock) {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[1].Text != "今天也要加油哦。" {
		t.Fatalf("real speech lost: %+v", got)
	}

	// With the watchdog sitting this stream out, everything goes through.
	err, got = run(t, lockWatch{primary: "ja", after: 0}, finalsOf(zh, "晚上好！", "", "", "", "还有这种事。")...)
	if errors.Is(err, ErrLanguageLock) || len(got) != 5 {
		t.Fatalf("disabled watchdog interfered: %v, %d finals", err, len(got))
	}
}

func TestWatchdogThresholdsAndEdges(t *testing.T) {
	// after=2 / after=3 need that many empty finals; lines in between do not reset.
	for after, wantForwarded := range map[int]int{2: 2, 3: 4} {
		err, got := run(t, lockWatch{primary: "ja", after: after}, finalsOf(zh, "", "不是。", "", "一般的话。", "", "")...)
		if !errors.Is(err, ErrLanguageLock) || len(got) != wantForwarded {
			t.Errorf("after=%d: err=%v forwarded=%d, want %d", after, err, len(got), wantForwarded)
		}
	}
	// Punctuation-only counts as empty.
	if err, _ := run(t, watch1, finalsOf(zh, "。")...); !errors.Is(err, ErrLanguageLock) {
		t.Errorf("punctuation-only final not treated as empty: %v", err)
	}
	// No language code (single-language streams may omit it): never a lock.
	if err, _ := run(t, watch1, finalsOf("", "", "", "")...); errors.Is(err, ErrLanguageLock) {
		t.Error("finals without a language code tripped the watchdog")
	}
	// Interim results are not evidence.
	if err, _ := run(t, watch1, resp(result("", false, zh)), resp(result("啊", false, zh))); errors.Is(err, ErrLanguageLock) {
		t.Error("interim results tripped the watchdog")
	}
	// Region/script variants of the primary language are the primary language.
	if err, _ := run(t, lockWatch{primary: "ja", after: 1}, finalsOf("ja-JP", "はい。", "")...); errors.Is(err, ErrLanguageLock) {
		t.Error("ja-JP not matched against primary ja")
	}
	// The interim flush must not resurrect text from a locked stream.
	err, got := run(t, watch1, resp(result("哎哟", false, zh)), resp(result("", true, zh)))
	if !errors.Is(err, ErrLanguageLock) || len(got) != 0 {
		t.Errorf("locked stream leaked interim text: %v %+v", err, got)
	}
}

// When a stream that is on an alternative language is cut by rotation, the
// kept interim text carries that stream's language, not the primary one
// (otherwise Chinese would be sent to the ja→zh translator).
func TestInterimFlushUsesStreamLanguage(t *testing.T) {
	err, got := run(t, lockWatch{}, resp(result("晚上好！", true, zh)), resp(result("今天", false, "")))
	if err == nil || len(got) != 2 || got[1].Language != zh {
		t.Fatalf("err=%v finals=%+v", err, got)
	}
}

func TestLockGuardBacksOffAndGivesUp(t *testing.T) {
	var g LockGuard
	lock := fmt.Errorf("wrapped: %w", ErrLanguageLock)

	// Every stream locks (music with nobody talking): ask for more evidence
	// each time, then sit one stream out.
	var thresholds []int
	for i := 0; i < maxLockRebuilds; i++ {
		thresholds = append(thresholds, g.Threshold())
		g.Observe(lock)
	}
	if fmt.Sprint(thresholds) != "[1 2 3]" {
		t.Fatalf("thresholds = %v", thresholds)
	}
	if g.Threshold() != 0 || g.Streak() != maxLockRebuilds {
		t.Fatalf("watchdog not paused after %d rebuilds: threshold=%d", maxLockRebuilds, g.Threshold())
	}

	// That stream runs to its normal rotation, which re-arms the watchdog.
	g.Observe(errRotation)
	if g.Threshold() != 1 || g.Streak() != 0 {
		t.Fatalf("not re-armed after a normal rotation: threshold=%d", g.Threshold())
	}

	// One lock, then a healthy stream: back to the quickest setting.
	g.Observe(lock)
	if g.Threshold() != 2 {
		t.Fatalf("threshold = %d, want 2", g.Threshold())
	}
	g.Observe(nil)
	if g.Threshold() != 1 {
		t.Fatalf("threshold = %d, want 1", g.Threshold())
	}
}
