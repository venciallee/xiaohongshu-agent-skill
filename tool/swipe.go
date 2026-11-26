package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// xhs swipe - 对标笔记排序。
//
// 小红书公开能看到的只有点赞、收藏、评论，看不到浏览量。所以这里的「爆」不按
// 绝对数算，而是按「这篇的赞藏 ÷ 这个账号最近笔记赞藏的中位数」。
// 10 万粉账号的 2000 赞藏是平常日子；2000 粉账号的 2000 赞藏是找到了东西，
// 而那个东西才是能学的。这就是小红书说的「低粉爆文」，这里把它算成一个倍数。
//
// 另外单独算「收藏 / 点赞」：科普和工具类笔记里，收藏明显多于点赞，说明读者
// 是存起来要用的。这个比值高的句式，比赞藏总量高的句式更值得抄。

var swipeAliases = map[string][]string{
	"account":   {"账号", "博主", "作者", "昵称", "handle"},
	"followers": {"粉丝", "粉丝数", "fans"},
	"median":    {"中位数", "赞藏中位数", "账号中位数", "baseline"},
	"likes":     {"点赞", "赞", "点赞数", "like"},
	"collects":  {"收藏", "收藏数", "collect", "saves"},
	"comments":  {"评论", "评论数", "comment"},
	"title":     {"标题", "hook"},
	"cover":     {"封面", "封面大字", "封面文字"},
	"format":    {"形式", "类型", "图文/视频"},
	"date":      {"日期", "发布日期", "发布时间"},
	"url":       {"链接", "link"},
	"engage":    {"赞藏", "赞藏数", "互动"},
}

type SwipeRow struct {
	Account   string  `json:"account"`
	Title     string  `json:"title"`
	Cover     string  `json:"cover,omitempty"`
	Followers float64 `json:"followers"`
	Median    float64 `json:"median"`
	Likes     float64 `json:"likes"`
	Collects  float64 `json:"collects"`
	Comments  float64 `json:"comments"`
	Engage    float64 `json:"engage"`
	Multiple  float64 `json:"multiple"`
	Basis     string  `json:"basis"`
	SaveRatio float64 `json:"save_ratio"`
	FormulaID int     `json:"formula_id"`
	Formula   string  `json:"formula"`
	Score     float64 `json:"title_score"`
}

func loadSwipe(path string, keywords []string) ([]SwipeRow, string, error) {
	raw, err := readInput(path)
	if err != nil {
		return nil, "", err
	}
	rows, _, err := readTable(raw, swipeAliases)
	if err != nil {
		return nil, "", err
	}
	tb, p, h := loadTitles(), loadPlatform(), loadHouse()
	var out []SwipeRow
	basisNote := "账号中位数"
	for _, m := range rows {
		r := SwipeRow{Account: m["account"], Title: m["title"], Cover: m["cover"]}
		r.Followers, _ = num(m["followers"])
		r.Median, _ = num(m["median"])
		r.Likes, _ = num(m["likes"])
		r.Collects, _ = num(m["collects"])
		r.Comments, _ = num(m["comments"])
		if e, ok := num(m["engage"]); ok && r.Likes == 0 && r.Collects == 0 {
			r.Engage = e
		} else {
			r.Engage = r.Likes + r.Collects
		}
		switch {
		case r.Median > 0:
			r.Multiple, r.Basis = r.Engage/r.Median, "中位数"
		case r.Followers > 0:
			r.Multiple, r.Basis = r.Engage/r.Followers, "赞粉比"
			basisNote = "部分行没有中位数，退回用赞藏 ÷ 粉丝数（赞粉比），两种倍数不能直接比"
		default:
			r.Basis = "无基线"
		}
		if r.Likes > 0 {
			r.SaveRatio = r.Collects / r.Likes
		}
		text := r.Title
		if text == "" {
			text = r.Cover
		}
		r.FormulaID, r.Formula = tb.classify(text)
		r.Score = scoreTitle(text, r.Title == "", keywords, tb, p, h).Score
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Multiple > out[j].Multiple })
	return out, basisNote, nil
}

func thirds(rows []SwipeRow) (top, bottom []SwipeRow) {
	n := len(rows)
	k := int(math.Max(1, math.Floor(float64(n)/3)))
	if n < 3 {
		return rows, nil
	}
	return rows[:k], rows[n-k:]
}

type formulaCount struct {
	id    int
	name  string
	top   int
	bot   int
	total int
}

func swipeSummary(rows []SwipeRow) (counts []formulaCount, lines []string) {
	top, bot := thirds(rows)
	byID := map[int]*formulaCount{}
	get := func(r SwipeRow) *formulaCount {
		if _, ok := byID[r.FormulaID]; !ok {
			byID[r.FormulaID] = &formulaCount{id: r.FormulaID, name: r.Formula}
		}
		return byID[r.FormulaID]
	}
	for _, r := range rows {
		get(r).total++
	}
	for _, r := range top {
		get(r).top++
	}
	for _, r := range bot {
		get(r).bot++
	}
	for _, c := range byID {
		counts = append(counts, *c)
	}
	sort.Slice(counts, func(i, j int) bool {
		if counts[i].top-counts[i].bot != counts[j].top-counts[j].bot {
			return counts[i].top-counts[i].bot > counts[j].top-counts[j].bot
		}
		return counts[i].total > counts[j].total
	})
	stat := func(rs []SwipeRow, f func(SwipeRow) float64) float64 {
		var xs []float64
		for _, r := range rs {
			xs = append(xs, f(r))
		}
		return median(xs)
	}
	if len(bot) > 0 {
		tl := func(r SwipeRow) float64 { return float64(utf16Len(r.Title)) }
		sr := func(r SwipeRow) float64 { return r.SaveRatio }
		hasNum := func(rs []SwipeRow) int {
			n := 0
			for _, r := range rs {
				if numberRE.MatchString(r.Title) {
					n++
				}
			}
			return n
		}
		lines = append(lines,
			fmt.Sprintf("标题长度中位数     上 1/3：%.0f 字    下 1/3：%.0f 字", stat(top, tl), stat(bot, tl)),
			fmt.Sprintf("收藏/点赞 中位数   上 1/3：%.2f      下 1/3：%.2f", stat(top, sr), stat(bot, sr)),
			fmt.Sprintf("标题带数字         上 1/3：%d/%d      下 1/3：%d/%d", hasNum(top), len(top), hasNum(bot), len(bot)),
		)
	}
	return
}

func confidence(n, accounts int) string {
	switch {
	case n >= 40 && accounts >= 6:
		return "样本够看规律（≥40 条、≥6 个账号）。"
	case n >= 20:
		return "样本偏少：只能当线索，别当结论。"
	default:
		return "样本太少：只描述，不下结论。至少收 40 条、6 个账号再看。"
	}
}

func writeSwipeMD(rows []SwipeRow, counts []formulaCount, lines []string, basis, out string) error {
	var b strings.Builder
	accounts := map[string]bool{}
	for _, r := range rows {
		accounts[r.Account] = true
	}
	fmt.Fprintf(&b, "# 对标笔记 swipe file\n\n%d 条 · %d 个账号 · 基线：%s\n\n%s\n\n", len(rows), len(accounts), basis, confidence(len(rows), len(accounts)))
	b.WriteString("## 排序（按倍数）\n\n| 倍数 | 收藏/赞 | 句式 | 标题分 | 账号 | 标题 |\n|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fid := "-"
		if r.FormulaID > 0 {
			fid = fmt.Sprintf("#%d %s", r.FormulaID, r.Formula)
		}
		fmt.Fprintf(&b, "| %.1fx | %.2f | %s | %.0f | %s | %s |\n", r.Multiple, r.SaveRatio, fid, r.Score, r.Account, strings.ReplaceAll(r.Title, "|", "/"))
	}
	b.WriteString("\n## 哪些句式在上 1/3 更多\n\n")
	for _, c := range counts {
		fmt.Fprintf(&b, "- %s：上 1/3 %d 次，下 1/3 %d 次，共 %d\n", nameOf(c), c.top, c.bot, c.total)
	}
	b.WriteString("\n## 上 1/3 和下 1/3 的差别\n\n")
	for _, l := range lines {
		b.WriteString("- " + l + "\n")
	}
	b.WriteString("\n## 未归类（手读，可能是 titles.json 里还没有的句式）\n\n")
	for _, r := range rows {
		if r.FormulaID == 0 {
			fmt.Fprintf(&b, "- %.1fx %s：%s\n", r.Multiple, r.Account, r.Title)
		}
	}
	b.WriteString("\n> 只学句式和结构，不抄标题和内容。每一行都注明了来源账号。\n")
	return os.WriteFile(out, []byte(b.String()), 0o644)
}

func nameOf(c formulaCount) string {
	if c.id == 0 {
		return "未归类"
	}
	return fmt.Sprintf("#%d %s", c.id, c.name)
}

func cmdSwipe(args []string) int {
	f := parseFlags(args, "out", "keywords")
	path := f.arg(0, "")
	if path == "" {
		fmt.Fprintln(os.Stderr, "用法: xhs swipe captured.tsv [--out swipe.md]")
		return 2
	}
	rows, basis, err := loadSwipe(path, splitList(f.vals["keywords"]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	counts, lines := swipeSummary(rows)
	if f.bools["json"] {
		printJSON(map[string]any{"rows": rows, "basis": basis})
		return 0
	}
	accounts := map[string]bool{}
	for _, r := range rows {
		accounts[r.Account] = true
	}
	fmt.Printf("\n对标笔记  ·  %d 条  ·  %d 个账号  ·  基线：%s\n", len(rows), len(accounts), basis)
	fmt.Println(strings.Repeat("=", 80))
	for _, r := range rows {
		fid := "-"
		if r.FormulaID > 0 {
			fid = fmt.Sprintf("#%d", r.FormulaID)
		}
		fmt.Printf("  %7.1fx  藏/赞 %4.2f  %s %s  %s  %s\n", r.Multiple, r.SaveRatio, padRight(fid, 4), padRight(r.Formula, 12), padRight(truncWidth(r.Account, 12), 12), truncWidth(r.Title, 36))
	}
	fmt.Println("\n哪些句式在上 1/3 更多")
	for _, c := range counts {
		fmt.Printf("  %s  上 %d / 下 %d / 共 %d\n", padRight(nameOf(c), 18), c.top, c.bot, c.total)
	}
	if len(lines) > 0 {
		fmt.Println("\n上 1/3 vs 下 1/3")
		for _, l := range lines {
			fmt.Println("  " + l)
		}
	}
	unc := 0
	for _, r := range rows {
		if r.FormulaID == 0 {
			unc++
		}
	}
	fmt.Printf("\n未归类 %d 条：要么是噪音，要么是 titles.json 里还没有的句式。手读这一栏。\n", unc)
	fmt.Println(confidence(len(rows), len(accounts)))
	if out := f.vals["out"]; out != "" {
		if err := writeSwipeMD(rows, counts, lines, basis, expandHome(out)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("\nwrote", out)
	}
	fmt.Println()
	return 0
}

// ---------- xhs eval ----------
// 用你自己收的对标数据检验标题打分：上 1/3（按倍数）和下 1/3 的分数能不能分开。
// AUC = 随机抽一条上 1/3 和一条下 1/3，上面那条分数更高的概率。0.5 等于抛硬币。

func auc(pos, neg []float64) float64 {
	if len(pos) == 0 || len(neg) == 0 {
		return math.NaN()
	}
	wins := 0.0
	for _, a := range pos {
		for _, b := range neg {
			switch {
			case a > b:
				wins++
			case a == b:
				wins += 0.5
			}
		}
	}
	return wins / float64(len(pos)*len(neg))
}

func cmdEval(args []string) int {
	f := parseFlags(args, "keywords")
	path := f.arg(0, "")
	if path == "" {
		fmt.Fprintln(os.Stderr, "用法: xhs eval swipe.tsv [--keywords a,b]")
		return 2
	}
	rows, _, err := loadSwipe(path, splitList(f.vals["keywords"]))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if len(rows) < 12 {
		fmt.Fprintf(os.Stderr, "只有 %d 条。至少 12 条才算得出东西，40 条以上才可信。\n", len(rows))
		return 2
	}
	top, bot := thirds(rows)
	tb, p, h := loadTitles(), loadPlatform(), loadHouse()
	kw := splitList(f.vals["keywords"])
	collect := func(rs []SwipeRow, check string) []float64 {
		var xs []float64
		for _, r := range rs {
			res := scoreTitle(r.Title, false, kw, tb, p, h)
			if check == "" {
				xs = append(xs, res.Score)
			} else {
				xs = append(xs, res.Checks[check].Score)
			}
		}
		return xs
	}
	if f.bools["json"] {
		res := map[string]any{"rows": len(rows), "top": len(top), "bottom": len(bot)}
		per := map[string]float64{"总分": auc(collect(top, ""), collect(bot, ""))}
		for _, c := range titleChecks {
			per[c] = auc(collect(top, c), collect(bot, c))
		}
		for k, v := range per {
			if math.IsNaN(v) {
				per[k] = 0.5
			}
		}
		res["auc"] = per
		printJSON(res)
		return 0
	}
	fmt.Printf("\n标题打分检验  ·  %d 条  ·  上 1/3 = %d 条，下 1/3 = %d 条（按倍数分组）\n", len(rows), len(top), len(bot))
	fmt.Println(strings.Repeat("=", 64))
	all := auc(collect(top, ""), collect(bot, ""))
	fmt.Printf("  %s AUC %.2f\n", padRight("总分", 10), all)
	for _, c := range titleChecks {
		a := auc(collect(top, c), collect(bot, c))
		fmt.Printf("  %s AUC %.2f   上 1/3 中位 %.0f / 下 1/3 中位 %.0f\n", padRight(c, 10), a, median(collect(top, c)), median(collect(bot, c)))
	}
	fmt.Println(strings.Repeat("-", 64))
	switch {
	case all >= 0.70:
		fmt.Println("  在你的对标数据上能分开好坏。可以拿它排标题备选。")
	case all >= 0.58:
		fmt.Println("  有一点区分度。挡烂标题可以，挑爆款别信它。")
	default:
		fmt.Println("  基本是抛硬币。它在你的领域只能挡掉明显的烂标题，看 AUC 最高的那一项，")
		fmt.Println("  其余几项可以在 titles.json / title.go 里调或者干脆不看。")
	}
	fmt.Println("  数据少于 40 条时，这个数字本身就很不稳。")
	fmt.Println()
	return 0
}
