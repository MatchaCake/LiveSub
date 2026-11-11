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

// --- 2026-11-11: romaji in Chinese subtitles -------------------------------

func TestPromptChineseTargetForbidsRomaji(t *testing.T) {
	p := buildPrompt("一応 平成生まれ なんです。", "ja-jp", "zh-CN")
	for _, want := range []string{"ja-jp", "zh-CN", "NEVER romanize", "平成 → 平成", "聖闘士星矢 → 圣斗士星矢", "single line"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	if strings.Contains(p, "output their romaji") {
		t.Errorf("the romaji instruction is back:\n%s", p)
	}
	if !strings.HasSuffix(p, "\n\n一応 平成生まれ なんです。") {
		t.Errorf("source text must close the prompt:\n%s", p)
	}
	for _, lang := range []string{"zh", "zh-TW", "cmn-Hans-CN"} {
		if !strings.Contains(buildPrompt("x", "ja-JP", lang), "NEVER romanize") {
			t.Errorf("%s not treated as Chinese", lang)
		}
	}
	// Japanese → English keeps the old rule: romaji is right there.
	if en := buildPrompt("x", "ja-JP", "en-US"); !strings.Contains(en, "romanization") || strings.Contains(en, "NEVER romanize") {
		t.Errorf("non-Chinese target changed:\n%s", en)
	}
}

// Lines the room actually got on 2026-11-11 (old prompt), with their sources.
func TestRomajiToken(t *testing.T) {
	flagged := []struct{ src, out, tok string }{
		{"一応 平成生まれ なんです。", "我好歹也是Heisei出生的。", "Heisei"},
		{"ペガサスファンタジーって 聖闘士星矢ってこと？", "Pegasus Fantasy是指Saint Seiya吗？", "Seiya"},
		{"今何度だ？糸満市", "Itoman现在几度？", "Itoman"},
		{"琵琶湖大橋。", "Biwako Ohashi。", "Biwako"},
		{"今日も 京都で。", "今天也是在Kyoto。", "Kyoto"},
		{"まず、団長自身がねそう。", "首先，Dancho 自己就快睡着了。", "Dancho"},
		{"団長に新しいな", "Danchou好有新意啊。", "Danchou"},
		{"腸内 包装 っていうのが流れるんだよ。", "会播那个叫“chounai housou”的广播。", "chounai"},
		{"ハワイへやるやる。", "去Hawaii，去去。", "Hawaii"},
		{"昭和 じゃ？", "这不是Shōwa风格吗？", "Shōwa"},
		{"思想犯をね。練習してたんです。", "刚才一直在练《Shisouhan》呢。", "Shisouhan"},
		{"銀の龍の背に乗って しかないから終わろう。", "只剩 Gin no Ryuu no Se ni Notte 了，就到这", "Ryuu"},
		{"異世界 情緒さんだね。", "是 Isekai Joucho 呢。", "Isekai"},
		{"今何度だ？糸満市", "Itoman-shi现在几度？", "Itoman-shi"},
	}
	for _, c := range flagged {
		if got := romajiToken(c.src, c.out); got != c.tok {
			t.Errorf("romajiToken(%q) = %q, want %q", c.out, got, c.tok)
		}
	}

	// English that belongs there, Latin the source already had, short tokens.
	clean := []struct{ src, out string }{
		{"ファイヤーボンバー 突撃ラブハート 行こうぜ！", "Fire Bomber 突击爱之心 上吧！突击爱之心！"},
		{"スラムダンク を歌ったことがあるのだ。", "唱过 SLAM DUNK 的啊。是这个人吗？"},
		{"ペガサスファンタジーで合ってる？", "是Pegasus Fantasy对吧？天马座的幻想？"},
		{"スプラトゥーン やろう。", "来玩Splatoon吧。"},
		{"アッパームーン！", "Upper Moon！"},
		{"vaundy って敵だったんだ。", "原来 Vaundy 是敌人啊。"},
		{"いやー、多分 Aimer ぎりだよ。", "哎呀，Aimer 应该已经是极限了吧。"},
		{"kokiaのスプレーする歌", "唱过kokia那首喷雾的歌吗？"},
		{"ok！多分これでいけるはず", "OK！这样应该没问题了。"},
		{"okをすればいいのか？", "点OK就行吗？"},
		{"ゲームオーバー。", "Game Over。"},
		{"ポイズン。", "是Poison吗？"},
		{"翻訳 botの次にコメントが多い。", "发评论是除了翻译bot之外最多的呢。"},
		{"一応 平成生まれ なんです。", "我好歹也是平成出生的。"},
		// STT heard it in Latin: echoing it is not romanization.
		{"今日は sakura の日。", "今天是 Sakura 之日。"},
	}
	for _, c := range clean {
		if got := romajiToken(c.src, c.out); got != "" {
			t.Errorf("false positive %q in %q", got, c.out)
		}
	}
}

func TestIsHepburn(t *testing.T) {
	yes := []string{"heisei", "seiya", "danchou", "karuru", "biwako", "ohashi", "katou", "genkidama",
		"hatenyan", "itoman", "sashihara", "kyoto", "tsushiei", "nyaanyaa", "shinzou", "matcha", "nippon",
		"shimbun", "zasshi", "ryuu", "okinawa"}
	for _, w := range yes {
		if !isHepburn(w) {
			t.Errorf("isHepburn(%q) = false", w)
		}
	}
	no := []string{"pegasus", "fantasy", "saint", "wonder", "lunch", "splatoon", "polaris", "bomber", "fire",
		"initial", "macross", "tigers", "heart", "love", "hello", "smile", "magic", "nice", "upper",
		"beautiful", "colors", "duolingo", "time", "site", "radio", "", "x"}
	for _, w := range no {
		if isHepburn(w) {
			t.Errorf("isHepburn(%q) = true", w)
		}
	}
}

func TestRomajiResultFallsThroughToNextModel(t *testing.T) {
	f := &fakeGen{reply: func(m string, _ bool) (string, error) {
		if m == "gemini-3.8-flash" {
			return "我好歹也是Heisei出生的。", nil
		}
		return "我好歹也是平成出生的。", nil
	}}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	got, err := tr.Translate(context.Background(), "一応 平成生まれ なんです。", "ja-jp", "zh-CN")
	if err != nil || got != "我好歹也是平成出生的。" {
		t.Fatalf("got %q, %v", got, err)
	}
	if strings.Join(f.calls, ",") != "gemini-3.8-flash:off,gemini-3.7-flash:off" {
		t.Fatalf("calls = %v", f.calls)
	}

	// Every model romanizes: the line still goes out (first answer), as before.
	f.calls = nil
	f.reply = func(string, bool) (string, error) { return "我好歹也是Heisei出生的。", nil }
	got, err = tr.Translate(context.Background(), "一応 平成生まれ なんです。", "ja-jp", "zh-CN")
	if err != nil || got != "我好歹也是Heisei出生的。" {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(f.calls) != 3 {
		t.Fatalf("calls = %v", f.calls)
	}
}

// Whole-line Latin for a kana source is still rejected; Latin for a Latin
// source is the right answer and must not cost three calls (zard。→ZARD。 did).
func TestMostlyLatinResult(t *testing.T) {
	f := &fakeGen{reply: func(m string, _ bool) (string, error) {
		if m == "gemini-3.8-flash" {
			return "Pegasus Fantasy。", nil
		}
		return "《天马座的幻想》。", nil
	}}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	got, _ := tr.Translate(context.Background(), "ペガサスファンタジー。", "ja-jp", "zh-CN")
	if got != "《天马座的幻想》。" || len(f.calls) != 2 {
		t.Fatalf("got %q after %v", got, f.calls)
	}

	for src, out := range map[string]string{"zard。": "ZARD。", "vaundy。": "Vaundy。", "the was。": "The was。"} {
		f := &fakeGen{reply: func(string, bool) (string, error) { return out, nil }}
		tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
		got, err := tr.Translate(context.Background(), src, "ja-jp", "zh-CN")
		if err != nil || got != out || len(f.calls) != 1 {
			t.Errorf("%q: got %q, %v after %v", src, got, err, f.calls)
		}
	}

	// English titles inside a Chinese line are accepted on the first model.
	f = &fakeGen{reply: func(string, bool) (string, error) {
		return "Fire Bomber 突击爱之心 上吧！突击爱之心！", nil
	}}
	tr = newTranslator(f.gen, "gemini-3.8-flash", chain)
	if got, _ := tr.Translate(context.Background(), "ファイヤーボンバー 突撃ラブハート 行こうぜ！突撃ラブハート！", "ja-jp", "zh-CN"); len(f.calls) != 1 || !strings.HasPrefix(got, "Fire Bomber") {
		t.Errorf("English title rejected: %q after %v", got, f.calls)
	}
}

// 2026-11-07 21:08, source 「櫓が？」: this reply went to the room as ~40 danmaku.
const commentaryPayload = "炮塔？ / 箭楼？ (Depending on context, \"橹\" in Japanese usually means \"turret\", \"tower\", \"scaffold\"; " +
	"in gaming contexts like Splatoon, \"tower\".)\nLet's choose the most natural and concise translation for a live stream.\n" +
	"\"箭楼？\" or \"橹？\"\n\nLet's provide: 塔？ / 高台？\nWait, \"櫓\" (yagura) is usually translated as \"塔\" (Splatoon) or \"高台？\".\nLet's output: 塔？"

func TestModelCommentaryIsNeverSent(t *testing.T) {
	f := &fakeGen{reply: func(m string, _ bool) (string, error) {
		if m == "gemini-3.8-flash" {
			return commentaryPayload, nil
		}
		return "塔？", nil
	}}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	got, err := tr.Translate(context.Background(), " 櫓が？", "ja-jp", "zh-CN")
	if err != nil || got != "塔？" {
		t.Fatalf("got %q, %v", got, err)
	}

	// All three ramble: drop the line (error) rather than keep the first answer.
	f.reply = func(string, bool) (string, error) { return commentaryPayload, nil }
	got, err = tr.Translate(context.Background(), " 櫓が？", "ja-jp", "zh-CN")
	if err == nil || got != "" {
		t.Fatalf("commentary kept: %q, %v", got, err)
	}
	if !strings.Contains(err.Error(), "model commentary") {
		t.Errorf("err = %v", err)
	}
}

func TestLooksLikeCommentaryBounds(t *testing.T) {
	ok := []struct{ src, out, lang string }{
		{"ね。", "对吧。", "zh-CN"},
		{"少し。", "差一点。", "zh-CN"},
		{"ありがとう。", "Thank you so much, everyone!", "en-US"},
		{"切って焼けばよし。", "切开烤一下就行了。", "zh-CN"},
		{"こんばんは。", "大家晚上好呀，欢迎来到直播间。", "zh-CN"},
	}
	for _, c := range ok {
		if looksLikeCommentary(c.src, c.out, c.lang) {
			t.Errorf("false positive: %q → %q", c.src, c.out)
		}
	}
	if !looksLikeCommentary("消してるから。", "已经关了/已经删了。 (Depending on context: \"消してるから\" usually means \"我已经关掉了\" or \"我已经删掉了\" or \"我都消音了\")", "zh-CN") {
		t.Error("2026-11-07 22:31 payload not caught")
	}
}

func TestMultiLineAnswerBecomesOneLine(t *testing.T) {
	f := &fakeGen{reply: func(string, bool) (string, error) { return "晚上好。\n欢迎光临。\n", nil }}
	tr := newTranslator(f.gen, "gemini-3.8-flash", chain)
	got, err := tr.Translate(context.Background(), "こんばんは。いらっしゃい。", "ja-jp", "zh-CN")
	if err != nil || got != "晚上好。 欢迎光临。" {
		t.Fatalf("got %q, %v", got, err)
	}
}
