package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// xhs detect - 五项读者视角自检。
//
// 这是什么：五个本地启发式，量的是读者一眼觉得「像模板 / 像 AI 写的」的那些东西：
// 句子长短一样、没有具体的东西、套话多、破折号和 Markdown 残留、不像人在说话。
//
// 这不是什么：不是小红书的 AI 识别模型，也不调用任何检测服务，更不是用来
// 绕过平台识别的。平台要求 AI 参与的内容主动声明，这一点和分数无关。
//
// 每项 0-100，越高越像人写的。

var (
	personRE   = regexp.MustCompile(`我|你|咱`)
	particleRE = regexp.MustCompile(`吧|呢|啊|嘛|呀|啦|哈|其实|就是|挺|有点|真的|直接|反正|还挺|蛮`)
)

const (
	chkBurst = "句长起伏"
	chkSpec  = "具体度"
	chkSlop  = "套话密度"
	chkMark  = "格式指纹"
	chkVoice = "口语感"
)

var detectChecks = []string{chkBurst, chkSpec, chkSlop, chkMark, chkVoice}

type CheckResult struct {
	Score  float64 `json:"score"`
	Detail string  `json:"detail"`
}

type DetectResult struct {
	Source  string                 `json:"source"`
	Checks  map[string]CheckResult `json:"checks"`
	Overall float64                `json:"human_score"`
	Verdict string                 `json:"verdict"`
}

func checkBurst(text string) CheckResult {
	var lens []float64
	for _, s := range sentences(text) {
		lens = append(lens, float64(units(s)))
	}
	if len(lens) < 4 {
		return CheckResult{50, "太短，判断不了"}
	}
	m := mean(lens)
	cv := 0.0
	if m > 0 {
		cv = pstdev(lens) / m
	}
	return CheckResult{scale(cv, 0.65, 0.20), fmt.Sprintf("%d 句，起伏 %.2f（想要 0.5 以上）", len(lens), cv)}
}

func checkSpec(text string) CheckResult {
	u := units(text)
	if u < 40 {
		return CheckResult{50, "太短，判断不了"}
	}
	nums := len(numberRE.FindAllString(text, -1)) + len(cnNumberRE.FindAllString(text, -1))
	names := map[string]bool{}
	for _, t := range asciiTokenRE.FindAllString(text, -1) {
		if len(t) >= 2 {
			names[strings.ToLower(t)] = true
		}
	}
	code := len(codeSpanRE.FindAllString(text, -1))
	hits := nums + len(names) + code
	d := float64(hits) * 100 / float64(u)
	return CheckResult{scale(d, 4.0, 0.5), fmt.Sprintf("%d 个数字、%d 个工具名/英文词、%d 处命令/引用，每百字 %.1f（想要 3 以上）", nums, len(names), code, d)}
}

func checkSlop(text string, lex *Lexicon) CheckResult {
	u := units(text)
	if u == 0 {
		return CheckResult{50, "空"}
	}
	hits := 0
	var found []string
	for _, t := range lex.Terms {
		if n := len(t.re.FindAllString(text, -1)); n > 0 {
			hits += n
			found = append(found, t.Find)
		}
	}
	for _, rw := range lex.Rewrites {
		if rw.re == nil || rw.ID == "nenggou" {
			continue
		}
		if n := len(rw.re.FindAllString(text, -1)); n > 0 {
			hits += n
			found = append(found, rw.Name)
		}
	}
	d := float64(hits) * 100 / float64(u)
	detail := fmt.Sprintf("%d 处套话，每百字 %.1f", hits, d)
	if len(found) > 0 {
		if len(found) > 5 {
			detail += "（" + strings.Join(found[:5], "、") + "……）"
		} else {
			detail += "（" + strings.Join(found, "、") + "）"
		}
	}
	return CheckResult{scale(d, 0, 3.0), detail}
}

var mdRE = regexp.MustCompile("\\*\\*[^*\n]+\\*\\*|(?m)^#{1,6}\\s|```")

func checkMark(text string) CheckResult {
	dash := strings.Count(text, "——") + strings.Count(strings.ReplaceAll(text, "——", ""), "—")
	md := len(mdRE.FindAllString(text, -1))
	emojiLines := 0
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && isEmoji([]rune(l)[0]) {
			emojiLines++
		}
	}
	excl := len(regexp.MustCompile(`[！!]{2,}`).FindAllString(text, -1))
	total := dash*2 + md*2 + emojiLines + excl
	per1k := float64(total) * 1000 / float64(max(runeLen(text), 1))
	return CheckResult{scale(per1k, 0, 10), fmt.Sprintf("%d 个破折号、%d 处 Markdown、%d 行 emoji 开头、%d 处连续感叹号", dash, md, emojiLines, excl)}
}

func checkVoice(text string, lex *Lexicon) CheckResult {
	u := units(text)
	if u < 40 {
		return CheckResult{50, "太短，判断不了"}
	}
	per := 100 / float64(u)
	person := float64(len(personRE.FindAllString(text, -1))) * per
	part := float64(len(particleRE.FindAllString(text, -1))) * per
	tells := scanStructures(text, lex)
	nt := 0
	var names []string
	for _, t := range tells {
		if t.Family == "uniform" {
			continue
		}
		nt += t.Count
		names = append(names, t.Name)
	}
	score := scale(person, 3.0, 0.3)*0.35 + scale(part, 2.5, 0.2)*0.35 + clamp(100-float64(nt)*20)*0.30
	detail := fmt.Sprintf("每百字 %.1f 个我/你、%.1f 个口语词，%d 处句式套路", person, part, nt)
	if len(names) > 0 {
		if len(names) > 3 {
			names = names[:3]
		}
		detail += "（" + strings.Join(names, "、") + "）"
	}
	return CheckResult{clamp(score), detail}
}

func runDetect(text string, lex *Lexicon) DetectResult {
	r := DetectResult{Checks: map[string]CheckResult{}}
	r.Checks[chkBurst] = checkBurst(text)
	r.Checks[chkSpec] = checkSpec(text)
	r.Checks[chkSlop] = checkSlop(text, lex)
	r.Checks[chkMark] = checkMark(text)
	r.Checks[chkVoice] = checkVoice(text, lex)
	var scores []float64
	for _, c := range detectChecks {
		scores = append(scores, r.Checks[c].Score)
	}
	// 最弱的一项拖分：读者只需要一个信号就会觉得「这是模板」。
	r.Overall = mean(scores)*0.6 + minf(scores)*0.4
	switch {
	case r.Overall >= 70 && minf(scores) >= 55:
		r.Verdict = "PASS"
	case r.Overall >= 50:
		r.Verdict = "REVIEW"
	default:
		r.Verdict = "FLAGGED"
	}
	return r
}

func renderDetect(r DetectResult, label string) {
	title := "AI 腔自检（读者视角）"
	if label != "" {
		title += "  -  " + label
	}
	fmt.Println()
	fmt.Println(title)
	fmt.Println(strings.Repeat("=", 66))
	weakest, ws := "", 101.0
	for _, c := range detectChecks {
		res := r.Checks[c]
		fmt.Printf("  %s %s %5.1f\n", padRight(c, 10), bar(res.Score, 24), res.Score)
		fmt.Printf("  %s %s\n", padRight("", 10), res.Detail)
		if res.Score < ws {
			weakest, ws = c, res.Score
		}
	}
	fmt.Println(strings.Repeat("-", 66))
	fmt.Printf("  %s %s %5.1f   %s\n", padRight("总分", 10), bar(r.Overall, 24), r.Overall, r.Verdict)
	if r.Verdict != "PASS" {
		fmt.Printf("\n  最弱的一项：%s。先改它。\n", weakest)
	}
	fmt.Println()
}

func cmdDetect(args []string) int {
	f := parseFlags(args)
	lex := loadLexicon()
	paths := f.pos
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	var results []DetectResult
	for _, p := range paths {
		text, err := readInput(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		r := runDetect(text, lex)
		r.Source = p
		results = append(results, r)
	}
	if f.bools["json"] {
		if len(results) == 1 {
			printJSON(results[0])
		} else {
			printJSON(results)
		}
		return 0
	}
	for _, r := range results {
		label := ""
		if len(results) > 1 {
			label = filepath.Base(r.Source)
		}
		renderDetect(r, label)
	}
	if len(results) == 2 {
		a, b := results[0], results[1]
		fmt.Printf("  %.1f %s  ->  %.1f %s   (%+.1f)\n\n", a.Overall, a.Verdict, b.Overall, b.Verdict, b.Overall-a.Overall)
	}
	if results[len(results)-1].Verdict == "PASS" {
		return 0
	}
	return 1
}
