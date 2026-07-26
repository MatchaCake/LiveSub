package controller

import "testing"

// Regression guard for the 2026-07-26 incident: a low-confidence STT segment
// made the translation model emit 58 consecutive 等, which spammed the room
// into a Bilibili mute (10031 → 1003). The detector must catch that shape
// without eating legitimate repetition — "等等等等" is a normal rendering of
// 「待って待って」, and "哈哈哈" is ordinary laughter.
func TestIsDegenerate(t *testing.T) {
	degenerate := []string{
		"等等等等等等等等等等等等等等等等等等等等等等等等等等等等", // the incident payload
		"啊啊啊啊啊啊啊啊啊啊啊啊",
		"ららららららららららら",
	}
	for _, s := range degenerate {
		reason, bad := isDegenerate(s)
		if !bad {
			t.Errorf("expected degenerate, got clean: %q", s)
			continue
		}
		t.Logf("blocked %q (%s)", truncForLog(s), reason)
	}

	clean := []string{
		"假装防守，我来进攻。啊，等等等等，没爬上去。一点点。", // the real translation that started it
		"等等等等",
		"哈哈哈",
		"守住就赢了对吧。只要守住。",
		"好极好极好极！",
		"", // empty must not panic
	}
	for _, s := range clean {
		if reason, bad := isDegenerate(s); bad {
			t.Errorf("false positive on %q (%s)", s, reason)
		}
	}
}
