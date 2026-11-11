package translate

import (
	"fmt"
	"strings"
)

// promptBase is shared by every language pair. "Single line / no notes" is not
// cosmetic: on 2026-11-07 a model answered 「櫓が？」 with a page of English
// deliberation ("Depending on context… Let's choose…") that went to the room
// as ~40 danmaku.
const promptBase = "Translate the following %s text to %s. " +
	"Output ONLY the translation on a single line: no notes, no explanations, no alternatives. " +
	"Keep it natural and concise (suitable for live stream subtitles). "

// properNounsChinese replaces the 2026-02 rule "output proper nouns as romaji".
// That rule is what put Heisei / Saint Seiya / Itoman / Danchou into Chinese
// subtitles (10–20% of all lines, every stream since February): viewers read
// kanji and Chinese names, not Hepburn. Names without an established Chinese
// form stay in the source script rather than being transliterated.
const properNounsChinese = "Proper nouns (people, places, era names, titles of songs/anime/games, brands): " +
	"write them the way Chinese speakers do. Names written in kanji keep their characters " +
	"(平成 → 平成, 聖闘士星矢 → 圣斗士星矢, 沖縄 → 冲绳); " +
	"katakana words are translated by meaning or by their usual Chinese name (ポケモン → 宝可梦). " +
	"If a name has no established Chinese form, keep it exactly as written in the source script. " +
	"NEVER romanize: do not spell Japanese words in Latin letters (wrong: Heisei, Seiya, Okinawa). " +
	"Latin letters are fine only where the source text itself has them, " +
	"or for names that Chinese speakers also write in Latin letters (ZARD, Aimer)."

// properNounsDefault is the original rule, kept for non-Chinese targets where
// romanization is the natural rendering (e.g. Japanese → English).
const properNounsDefault = "For proper nouns and person names, output their romaji/romanization instead of translating them."

// isChinese reports whether a BCP-47-ish language code is Chinese
// ("zh-CN", "zh", "cmn-Hans-CN").
func isChinese(lang string) bool {
	l := strings.ToLower(strings.TrimSpace(lang))
	return strings.HasPrefix(l, "zh") || strings.HasPrefix(l, "cmn")
}

// buildPrompt renders the instruction block followed by the text to translate.
func buildPrompt(text, sourceLang, targetLang string) string {
	rule := properNounsDefault
	if isChinese(targetLang) {
		rule = properNounsChinese
	}
	return fmt.Sprintf(promptBase, sourceLang, targetLang) + rule + "\n\n" + text
}
