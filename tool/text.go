package main

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
)

// ---------- models shared by several commands ----------

type House struct {
	Note               string    `json:"note"`
	Account            string    `json:"account"`
	HandleOnCards      string    `json:"handle_on_cards"`
	MaxCards           int       `json:"max_cards"`
	TagsMin            int       `json:"tags_min"`
	TagsMax            int       `json:"tags_max"`
	CoverBigMax        int       `json:"cover_big_max"`
	CardHeadMax        int       `json:"card_head_max"`
	CardTextMax        int       `json:"card_text_max"`
	CardCodeLinesMax   int       `json:"card_code_lines_max"`
	IdentitySentence   string    `json:"identity_sentence"`
	CompanyNotSubject  []string  `json:"company_not_subject"`
	FirstPersonMarkers []string  `json:"first_person_markers"`
	CardForbidden      []string  `json:"card_forbidden"`
	RequireFields      []string  `json:"require_fields"`
	SearchWords        []string  `json:"search_words"`
	ConcreteWords      []string  `json:"concrete_words"`
	CardStyle          CardStyle `json:"card_style"`
}

type CardStyle struct {
	Background string `json:"background"`
	Ink        string `json:"ink"`
	Accent     string `json:"accent"`
	Muted      string `json:"muted"`
	CodeBg     string `json:"code_bg"`
	Font       string `json:"font"`
	Mono       string `json:"mono"`
}

// ---------- CJK-aware text helpers ----------

func isHan(r rune) bool { return unicode.Is(unicode.Han, r) }

func isEmoji(r rune) bool {
	return (r >= 0x1F000 && r <= 0x1FAFF) || (r >= 0x2600 && r <= 0x27BF) ||
		r == 0x2B50 || r == 0x2B55 || r == 0x2B06 || r == 0x2B07 || r == 0x2934 || r == 0x2935 ||
		r == 0x203C || r == 0x2049 || (r >= 0x2190 && r <= 0x21FF && r != 0x2192)
}

func countEmoji(s string) int {
	n := 0
	for _, r := range s {
		if isEmoji(r) {
			n++
		}
	}
	return n
}

// utf16Len is how a browser text field counts: CJK and ASCII 1, most emoji 2.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

func runeLen(s string) int { return len([]rune(s)) }

func runeWidth(r rune) int {
	switch {
	case r == 0, r == 0x200D, r == 0xFE0F, unicode.Is(unicode.Mn, r):
		return 0
	case r >= 0x1F000 && r <= 0x1FAFF:
		return 2
	case (r >= 0x1100 && r <= 0x115F), (r >= 0x2E80 && r <= 0xA4CF), (r >= 0xAC00 && r <= 0xD7A3),
		(r >= 0xF900 && r <= 0xFAFF), (r >= 0xFE30 && r <= 0xFE4F), (r >= 0xFF00 && r <= 0xFF60),
		(r >= 0xFFE0 && r <= 0xFFE6), (r >= 0x20000 && r <= 0x3FFFD):
		return 2
	}
	return 1
}

func strWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

func padRight(s string, w int) string {
	if d := w - strWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

func padLeft(s string, w int) string {
	if d := w - strWidth(s); d > 0 {
		return strings.Repeat(" ", d) + s
	}
	return s
}

func truncWidth(s string, w int) string {
	if strWidth(s) <= w {
		return s
	}
	out, cur := []rune{}, 0
	for _, r := range s {
		rw := runeWidth(r)
		if cur+rw > w-1 {
			break
		}
		out = append(out, r)
		cur += rw
	}
	return string(out) + "…"
}

// wrapWidth breaks a line by display width. CJK can break anywhere; ASCII
// words are kept whole when they fit.
func wrapWidth(s string, w int) []string {
	var lines []string
	for _, raw := range strings.Split(s, "\n") {
		if raw == "" {
			lines = append(lines, "")
			continue
		}
		var cur []rune
		cw := 0
		flush := func() {
			lines = append(lines, string(cur))
			cur, cw = nil, 0
		}
		runes := []rune(raw)
		for i := 0; i < len(runes); i++ {
			r := runes[i]
			if r < 128 && r != ' ' {
				j := i
				for j < len(runes) && runes[j] < 128 && runes[j] != ' ' {
					j++
				}
				word := runes[i:j]
				ww := len(word)
				if cw+ww > w && cw > 0 && ww <= w {
					flush()
				}
				for _, c := range word {
					if cw+1 > w {
						flush()
					}
					cur = append(cur, c)
					cw++
				}
				i = j - 1
				continue
			}
			rw := runeWidth(r)
			if cw+rw > w {
				flush()
			}
			cur = append(cur, r)
			cw += rw
		}
		if len(cur) > 0 {
			flush()
		}
	}
	return lines
}

var (
	asciiTokenRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9._+\-/]*[A-Za-z0-9]|[A-Za-z]`)
	numberRE     = regexp.MustCompile(`[0-9]+(?:[.,][0-9]+)*\s*(?:%|倍|个|次|行|分钟|小时|秒|天|周|月|年|万|k|K|x)?`)
	cnNumberRE   = regexp.MustCompile(`(?:[二三四五六七八九十两百千万]|一[一二三四五六七八九十两百千万])[一二三四五六七八九十两百千万]*(?:个|条|步|招|种|行|次|分钟|小时|倍)`)
	codeSpanRE   = regexp.MustCompile("`[^`\n]+`|「[^」\n]{1,40}」")
	sentenceSep  = regexp.MustCompile(`[。！？!?；;…\n]+`)
	clauseSep    = regexp.MustCompile(`[，、,：:]`)
	digitsRE     = regexp.MustCompile(`[0-9]+`)
)

// units approximates 字数 for density metrics: each Han character counts
// one, each ASCII word counts one.
func units(s string) int {
	n := 0
	for _, r := range s {
		if isHan(r) {
			n++
		}
	}
	return n + len(asciiTokenRE.FindAllString(s, -1)) + len(digitsRE.FindAllString(s, -1))
}

func sentences(s string) []string {
	var out []string
	for _, p := range sentenceSep.Split(s, -1) {
		p = strings.TrimSpace(p)
		if units(p) >= 3 {
			out = append(out, p)
		}
	}
	return out
}

func clamp(x float64) float64 { return math.Max(0, math.Min(100, x)) }

// scale maps value onto 0..100 where `good` -> 100 and `bad` -> 0.
func scale(value, good, bad float64) float64 {
	if good == bad {
		return 50
	}
	return clamp((value - bad) / (good - bad) * 100)
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func pstdev(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := mean(xs)
	s := 0.0
	for _, x := range xs {
		s += (x - m) * (x - m)
	}
	return math.Sqrt(s / float64(len(xs)))
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	c := append([]float64(nil), xs...)
	sort.Float64s(c)
	n := len(c)
	if n%2 == 1 {
		return c[n/2]
	}
	return (c[n/2-1] + c[n/2]) / 2
}

func minf(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	m := xs[0]
	for _, x := range xs[1:] {
		if x < m {
			m = x
		}
	}
	return m
}

func bar(score float64, width int) string {
	filled := int(math.Round(score / 100 * float64(width)))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("#", filled) + strings.Repeat(".", width-filled)
}

func firstUnits(s string, n int) string {
	// the first n display "units" of a string (Han char = 1, ASCII run = 1)
	out := []rune{}
	count := 0
	inASCII := false
	for _, r := range s {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			if !inASCII {
				count++
				inASCII = true
			}
		} else {
			inASCII = false
			if !unicode.IsSpace(r) && !unicode.IsPunct(r) {
				count++
			}
		}
		if count > n {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

func containsAnyFold(s string, words []string) []string {
	var hit []string
	ls := strings.ToLower(s)
	for _, w := range words {
		if w != "" && strings.Contains(ls, strings.ToLower(w)) {
			hit = append(hit, w)
		}
	}
	return hit
}

func uniq(xs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
