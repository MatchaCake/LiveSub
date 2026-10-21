package controller

import (
	"strings"
	"testing"
)

// Cases are real lines from the 2026-02 … 2026-10 transcripts/logs.
func TestJunkSource(t *testing.T) {
	drop := []string{
		"",
		"   ",
		"Unitelligible。", // Google STT's own misspelling, conf 0.8–0.9
		" Unitelligible。",
		"Unintelligible。",
		"Foreign speech」",
		"。",
		"？！",
		" た。", // → "。"
		"と。",
		"て。",
		"だ。",
		"を。",
	}
	for _, s := range drop {
		if _, reason, junk := JunkSource(s); !junk {
			t.Errorf("expected drop: %q", s)
		} else {
			t.Logf("dropped %q (%s)", s, reason)
		}
	}

	keep := []string{
		"はい。",
		"ありがとう。",
		"うん。",
		"ok。",
		"ね。",
		"よ。",
		"か。",
		"の。",
		"何。",
		"嘘。",
		"あ。",
		"で。",
		"Www。",
		"谢谢！",
		"180。",
		"かき？",
		"よいしょ！",
		"こんばんは。",
		" 噛む 噛む。",
	}
	for _, s := range keep {
		cleaned, reason, junk := JunkSource(s)
		if junk {
			t.Errorf("false positive on %q (%s)", s, reason)
		}
		if cleaned != strings.TrimSpace(s) {
			t.Errorf("unexpected rewrite %q -> %q", s, cleaned)
		}
	}

	// Placeholder inside real speech: strip it, keep the speech.
	if cleaned, _, junk := JunkSource("阿弥陀佛unintelligible。"); junk || cleaned != "阿弥陀佛。" {
		t.Errorf("got %q junk=%v, want 阿弥陀佛。", cleaned, junk)
	}
}

func TestJunkTranslation(t *testing.T) {
	drop := []string{
		"。",
		"？",
		"”。",
		"Unitelligible。",
		"（嚼 嚼 / 咬 咬）",
		"[音乐]",
		"(音声なし)",
		"【笑】",
		"无法识别。",
		"抱歉，无法翻译这段内容。",
		"请提供需要翻译的文本。",
		"原文为空。",
		"无语音",
		"音声なし",
		"I'm sorry, I can't translate that.",
	}
	for _, s := range drop {
		if reason, junk := junkTranslation(s); !junk {
			t.Errorf("expected drop: %q", s)
		} else {
			t.Logf("dropped %q (%s)", s, reason)
		}
	}

	keep := []string{
		"", // empty is handled by the caller, not reported as junk
		"好的。",
		"谢谢。",
		"嗯。",
		"呢。",
		"抱歉。",
		"音乐。",
		"背景音乐。",
		"无法加载。",
		"我们是什么时候发现自己无法独自生存的？",
		"抱歉，我将调小音量，提高我的歌声。",
		"《Chikyuugi》。",
		"雪人（yuki-daruma）在跳舞。",
		"Www。",
		"7。",
	}
	for _, s := range keep {
		if reason, junk := junkTranslation(s); junk {
			t.Errorf("false positive on %q (%s)", s, reason)
		}
	}
}

// A translation one rune longer than the danmaku limit used to send its final
// "。" as a separate danmaku.
func TestSplitDropsPunctuationTail(t *testing.T) {
	text := strings.Repeat("好", 18) + "。"
	chunks := splitWithWrap(text, "【", "】", 20) // 18 content runes per chunk
	if len(chunks) != 1 || chunks[0] != "【"+strings.Repeat("好", 18)+"】" {
		t.Fatalf("got %q", chunks)
	}
	// Real content in the tail is still sent.
	chunks = splitWithWrap(strings.Repeat("好", 18)+"啊。", "【", "】", 20)
	if len(chunks) != 2 || chunks[1] != "【啊。】" {
		t.Fatalf("got %q", chunks)
	}
	// A punctuation-only text that does not need splitting is left to the
	// junk filter, not silently turned into nothing here.
	if chunks = splitWithWrap("。", "【", "】", 20); len(chunks) != 1 {
		t.Fatalf("got %q", chunks)
	}
}
