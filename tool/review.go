package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// xhs review - 72 小时复盘。
//
// 只认这个账号自己的数据。所有「好 / 差」都是和本账号最近笔记的中位数比，
// 不用网上流传的「点击率低于 X% 就是差」这种绝对线：那些数字没有出处，
// 而且不同领域差得很远。
//
// 漏斗拆成四段，先找最先掉下去的那一段，后面的都是它的下游：
//
//	曝光  ->  点击（观看 / 曝光）  ->  留下（收藏率、平均观看时长）  ->  关注（涨粉 / 观看）
//	分发问题        封面标题问题              内容兑现问题                   主页问题
//
// 「看的人多、关注的少」不是这篇失败了，是主页没接住，去 xhs-profile。

var reviewAliases = map[string][]string{
	"date":        {"日期", "发布日期", "发布时间", "published"},
	"title":       {"标题", "笔记", "笔记标题"},
	"formula":     {"句式", "formula_id"},
	"impressions": {"曝光", "曝光数", "曝光量", "展现", "impression"},
	"views":       {"观看", "观看数", "阅读", "阅读数", "浏览", "浏览量", "view", "reads"},
	"ctr":         {"点击率", "封面点击率"},
	"avg_time":    {"平均观看时长", "人均观看时长", "平均阅读时长", "avg_watch", "avg_seconds"},
	"likes":       {"点赞", "点赞数", "赞", "like"},
	"collects":    {"收藏", "收藏数", "collect", "saves"},
	"comments":    {"评论", "评论数", "comment"},
	"shares":      {"分享", "分享数", "转发", "share"},
	"follows":     {"涨粉", "涨粉数", "新增关注", "follow"},
}

type NoteStat struct {
	Date        string   `json:"date"`
	Title       string   `json:"title"`
	FormulaID   int      `json:"formula_id"`
	Formula     string   `json:"formula"`
	Testing     string   `json:"testing,omitempty"`
	Impressions float64  `json:"impressions"`
	Views       float64  `json:"views"`
	CTR         float64  `json:"ctr"`
	AvgTime     float64  `json:"avg_time"`
	Likes       float64  `json:"likes"`
	Collects    float64  `json:"collects"`
	Comments    float64  `json:"comments"`
	Shares      float64  `json:"shares"`
	Follows     float64  `json:"follows"`
	SaveRate    float64  `json:"save_rate"`
	EngageRate  float64  `json:"engage_rate"`
	FollowRate  float64  `json:"follow_rate"`
	CommentRate float64  `json:"comment_rate"`
	Multiple    float64  `json:"multiple"`
	Diagnosis   []string `json:"diagnosis"`
}

func ratio(a, b float64) float64 {
	if b <= 0 {
		return 0
	}
	return a / b
}

func cmdReview(args []string) int {
	f := parseFlags(args)
	path := f.arg(0, "")
	if path == "" {
		fmt.Fprintln(os.Stderr, "用法: xhs review notes.csv   （创作中心「笔记数据」抄成表，一篇一行）")
		return 2
	}
	raw, err := readInput(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	rows, unknown, err := readTable(raw, reviewAliases)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	tb := loadTitles()
	logByTitle := map[string]map[string]any{}
	for _, e := range readLog() {
		if t, ok := e["title"].(string); ok {
			logByTitle[t] = e
		}
	}

	var notes []NoteStat
	for _, m := range rows {
		n := NoteStat{Date: m["date"], Title: m["title"]}
		n.Impressions, _ = num(m["impressions"])
		n.Views, _ = num(m["views"])
		n.AvgTime, _ = num(m["avg_time"])
		n.Likes, _ = num(m["likes"])
		n.Collects, _ = num(m["collects"])
		n.Comments, _ = num(m["comments"])
		n.Shares, _ = num(m["shares"])
		n.Follows, _ = num(m["follows"])
		if n.Impressions > 0 && n.Views > 0 {
			n.CTR = n.Views / n.Impressions
		} else if v, ok := num(m["ctr"]); ok {
			if v > 1 {
				v /= 100
			}
			n.CTR = v
		}
		n.SaveRate = ratio(n.Collects, n.Views)
		n.EngageRate = ratio(n.Likes+n.Collects+n.Comments+n.Shares, n.Views)
		n.FollowRate = ratio(n.Follows, n.Views)
		n.CommentRate = ratio(n.Comments, n.Views)
		n.FormulaID, n.Formula = tb.classify(n.Title)
		if e, ok := logByTitle[n.Title]; ok {
			if t, ok := e["testing"].(string); ok {
				n.Testing = t
			}
		}
		notes = append(notes, n)
	}

	col := func(fn func(NoteStat) float64) []float64 {
		var xs []float64
		for _, n := range notes {
			if v := fn(n); v > 0 {
				xs = append(xs, v)
			}
		}
		return xs
	}
	med := map[string]float64{
		"impressions": median(col(func(n NoteStat) float64 { return n.Impressions })),
		"views":       median(col(func(n NoteStat) float64 { return n.Views })),
		"ctr":         median(col(func(n NoteStat) float64 { return n.CTR })),
		"avg_time":    median(col(func(n NoteStat) float64 { return n.AvgTime })),
		"save":        median(col(func(n NoteStat) float64 { return n.SaveRate })),
		"follow":      median(col(func(n NoteStat) float64 { return n.FollowRate })),
		"comment":     median(col(func(n NoteStat) float64 { return n.CommentRate })),
	}
	enough := len(notes) >= 3
	for i := range notes {
		n := &notes[i]
		n.Multiple = ratio(n.Views, med["views"])
		if !enough {
			continue
		}
		rel := func(v float64, key string) float64 { return ratio(v, med[key]) }
		switch {
		case med["impressions"] > 0 && n.Impressions > 0 && rel(n.Impressions, "impressions") < 0.5:
			n.Diagnosis = append(n.Diagnosis, fmt.Sprintf("分发：曝光只有中位数的 %.0f%%。先看这篇有没有违规 / 限流提示，再看话题和标题里的搜索词。", rel(n.Impressions, "impressions")*100))
		case med["ctr"] > 0 && n.CTR > 0 && rel(n.CTR, "ctr") < 0.75:
			n.Diagnosis = append(n.Diagnosis, fmt.Sprintf("封面 / 标题：曝光正常，点击率只有中位数的 %.0f%%。问题在双列流那一秒，不在正文。", rel(n.CTR, "ctr")*100))
		case med["save"] > 0 && rel(n.SaveRate, "save") < 0.6 && (med["avg_time"] == 0 || rel(n.AvgTime, "avg_time") < 0.85):
			n.Diagnosis = append(n.Diagnosis, "内容兑现：点进来了，但没存、也没看完。前两张卡有没有兑现封面的承诺？能复制的东西是不是在第 2 张？")
		}
		if n.Multiple >= 1.5 && med["follow"] > 0 && rel(n.FollowRate, "follow") < 0.6 {
			n.Diagnosis = append(n.Diagnosis, "主页：看的人多、关注的少。这篇没失败，是主页没接住，去 xhs-profile。")
		}
		if med["comment"] > 0 && rel(n.CommentRate, "comment") >= 2 {
			n.Diagnosis = append(n.Diagnosis, "评论区有东西：评论率是中位数的两倍以上。去 xhs-reply 挖下一篇。")
		}
		if n.Multiple >= 2 && rel(n.SaveRate, "save") >= 1 {
			n.Diagnosis = append(n.Diagnosis, "信号：观看是中位数的两倍以上，收藏率也不低。把它的选题类型和句式记下来，下周复用一次。")
		}
	}

	if f.bools["json"] {
		printJSON(map[string]any{"notes": notes, "medians": med})
		return 0
	}

	md := f.bools["md"]
	out := func(format string, a ...any) { fmt.Printf(format, a...) }
	sorted := append([]NoteStat(nil), notes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Multiple > sorted[j].Multiple })

	if md {
		out("## 复盘 %s · %d 篇\n\n", time.Now().Format("2006-01-02"), len(notes))
		out("中位数：曝光 %s，观看 %s，点击率 %s，收藏率 %s，涨粉率 %s\n\n", fmtCount(med["impressions"]), fmtCount(med["views"]), pct(med["ctr"]), pct(med["save"]), pct(med["follow"]))
		out("| 倍数 | 点击率 | 收藏率 | 涨粉率 | 句式 | 标题 |\n|---|---|---|---|---|---|\n")
		for _, n := range sorted {
			out("| %.1fx | %s | %s | %s | #%d | %s |\n", n.Multiple, pct(n.CTR), pct(n.SaveRate), pct(n.FollowRate), n.FormulaID, n.Title)
		}
		out("\n")
		for _, n := range sorted {
			for _, d := range n.Diagnosis {
				out("- **%s**：%s\n", n.Title, d)
			}
		}
		return 0
	}

	out("\n复盘  ·  %d 篇  ·  中位数：曝光 %s / 观看 %s / 点击率 %s / 收藏率 %s / 涨粉率 %s\n",
		len(notes), fmtCount(med["impressions"]), fmtCount(med["views"]), pct(med["ctr"]), pct(med["save"]), pct(med["follow"]))
	if len(unknown) > 0 {
		out("  （没认出的列：%s）\n", strings.Join(unknown, "、"))
	}
	out("%s\n", strings.Repeat("=", 86))
	out("  %s %s %s %s %s %s  %s\n", padLeft("倍数", 6), padLeft("曝光", 7), padLeft("点击率", 7), padLeft("收藏率", 7), padLeft("涨粉率", 7), padRight("句式", 14), "标题")
	for _, n := range sorted {
		fid := "-"
		if n.FormulaID > 0 {
			fid = fmt.Sprintf("#%d %s", n.FormulaID, n.Formula)
		}
		out("  %s %s %s %s %s %s  %s\n", padLeft(fmt.Sprintf("%.1fx", n.Multiple), 6), padLeft(fmtCount(n.Impressions), 7), padLeft(pct(n.CTR), 7),
			padLeft(pct(n.SaveRate), 7), padLeft(pct(n.FollowRate), 7), padRight(truncWidth(fid, 14), 14), truncWidth(n.Title, 30))
	}
	out("\n诊断（和本账号中位数比）\n")
	if !enough {
		out("  不到 3 篇，没有中位数可比，只列数据。\n")
	}
	any := false
	for _, n := range sorted {
		for i, d := range n.Diagnosis {
			if i == 0 {
				out("  %s\n", truncWidth(n.Title, 60))
			}
			out("    - %s\n", d)
			any = true
		}
		if n.Testing != "" && len(n.Diagnosis) > 0 {
			out("    本篇在测：%s\n", n.Testing)
		}
	}
	if enough && !any {
		out("  没有明显掉下去的环节，这批都在本账号的正常范围内。\n")
	}

	// 句式聚合
	type agg struct {
		name string
		xs   []float64
	}
	byF := map[int]*agg{}
	for _, n := range notes {
		if _, ok := byF[n.FormulaID]; !ok {
			name := n.Formula
			if n.FormulaID == 0 {
				name = "未归类"
			}
			byF[n.FormulaID] = &agg{name: name}
		}
		byF[n.FormulaID].xs = append(byF[n.FormulaID].xs, n.Multiple)
	}
	if len(notes) >= 6 && len(byF) > 1 {
		var keys []int
		for k := range byF {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return median(byF[keys[i]].xs) > median(byF[keys[j]].xs) })
		out("\n按句式（倍数中位数，n = 篇数）\n")
		for _, k := range keys {
			a := byF[k]
			out("  %s  %.1fx  n=%d\n", padRight(a.name, 14), median(a.xs), len(a.xs))
		}
	}

	out("\n%s\n", strings.Repeat("-", 86))
	switch {
	case len(notes) < 10:
		out("  %d 篇：只能看单篇哪一段掉了，看不出规律。别据此改方向。\n", len(notes))
	case len(notes) < 30:
		out("  %d 篇：能看出线索。结论写成「下 5 篇验证 X」，不要写成「X 有效」。\n", len(notes))
	default:
		out("  %d 篇：可以下结论了，但要附上证据和 n。\n", len(notes))
	}
	out("  发布时间最后才看：它几乎从来不是原因。\n\n")
	_ = math.NaN
	return 0
}

// ---------- xhs log ----------

func cmdLog(args []string) int {
	f := parseFlags(args, "published", "n")
	sub := f.arg(0, "list")
	path := filepath.Join(xhsHome(), "log.jsonl")
	switch sub {
	case "add":
		pp := f.arg(1, "")
		if pp == "" {
			fmt.Fprintln(os.Stderr, "用法: xhs log add post.json [--published \"2026-11-26 09:30\"]")
			return 2
		}
		p, _, err := loadPost(pp)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		title := p.Title
		if title == "" && len(p.Titles) > 0 {
			title = p.Titles[0]
		}
		tb := loadTitles()
		fid, fname := tb.classify(title)
		pub := f.vals["published"]
		if pub == "" {
			pub = time.Now().Format("2006-01-02 15:04")
		}
		entry := map[string]any{
			"logged": time.Now().Format(time.RFC3339), "published": pub, "slug": p.Slug,
			"title": title, "cover": p.Cover.Big, "formula_id": fid, "formula": fname,
			"insight": p.Insight, "testing": p.Testing, "keywords": p.Keywords, "cards": len(p.Cards),
		}
		if err := os.MkdirAll(xhsHome(), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer fh.Close()
		b, _ := json.Marshal(entry)
		fh.Write(append(b, '\n'))
		fmt.Printf("记下了：%s  ·  #%d %s  ·  发布 %s\n72 小时后（%s 左右）跑 xhs-review。\n", title, fid, fname, pub, hint72(pub))
		return 0
	case "list":
		logs := readLog()
		n := 10
		if v, ok := num(f.vals["n"]); ok && v > 0 {
			n = int(v)
		}
		if len(logs) > n {
			logs = logs[len(logs)-n:]
		}
		if len(logs) == 0 {
			fmt.Println("还没有发布记录：", path)
			return 0
		}
		for _, e := range logs {
			fmt.Printf("  %v  #%v %-8v  %v\n", e["published"], e["formula_id"], e["formula"], e["title"])
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "用法: xhs log add <post.json> | list [-n 10]")
	return 2
}

func hint72(pub string) string {
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, pub, time.Local); err == nil {
			return t.Add(72 * time.Hour).Format("01-02 15:04")
		}
	}
	return "三天后"
}
