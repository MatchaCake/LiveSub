package controller

import (
	"strings"
	"testing"
	"unicode"
)

// unwrap strips the 【】 the tests wrap chunks with.
func unwrap(chunks []string) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = strings.TrimSuffix(strings.TrimPrefix(c, "【"), "】")
	}
	return out
}

func asciiAlnum(r rune) bool {
	return r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r))
}

// assertNoTokenCut fails if a chunk boundary falls between two ASCII
// letters/digits, or if the chunks do not add up to the original text.
func assertNoTokenCut(t *testing.T, text string, chunks []string, maxLen int) {
	t.Helper()
	parts := unwrap(chunks)
	if got := strings.Join(parts, ""); got != text {
		t.Fatalf("chunks lose text:\n got %q\nwant %q", got, text)
	}
	for i, c := range chunks {
		if n := len([]rune(c)); n > maxLen {
			t.Errorf("chunk %d is %d runes (max %d): %q", i, n, maxLen, c)
		}
	}
	for i := 0; i+1 < len(parts); i++ {
		a, b := []rune(parts[i]), []rune(parts[i+1])
		if len(a) == 0 || len(b) == 0 {
			t.Fatalf("empty chunk in %q", chunks)
		}
		if asciiAlnum(a[len(a)-1]) && asciiAlnum(b[0]) {
			t.Errorf("token cut between chunk %d and %d: %q | %q", i, i+1, parts[i], parts[i+1])
		}
	}
}

// 2026-11-11 22:18: the danmaku limit landed inside a Latin
// word and the room got 「…Heisei时代的Hei」 followed by 「sei。不知道哎…」.
func TestSplitDoesNotCutLatinWord(t *testing.T) {
	text := "Saint Seiya的《奇迹之翼》我不会唱。刚才那个发射器真的吓到我了。我好歹也是Heisei出生的。" +
		"这个是Heisei的。就是那个Heisei时代的Heisei。不知道哎，我出生那会儿有这个了吗？"
	chunks := splitWithWrap(text, "【", "】", 30)
	assertNoTokenCut(t, text, chunks, 30)
	for _, c := range unwrap(chunks) {
		if strings.HasPrefix(c, "sei") || strings.HasSuffix(c, "Hei") {
			t.Errorf("Heisei was cut: %q", c)
		}
	}
}

func TestSplitKeepsTokensWhole(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		maxLen int
		whole  []string // tokens that must each appear intact in one chunk
	}{
		{"decimal", "今天糸满的最高气温是2.5度，现在是22.5度左右吧", 14, []string{"2.5", "22.5"}},
		{"integer", "大概十二点前睡觉然后早上6点50分起床这样持续了2026年", 16, []string{"50", "2026"}},
		{"thousands", "这个礼物大概要花掉1,000,000日元左右呢真的假的", 16, []string{"1,000,000"}},
		{"hyphenated title", "刚才唱的那首歌的名字叫做Butter-Fly对吧我很喜欢", 18, []string{"Butter-Fly"}},
		{"apostrophe", "他最后说了一句非常帅气的don't worry然后就走了", 20, []string{"don't", "worry"}},
		{"trailing ascii punct", "好的好的好的好的好的好的好的OK!!然后我们继续吧", 18, []string{"OK!!"}},
		{"clock", "明天的直播时间改成晚上的21:30开始请大家注意", 16, []string{"21:30"}},
		{"latin1 letters", "下一首想唱的歌是那个很有名的Café Français哦", 22, []string{"Café", "Français"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chunks := splitWithWrap(tc.text, "【", "】", tc.maxLen)
			if len(chunks) < 2 {
				t.Fatalf("expected a split, got %q", chunks)
			}
			assertNoTokenCut(t, tc.text, chunks, tc.maxLen)
			for _, tok := range tc.whole {
				found := false
				for _, c := range unwrap(chunks) {
					if strings.Contains(c, tok) {
						found = true
					}
				}
				if !found {
					t.Errorf("token %q was cut: %q", tok, chunks)
				}
			}
		})
	}
}

// A token longer than a whole danmaku cannot be kept intact; it must still be
// sent (hard cut) rather than looping forever or being dropped.
func TestSplitOversizedTokenStillSent(t *testing.T) {
	text := "看这个" + strings.Repeat("abcdefghij", 5) + "好长"
	chunks := splitWithWrap(text, "【", "】", 20)
	if got := strings.Join(unwrap(chunks), ""); got != text {
		t.Fatalf("lost text: %q", chunks)
	}
	for _, c := range chunks {
		if n := len([]rune(c)); n > 20 {
			t.Errorf("chunk too long (%d): %q", n, c)
		}
	}
}

// Pure CJK text keeps the old behaviour: break after 。/，/、 in the second
// half of the window, otherwise at the limit.
func TestSplitCJKUnchanged(t *testing.T) {
	text := "今天也是一如既往的哄睡直播。感谢大家一直以来的支持，真的非常感谢。"
	chunks := splitWithWrap(text, "【", "】", 20)
	want := []string{"【今天也是一如既往的哄睡直播。】", "【感谢大家一直以来的支持，】", "【真的非常感谢。】"}
	if strings.Join(chunks, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", chunks, want)
	}
}
