package translate

import (
	"regexp"
	"strings"
)

// Output checks. A model reply is not trusted just because the call succeeded:
// it may be untranslated, romanized, or the model thinking out loud. Each of
// these reached the room at some point. A rejected reply sends the line to the
// next model in the chain.

// rejection is why a model's reply was not accepted.
type rejection struct {
	reason string
	// keep: usable as a last resort when every model was rejected (a subtitle
	// in the wrong script beats a dropped line). Model commentary is never kept.
	keep bool
}

// checkResult returns nil when result is an acceptable translation of source.
func checkResult(source, result, sourceLang, targetLang string) *rejection {
	if looksLikeCommentary(source, result, targetLang) {
		return &rejection{reason: "model commentary", keep: false}
	}
	if looksLikeSource(result, sourceLang, targetLang) && !mostlyLatin(source) {
		return &rejection{reason: "source language", keep: true}
	}
	if isChinese(targetLang) && langBase(sourceLang) == "ja" {
		if tok := romajiToken(source, result); tok != "" {
			return &rejection{reason: "romaji " + tok, keep: true}
		}
	}
	return nil
}

func langBase(lang string) string {
	return strings.SplitN(strings.ToLower(strings.TrimSpace(lang)), "-", 2)[0]
}

// mostlyLatin reports whether more than half of the letters in s are ASCII
// letters. STT sometimes hears a Latin-script name ("zard。", "vaundy。"); a
// Latin reply ("ZARD。") is then the correct translation, not a wrong-language
// one, and used to cost three model calls for nothing.
func mostlyLatin(s string) bool {
	latin, other := 0, 0
	for _, r := range s {
		switch {
		case (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z'):
			latin++
		case r >= 0x3040: // kana, CJK, hangul, full-width…
			other++
		}
	}
	return latin > other
}

// looksLikeCommentary catches a model that answers with deliberation instead
// of a subtitle. 2026-11-07 21:08: 「櫓が？」 (3 runes) came back as
// "炮塔？ / 箭楼？ (Depending on context, "櫓" in Japanese usually means
// "turret"… Let's choose the most natural and concise translation…" — some 700
// runes, sent to the room as ~40 danmaku. A translation is never that much
// longer than its source; CJK targets are usually shorter than the Japanese.
func looksLikeCommentary(source, result, targetLang string) bool {
	src := len([]rune(strings.TrimSpace(source)))
	res := len([]rune(result))
	limit := 3*src + 20
	switch langBase(targetLang) {
	case "zh", "cmn", "ja", "ko":
	default:
		limit = 6*src + 40 // alphabetic targets take more runes per word
	}
	return res > limit
}

// latinToken matches a Latin-script word, allowing inner hyphens/apostrophes
// and macron vowels ("Chan-shan", "Shōwa").
var latinToken = regexp.MustCompile(`[A-Za-zĀāĒēĪīŌōŪū]+(?:[-'’][A-Za-zĀāĒēĪīŌōŪū]+)*`)

var macronFold = strings.NewReplacer("ā", "a", "ē", "e", "ī", "i", "ō", "o", "ū", "u", "-", "", "'", "", "’", "")

// romajiToken returns the first word in result that is a Japanese word spelled
// in Hepburn romaji ("Heisei", "Danchou", "Itoman") and that the source did
// not already contain in Latin letters; "" if there is none.
//
// Only romanized Japanese is flagged. English that a title or a term is really
// written in ("Fire Bomber", "SLAM DUNK", "Splatoon") is left alone: those
// words are not valid Hepburn. Words shorter than 4 letters are ignored (OK,
// AI, no, on) and so are common English words that happen to parse as romaji.
func romajiToken(source, result string) string {
	src := strings.ToLower(source)
	for _, tok := range latinToken.FindAllString(result, -1) {
		w := macronFold.Replace(strings.ToLower(tok))
		if len(w) < 4 || englishLookalikes[w] || strings.Contains(src, strings.ToLower(tok)) {
			continue
		}
		if isHepburn(w) {
			return tok
		}
	}
	return ""
}

// englishLookalikes are common English words made only of Hepburn syllables.
// Not exhaustive on purpose: a miss costs one extra model call, nothing more.
var englishLookalikes = func() map[string]bool {
	m := make(map[string]bool)
	for _, w := range strings.Fields(`
		again bake bare base bean been bite bitten bone button data date demo done
		dose gain game gate gene gone gotten happen hate here hero hidden hide home
		hose into join june keen kite kitten made main make mama mate mean memo
		menu mere mine mini moon more name nano nine none noon nose note onion onto
		open pain panda papa piano pose poison potato pure rain rare rate reason
		region ride rose rotten same season seen soda some soon sudden sure take
		taken teen token tomato tone union upon wake woman zero zone banana`) {
		m[w] = true
	}
	return m
}()

// isHepburn reports whether w (lower-case ASCII letters) can be read entirely
// as Hepburn-romanized kana: (C)V morae, palatalized morae, moraic n, and
// doubled consonants. English words almost never can: they need consonant
// clusters, final consonants other than n, or the letters c/l/q/v/x.
func isHepburn(w string) bool {
	n := len(w)
	if n == 0 {
		return false
	}
	// ok[i]: w[i:] is a valid sequence of morae.
	ok := make([]bool, n+1)
	ok[n] = true
	for i := n - 1; i >= 0; i-- {
		for _, l := range moraLengths(w[i:]) {
			if ok[i+l] {
				ok[i] = true
				break
			}
		}
	}
	return ok[0]
}

func isVowel(c byte) bool { return c == 'a' || c == 'i' || c == 'u' || c == 'e' || c == 'o' }

// moraLengths lists how many bytes a single mora at the start of s may take.
func moraLengths(s string) []int {
	var out []int
	c := s[0]
	at := func(i int) byte {
		if i < len(s) {
			return s[i]
		}
		return 0
	}
	if isVowel(c) {
		out = append(out, 1)
	}
	// Moraic n (also m before b/p/m: shimbun, Namba).
	if c == 'n' || (c == 'm' && strings.IndexByte("bpm", at(1)) >= 0 && at(1) != 0) {
		out = append(out, 1)
	}
	// Sokuon: a doubled consonant, or t before ch (matcha).
	if strings.IndexByte("kstpgdbzj", c) >= 0 && (at(1) == c || (c == 't' && at(1) == 'c' && at(2) == 'h')) {
		out = append(out, 1)
	}
	two := s[:min(2, len(s))]
	three := s[:min(3, len(s))]
	switch three {
	case "shi", "chi", "tsu":
		out = append(out, 3)
	}
	if len(three) == 3 && strings.IndexByte("auo", three[2]) >= 0 {
		switch two {
		case "ky", "gy", "sh", "ch", "ny", "hy", "by", "py", "my", "ry":
			out = append(out, 3) // kya, sho, chu, ryo…
		}
	}
	if len(two) == 2 && isVowel(two[1]) {
		switch c {
		case 'k', 'g', 'n', 'b', 'p', 'm', 'r':
			out = append(out, 2)
		case 's', 'z':
			if two[1] != 'i' { // shi / ji in Hepburn
				out = append(out, 2)
			}
		case 't':
			if two[1] != 'i' && two[1] != 'u' { // chi / tsu
				out = append(out, 2)
			}
		case 'd':
			if two[1] != 'i' && two[1] != 'u' { // ji / zu
				out = append(out, 2)
			}
		case 'h':
			if two[1] != 'u' { // fu
				out = append(out, 2)
			}
		case 'y':
			if two[1] == 'a' || two[1] == 'u' || two[1] == 'o' {
				out = append(out, 2)
			}
		case 'w':
			if two[1] == 'a' || two[1] == 'o' {
				out = append(out, 2)
			}
		case 'j':
			if two[1] != 'e' { // ja ji ju jo
				out = append(out, 2)
			}
		case 'f':
			if two[1] == 'u' {
				out = append(out, 2)
			}
		}
	}
	return out
}

// newlines collapses line breaks a model put inside a short answer: a danmaku
// is one line.
var newlines = regexp.MustCompile(`\s*[\r\n]+\s*`)

func singleLine(s string) string {
	return newlines.ReplaceAllString(strings.TrimSpace(s), " ")
}
