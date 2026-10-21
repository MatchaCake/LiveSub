package controller

import (
	"regexp"
	"strings"
	"unicode"
)

// Junk filtering: lines that carry no meaning for viewers must never become
// danmaku. Mined from the 2026-02 … 2026-10 transcripts and logs, the junk that
// actually reached the room was:
//
//   - Google STT placeholders. When the Mandarin alt-language wins, STT emits
//     "Unitelligible。" (sic) / "Foreign speech」" with HIGH confidence (0.8–0.9),
//     and because the source already matches the zh target it skipped
//     translation and went straight to the room.
//   - Lone particle fragments (「た。」「と。」「て。」…) that the model can only
//     render as a bare "。".
//   - Punctuation-only text: either such a translation, or the "。"/"？" tail
//     left over when a translation is one rune longer than the danmaku limit.
//   - Model annotations instead of a translation: a whole line in brackets
//     ("（嚼 嚼 / 咬 咬）", "[音乐]") or a refusal ("无法识别", "请提供原文").
//
// STT confidence is deliberately NOT used: placeholders score 0.8–0.9 while real
// speech regularly scores 0–0.2, so any threshold drops more speech than junk.
//
// JunkSource runs before translation (saves the Gemini call); junkTranslation
// runs on whatever is about to be queued for an output (translated or passed
// through) as the backstop.

// sttPlaceholder matches Google STT's non-speech placeholders, including its
// own misspelling "Unitelligible".
var sttPlaceholder = regexp.MustCompile(`(?i)un[a-z]{0,3}telligible|foreign\s*speech|inaudible|no\s+speech`)

// fragmentParticles are single kana that, on their own, are a cut-off word
// ending or particle with nothing to translate. Interjection-like singles
// (ね よ か で は な あ え ん の …) are NOT listed: they translate to 呢/好的/吗/…
var fragmentParticles = map[rune]bool{
	'た': true, 'と': true, 'て': true, 'だ': true, 'を': true, 'し': true,
	'に': true, 'へ': true, 'ろ': true, 'ぜ': true, 'も': true,
}

// bracketedOnly is a line that is entirely one bracketed annotation, e.g.
// "（嚼 嚼 / 咬 咬）", "[音乐]", "(音声なし)", "【笑】". Title marks 《》 are
// excluded: a song title alone (《Chikyuugi》) is a real subtitle.
var bracketedOnly = regexp.MustCompile(`^[(（\[［【〔][^(（\[［【〔]*[)）\]］】〕]$`)

// metaReply matches a model talking about the input instead of translating it.
// Anchored at the start so ordinary sentences that merely contain 无法 etc. pass.
var metaReply = regexp.MustCompile(`(?i)^(?:` +
	`(?:抱歉|对不起|不好意思)?[，,]?(?:我)?(?:无法|不能|没法)(?:识别|翻译|辨认|听清|理解(?:这段|该|原文|输入|您))` +
	`|请提供` +
	`|没有(?:可|需要)?(?:翻译|识别)的` +
	`|(?:原文|输入|文本|音频)(?:为空|无意义|不完整|中没有)` +
	`|无(?:有效)?(?:语音|人声|声音|内容)$` +
	`|音声(?:なし|無し)$` +
	`|(?:i'?m sorry|sorry,|i cannot|i can'?t|there is no (?:speech|text))` +
	`)`)

// hasContent reports whether s contains any letter or digit.
func hasContent(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// trimNoise strips surrounding whitespace and punctuation/symbols.
func trimNoise(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
	})
}

// JunkSource inspects one final STT result before it is translated. It returns
// the text to translate (STT placeholders removed) or drop=true with a reason.
func JunkSource(text string) (cleaned, reason string, drop bool) {
	cleaned = strings.TrimSpace(text)
	if cleaned == "" {
		return "", "empty", true
	}
	if sttPlaceholder.MatchString(cleaned) {
		cleaned = strings.TrimSpace(sttPlaceholder.ReplaceAllString(cleaned, ""))
		if !hasContent(cleaned) {
			return "", "stt placeholder", true
		}
	}
	if !hasContent(cleaned) {
		return "", "no letters", true
	}
	if core := []rune(trimNoise(cleaned)); len(core) == 1 && fragmentParticles[core[0]] {
		return "", "lone particle", true
	}
	return cleaned, "", false
}

// junkTranslation reports whether text (about to be queued for an output) is
// meaningless and should be dropped.
func junkTranslation(text string) (string, bool) {
	s := strings.TrimSpace(text)
	if s == "" {
		return "", false // empty is already skipped by the caller
	}
	if !hasContent(s) {
		return "punctuation only", true
	}
	if sttPlaceholder.MatchString(s) && !hasContent(sttPlaceholder.ReplaceAllString(s, "")) {
		return "stt placeholder", true
	}
	if bracketedOnly.MatchString(strings.TrimRight(s, "。．.!！?？ ")) {
		return "bracketed annotation", true
	}
	if metaReply.MatchString(s) {
		return "model meta reply", true
	}
	return "", false
}
