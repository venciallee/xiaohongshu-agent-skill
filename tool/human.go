package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

// xhs human - 去 AI 腔。
//
// 三遍处理，顺序固定：
//   1. 排版   破折号改逗号，汉字后的半角标点改全角，去掉 Markdown 星号和井号
//             （小红书正文不渲染 Markdown，会原样显示）。
//   2. 改写   只做删掉或替换后句子一定通顺的那几种：开头的「在当今……时代」
//             「随着……的发展」、「进行优化」→「优化」、「能够」→「能」。
//   3. 填充语 删掉「值得注意的是」「众所周知」「综上所述」这类删了不影响意思的词。
//
// 其余的套话（赋能、底层逻辑、保姆级……）和句式（不是 A 而是 B、排比三连、
// 首先其次最后）只标记、不改：中文换词要看整句，脚本硬换只会把句子弄坏。
//
// 它不碰任何不可见字符或隐式标识。这个工具的目标是让读者读着不像模板，
// 不是绕过平台的 AI 识别；AI 参与的笔记照样在 App 里勾选 AI 声明。

type Lexicon struct {
	Version     string         `json:"version"`
	Note        string         `json:"note"`
	Typographic []TypoRule     `json:"typographic"`
	Rewrites    []RewriteRule  `json:"rewrites"`
	Terms       []Term         `json:"terms"`
	Structures  []StructureDef `json:"structures"`
}

type TypoRule struct {
	Find string `json:"find"`
	Name string `json:"name"`
	To   string `json:"to"`
	Note string `json:"note"`
}

type RewriteRule struct {
	ID      string `json:"id"`
	Regex   string `json:"regex"`
	Replace string `json:"replace"`
	Name    string `json:"name"`
	re      *regexp.Regexp
}

type Term struct {
	Find   string `json:"find"`
	Action string `json:"action"`
	To     string `json:"to"`
	Family string `json:"family"`
	Hint   string `json:"hint"`
	re     *regexp.Regexp
}

type StructureDef struct {
	ID    string `json:"id"`
	Regex string `json:"regex"`
	Name  string `json:"name"`
	Fix   string `json:"fix"`
	re    *regexp.Regexp
}

func loadLexicon() *Lexicon {
	var lex Lexicon
	mustLoad("slop.json", &lex)
	for i := range lex.Rewrites {
		re, err := regexp.Compile(lex.Rewrites[i].Regex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "slop.json rewrite %s 正则无效: %v\n", lex.Rewrites[i].ID, err)
			continue
		}
		lex.Rewrites[i].re = re
	}
	for i := range lex.Terms {
		t := &lex.Terms[i]
		pat := regexp.QuoteMeta(t.Find)
		if isASCIIWord(t.Find) {
			pat = `(?i)\b` + pat + `\b`
		}
		if t.Action == "delete" {
			pat += `[，,、：:]?\s*`
		}
		t.re = regexp.MustCompile(pat)
	}
	for i := range lex.Structures {
		re, err := regexp.Compile(lex.Structures[i].Regex)
		if err != nil {
			fmt.Fprintf(os.Stderr, "slop.json structure %s 正则无效: %v\n", lex.Structures[i].ID, err)
			continue
		}
		lex.Structures[i].re = re
	}
	return &lex
}

func isASCIIWord(s string) bool {
	for _, r := range s {
		if r >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

type Hit struct {
	Name   string `json:"name"`
	Count  int    `json:"count"`
	Action string `json:"action,omitempty"`
	Hint   string `json:"hint,omitempty"`
	Family string `json:"family,omitempty"`
}

type HumanReport struct {
	Typographic []Hit `json:"typographic"`
	Rewrites    []Hit `json:"rewrites"`
	Deleted     []Hit `json:"deleted"`
	Flagged     []Hit `json:"flagged"`
	Structures  []Hit `json:"structures"`
}

func (r HumanReport) fixedCount() int {
	n := 0
	for _, hs := range [][]Hit{r.Typographic, r.Rewrites, r.Deleted} {
		for _, h := range hs {
			n += h.Count
		}
	}
	return n
}

var (
	protectRE   = regexp.MustCompile("(?s)```.*?```|`[^`\n]+`|https?://\\S+|www\\.\\S+|\\{\\{[^}]+\\}\\}")
	halfPunctRE = regexp.MustCompile(`(\p{Han})([,:;!?])`)
	boldRE      = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	headingRE   = regexp.MustCompile(`(?m)^#{1,6}\s+`)
	dashRE      = regexp.MustCompile(`\s*—+\s*`)
)

var fullWidth = map[string]string{",": "，", ":": "：", ";": "；", "!": "！", "?": "？"}

func protect(text string) (string, []string) {
	var found []string
	out := protectRE.ReplaceAllStringFunc(text, func(m string) string {
		found = append(found, m)
		return fmt.Sprintf("\x00P%d\x00", len(found)-1)
	})
	return out, found
}

func restore(text string, found []string) string {
	for i, f := range found {
		text = strings.ReplaceAll(text, fmt.Sprintf("\x00P%d\x00", i), f)
	}
	return text
}

func humanize(text string, lex *Lexicon) (string, HumanReport) {
	var rep HumanReport
	text, saved := protect(text)

	// 1. 排版
	counted := text
	for _, t := range lex.Typographic {
		n := strings.Count(counted, t.Find)
		counted = strings.ReplaceAll(counted, t.Find, "")
		if n > 0 {
			rep.Typographic = append(rep.Typographic, Hit{Name: t.Find + " " + t.Name, Count: n, Action: "→ " + t.To, Hint: t.Note})
		}
	}
	text = dashRE.ReplaceAllString(text, "，")
	if m := halfPunctRE.FindAllString(text, -1); len(m) > 0 {
		rep.Typographic = append(rep.Typographic, Hit{Name: "汉字后的半角标点", Count: len(m), Action: "→ 全角"})
		text = halfPunctRE.ReplaceAllStringFunc(text, func(s string) string {
			r, size := utf8.DecodeLastRuneInString(s)
			return s[:len(s)-size] + fullWidth[string(r)]
		})
	}
	if m := boldRE.FindAllString(text, -1); len(m) > 0 {
		rep.Typographic = append(rep.Typographic, Hit{Name: "**加粗**（小红书不渲染）", Count: len(m), Action: "→ 去掉星号"})
		text = boldRE.ReplaceAllString(text, "$1")
	}
	if m := headingRE.FindAllString(text, -1); len(m) > 0 {
		rep.Typographic = append(rep.Typographic, Hit{Name: "# 标题（小红书不渲染）", Count: len(m), Action: "→ 去掉井号"})
		text = headingRE.ReplaceAllString(text, "")
	}

	// 2. 改写
	for _, rw := range lex.Rewrites {
		if rw.re == nil {
			continue
		}
		if m := rw.re.FindAllString(text, -1); len(m) > 0 {
			rep.Rewrites = append(rep.Rewrites, Hit{Name: rw.Name, Count: len(m)})
			text = rw.re.ReplaceAllString(text, rw.Replace)
		}
	}

	// 3. 填充语 + 标记
	for _, t := range lex.Terms {
		m := t.re.FindAllString(text, -1)
		if len(m) == 0 {
			continue
		}
		switch t.Action {
		case "delete":
			rep.Deleted = append(rep.Deleted, Hit{Name: t.Find, Count: len(m), Family: t.Family})
			text = t.re.ReplaceAllString(text, "")
		case "replace":
			rep.Rewrites = append(rep.Rewrites, Hit{Name: t.Find + " → " + t.To, Count: len(m), Family: t.Family})
			text = t.re.ReplaceAllString(text, t.To)
		default:
			rep.Flagged = append(rep.Flagged, Hit{Name: t.Find, Count: len(m), Family: t.Family, Hint: t.Hint})
		}
	}

	text = tidy(text)
	text = restore(text, saved)
	rep.Structures = scanStructures(text, lex)
	return strings.TrimSpace(text) + "\n", rep
}

var (
	tidyRules = []struct {
		re  *regexp.Regexp
		rep string
	}{
		{regexp.MustCompile(`[ \t]{2,}`), " "},
		{regexp.MustCompile(`，\s*([，。！？；：、])`), "$1"},
		{regexp.MustCompile(`([。！？；：、])\s*，`), "$1"},
		{regexp.MustCompile(`(?m)^[ \t]*[，。、；：,]+[ \t]*`), ""},
		{regexp.MustCompile(`(?m)[ \t]+$`), ""},
		{regexp.MustCompile(`\n{3,}`), "\n\n"},
	}
)

func tidy(text string) string {
	for i := 0; i < 2; i++ {
		for _, r := range tidyRules {
			text = r.re.ReplaceAllString(text, r.rep)
		}
	}
	return text
}

func scanStructures(text string, lex *Lexicon) []Hit {
	var out []Hit
	for _, s := range lex.Structures {
		if s.re == nil {
			continue
		}
		if m := s.re.FindAllString(text, -1); len(m) > 0 {
			out = append(out, Hit{Name: s.Name, Count: len(m), Hint: s.Fix, Family: s.ID})
		}
	}

	// 首先……其次……最后
	if strings.Contains(text, "首先") && (strings.Contains(text, "其次") || strings.Contains(text, "再次")) {
		out = append(out, Hit{Name: "「首先……其次……最后」", Count: 1, Hint: "报告腔。换成编号卡片，或者只讲最重要的那一步。", Family: "firstly"})
	}

	// 排比三连：连续三个短分句用同一个字开头（更快，更稳，更省）
	if n := countTriads(text); n > 0 {
		out = append(out, Hit{Name: "排比三连（同字开头的三个短分句）", Count: n, Hint: "留最强的一个，另外两个删掉。三连是模型的默认节奏。", Family: "triad"})
	}

	// emoji 列表
	lines := strings.Split(text, "\n")
	emojiLines := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		r, _ := utf8.DecodeRuneInString(l)
		if isEmoji(r) {
			emojiLines++
		}
	}
	if emojiLines >= 3 {
		out = append(out, Hit{Name: "emoji 开头的列表", Count: emojiLines, Hint: "✅📌👉 开头一排是最常见的模板感来源。换成数字编号，或者干脆写成句子。", Family: "emoji-list"})
	}

	// 冒号小标题堆叠
	heads := 0
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" || utf8.RuneCountInString(l) > 40 {
			continue
		}
		if idx := strings.IndexAny(l, "：:"); idx > 0 && utf8.RuneCountInString(l[:idx]) <= 8 {
			heads++
		}
	}
	if heads >= 4 {
		out = append(out, Hit{Name: "「小标题：内容」堆叠", Count: heads, Hint: "像报告。小红书正文是说话，不是大纲。", Family: "colon-heads"})
	}

	// 句长过于均匀
	var lens []float64
	for _, s := range sentences(text) {
		lens = append(lens, float64(units(s)))
	}
	if len(lens) >= 5 {
		if m := mean(lens); m > 0 {
			if cv := pstdev(lens) / m; cv < 0.35 {
				out = append(out, Hit{Name: fmt.Sprintf("句子长度太均匀（起伏 %.2f）", cv), Count: len(lens), Hint: "把一句拆成两半，让另一句长一点。人写字有长有短。", Family: "uniform"})
			}
		}
	}
	return out
}

func countTriads(text string) int {
	n := 0
	for _, sent := range sentenceSep.Split(text, -1) {
		parts := clauseSep.Split(sent, -1)
		run := 1
		for i := 1; i < len(parts); i++ {
			a, b := []rune(strings.TrimSpace(parts[i-1])), []rune(strings.TrimSpace(parts[i]))
			if len(a) > 0 && len(b) > 0 && len(a) <= 10 && len(b) <= 10 && a[0] == b[0] && isHan(a[0]) {
				run++
				if run == 3 {
					n++
				}
			} else {
				run = 1
			}
		}
	}
	return n
}

func renderHumanReport(rep HumanReport) {
	w := os.Stderr
	head := func(t string) { fmt.Fprintf(w, "\n%s\n%s\n", t, strings.Repeat("-", strWidth(t))) }
	head("去 AI 腔报告")
	fmt.Fprintf(w, "自动处理 %d 处，%d 个词标记待改，%d 类句式需要你重写\n",
		rep.fixedCount(), len(rep.Flagged), len(rep.Structures))
	if len(rep.Typographic) > 0 {
		head("1. 排版（已改）")
		for _, h := range rep.Typographic {
			fmt.Fprintf(w, "  %3dx  %s  %s\n", h.Count, h.Name, h.Action)
		}
	}
	if len(rep.Rewrites) > 0 {
		head("2. 改写（已改）")
		for _, h := range rep.Rewrites {
			fmt.Fprintf(w, "  %3dx  %s\n", h.Count, h.Name)
		}
	}
	if len(rep.Deleted) > 0 {
		head("3. 填充语（已删）")
		for _, h := range rep.Deleted {
			fmt.Fprintf(w, "  %3dx  %s  [%s]\n", h.Count, h.Name, h.Family)
		}
	}
	if len(rep.Flagged) > 0 {
		head("4. 套话（没改，逐个看）")
		for _, h := range rep.Flagged {
			fmt.Fprintf(w, "  %3dx  %s  [%s]  %s\n", h.Count, h.Name, h.Family, h.Hint)
		}
	}
	if len(rep.Structures) > 0 {
		head("5. 句式（没改，你来重写）")
		for _, h := range rep.Structures {
			fmt.Fprintf(w, "  %3dx  %s\n        %s\n", h.Count, h.Name, h.Hint)
		}
	}
	if rep.fixedCount() == 0 && len(rep.Flagged) == 0 && len(rep.Structures) == 0 {
		head("干净")
		fmt.Fprintln(w, "  没有要处理的。")
	}
	fmt.Fprintln(w)
}

func cmdHuman(args []string) int {
	f := parseFlags(args, "o", "out")
	in := f.arg(0, "-")
	raw, err := readInput(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	lex := loadLexicon()
	clean, rep := humanize(raw, lex)
	if f.bools["json"] {
		printJSON(map[string]any{"text": clean, "report": rep})
		return 0
	}
	out := f.vals["o"]
	if out == "" {
		out = f.vals["out"]
	}
	if out != "" {
		if err := os.WriteFile(out, []byte(clean), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Fprintln(os.Stderr, "wrote", out)
	} else {
		fmt.Print(clean)
	}
	if f.bools["report"] {
		renderHumanReport(rep)
	}
	return 0
}
