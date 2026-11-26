package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// xhs review - 复盘。
//
// 只认这个账号自己的数据。所有「好 / 差」都是和本账号笔记的中位数比，
// 不用网上流传的「点击率低于 X% 就是差」这种绝对线：那些数字没有出处，
// 而且不同领域差得很远。
//
// 漏斗拆成四段，先找最先掉下去的那一段，后面的都是它的下游：
//
//	曝光  ->  点击（观看 / 曝光）  ->  留下（收藏率、平均观看时长）  ->  关注（涨粉 / 观看）
//	分发问题        封面标题问题              内容兑现问题                   主页问题
//
// 再往上一层是选题：同一类题换了几种句式都起不来，问题在题本身，不在包装。
//
//	xhs review                 读 log.jsonl + data.jsonl，按发布后同一时间点比较
//	xhs review notes.csv       直接读一张表（创作中心抄下来的）
//	xhs review --at 48         比较时间点（小时），默认 goal.json 的 compare_hours
//	xhs review --record        把这次的摘要写进 reviews.jsonl（xhs status 用它判断有没有起色）
//	xhs review --md | --json

var reviewAliases = map[string][]string{
	"date":        {"日期", "发布日期", "发布时间", "published"},
	"title":       {"标题", "笔记", "笔记标题"},
	"formula":     {"句式", "formula_id"},
	"category":    {"类型", "选题类型", "分类"},
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
	ID          string   `json:"id,omitempty"`
	Date        string   `json:"date"`
	Title       string   `json:"title"`
	FormulaID   int      `json:"formula_id"`
	Formula     string   `json:"formula"`
	Category    string   `json:"category,omitempty"`
	Exp         string   `json:"exp,omitempty"`
	Testing     string   `json:"testing,omitempty"`
	AgeHours    float64  `json:"age_hours"`
	Mature      bool     `json:"mature"`
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

func (n *NoteStat) derive() {
	if n.CTR == 0 && n.Impressions > 0 && n.Views > 0 {
		n.CTR = n.Views / n.Impressions
	}
	n.SaveRate = ratio(n.Collects, n.Views)
	n.EngageRate = ratio(n.Likes+n.Collects+n.Comments+n.Shares, n.Views)
	n.FollowRate = ratio(n.Follows, n.Views)
	n.CommentRate = ratio(n.Comments, n.Views)
}

// notesFromStore joins log.jsonl with data.jsonl at a fixed age.
func notesFromStore(at float64, tb *TitleBook) []NoteStat {
	logs := readLogEntries()
	pts := readData()
	var out []NoteStat
	for _, e := range logs {
		p, ok := snapshotAt(pts, e.ID, at)
		if !ok {
			continue
		}
		n := NoteStat{ID: e.ID, Date: e.Published, Title: e.Title, FormulaID: e.FormulaID, Formula: e.Formula,
			Category: e.Category, Exp: e.Exp, Testing: e.Testing, AgeHours: p.Hours,
			Impressions: p.Impressions, Views: p.Views, CTR: p.CTR, AvgTime: p.AvgTime, Likes: p.Likes,
			Collects: p.Collects, Comments: p.Comments, Shares: p.Shares, Follows: p.Follows}
		if n.Formula == "" {
			n.FormulaID, n.Formula = tb.classify(n.Title)
		}
		n.Mature = p.Hours >= at*0.75
		n.derive()
		out = append(out, n)
	}
	return out
}

func notesFromTable(raw string, tb *TitleBook) ([]NoteStat, []string, error) {
	rows, unknown, err := readTable(raw, reviewAliases)
	if err != nil {
		return nil, nil, err
	}
	byTitle := map[string]LogEntry{}
	for _, e := range readLogEntries() {
		byTitle[e.Title] = e
	}
	var notes []NoteStat
	for _, m := range rows {
		n := NoteStat{Date: m["date"], Title: m["title"], Category: m["category"], Mature: true, AgeHours: -1}
		n.Impressions, _ = num(m["impressions"])
		n.Views, _ = num(m["views"])
		n.AvgTime, _ = num(m["avg_time"])
		n.Likes, _ = num(m["likes"])
		n.Collects, _ = num(m["collects"])
		n.Comments, _ = num(m["comments"])
		n.Shares, _ = num(m["shares"])
		n.Follows, _ = num(m["follows"])
		if !(n.Impressions > 0 && n.Views > 0) {
			if v, ok := num(m["ctr"]); ok {
				if v > 1 {
					v /= 100
				}
				n.CTR = v
			}
		}
		n.FormulaID, n.Formula = tb.classify(n.Title)
		if e, ok := byTitle[n.Title]; ok {
			n.ID, n.Testing, n.Exp = e.ID, e.Testing, e.Exp
			if n.Category == "" {
				n.Category = e.Category
			}
		}
		n.derive()
		notes = append(notes, n)
	}
	return notes, unknown, nil
}

func noteMedians(notes []NoteStat) map[string]float64 {
	col := func(fn func(NoteStat) float64) []float64 {
		var xs []float64
		for _, n := range notes {
			if !n.Mature {
				continue
			}
			if v := fn(n); v > 0 {
				xs = append(xs, v)
			}
		}
		return xs
	}
	return map[string]float64{
		"impressions": median(col(func(n NoteStat) float64 { return n.Impressions })),
		"views":       median(col(func(n NoteStat) float64 { return n.Views })),
		"ctr":         median(col(func(n NoteStat) float64 { return n.CTR })),
		"avg_time":    median(col(func(n NoteStat) float64 { return n.AvgTime })),
		"save":        median(col(func(n NoteStat) float64 { return n.SaveRate })),
		"follow":      median(col(func(n NoteStat) float64 { return n.FollowRate })),
		"comment":     median(col(func(n NoteStat) float64 { return n.CommentRate })),
	}
}

func diagnose(notes []NoteStat, med map[string]float64) {
	mature := 0
	for _, n := range notes {
		if n.Mature {
			mature++
		}
	}
	for i := range notes {
		n := &notes[i]
		n.Multiple = ratio(n.Views, med["views"])
		if mature < 3 || !n.Mature {
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
}

type groupStat struct {
	Name     string  `json:"name"`
	N        int     `json:"n"`
	Median   float64 `json:"median_multiple"`
	Formulas int     `json:"formulas"`
}

func groupBy(notes []NoteStat, key func(NoteStat) string) []groupStat {
	type acc struct {
		xs       []float64
		formulas map[int]bool
	}
	m := map[string]*acc{}
	for _, n := range notes {
		if !n.Mature {
			continue
		}
		k := key(n)
		if k == "" {
			continue
		}
		if m[k] == nil {
			m[k] = &acc{formulas: map[int]bool{}}
		}
		m[k].xs = append(m[k].xs, n.Multiple)
		m[k].formulas[n.FormulaID] = true
	}
	var out []groupStat
	for k, a := range m {
		out = append(out, groupStat{Name: k, N: len(a.xs), Median: median(a.xs), Formulas: len(a.formulas)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Median > out[j].Median })
	return out
}

// topicFindings: a category that stays low across several different title
// formulas is a topic problem, not a packaging problem.
func topicFindings(cats []groupStat) []string {
	var out []string
	for _, c := range cats {
		if c.N >= 3 && c.Formulas >= 2 && c.Median < 0.7 {
			out = append(out, fmt.Sprintf("选题：「%s」类 %d 篇、换过 %d 种标题句式，倍数中位数仍只有 %.1fx。问题更可能在题本身，不在封面标题。下一轮先回 xhs-audience 看读者到底要不要这类题。", c.Name, c.N, c.Formulas, c.Median))
		}
		if c.N >= 3 && c.Median >= 1.5 {
			out = append(out, fmt.Sprintf("选题：「%s」类 %d 篇，倍数中位数 %.1fx，是目前最稳的一类。", c.Name, c.N, c.Median))
		}
	}
	return out
}

func recentMedians(notes []NoteStat, days float64, now time.Time) (map[string]float64, int) {
	var recent []NoteStat
	for _, n := range notes {
		if t, ok := parseTime(n.Date); ok && now.Sub(t).Hours() <= days*24 {
			recent = append(recent, n)
		}
	}
	m := noteMedians(recent)
	c := 0
	for _, n := range recent {
		if n.Mature {
			c++
		}
	}
	return m, c
}

func cmdReview(args []string) int {
	f := parseFlags(args, "at")
	tb := loadTitles()
	g, _ := loadGoal()
	at := g.CompareHours
	if v, ok := num(f.vals["at"]); ok && v > 0 {
		at = v
	}
	var notes []NoteStat
	var unknown []string
	source := ""
	if path := f.arg(0, ""); path != "" {
		raw, err := readInput(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		notes, unknown, err = notesFromTable(raw, tb)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		source = path
	} else {
		notes = notesFromStore(at, tb)
		source = fmt.Sprintf("log.jsonl + data.jsonl，按发布后约 %.0f 小时比较", at)
		if len(notes) == 0 {
			fmt.Fprintln(os.Stderr, "没有可复盘的数据：先 xhs log add 记发布，再 xhs data add 记数据；或者直接 xhs review notes.csv。")
			return 2
		}
	}

	med := noteMedians(notes)
	diagnose(notes, med)
	mature := 0
	for _, n := range notes {
		if n.Mature {
			mature++
		}
	}
	cats := groupBy(notes, func(n NoteStat) string { return n.Category })
	forms := groupBy(notes, func(n NoteStat) string {
		if n.FormulaID == 0 {
			return "未归类"
		}
		return fmt.Sprintf("#%d %s", n.FormulaID, n.Formula)
	})
	findings := topicFindings(cats)
	now := time.Now()
	recent, recentN := recentMedians(notes, g.RecentDays, now)
	tr := trajectory(g, accountPoints(), now)

	if f.bools["record"] {
		rec := ReviewRecord{At: nowStr(), Notes: len(notes), Mature: mature, CompareHours: at, Medians: med,
			Recent: recent, RecentNotes: recentN, Followers: tr.Current, FollowersPerDay: tr.PerDay, Findings: findings}
		for _, n := range notes {
			for _, d := range n.Diagnosis {
				rec.Findings = append(rec.Findings, n.Title+"｜"+d)
			}
		}
		if err := appendJSONL("reviews.jsonl", rec); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}

	sorted := append([]NoteStat(nil), notes...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Multiple > sorted[j].Multiple })

	if f.bools["json"] {
		printJSON(map[string]any{"source": source, "compare_hours": at, "notes": sorted, "medians": med,
			"recent": recent, "recent_notes": recentN, "categories": cats, "formulas": forms,
			"findings": findings, "trajectory": tr, "recorded": f.bools["record"]})
		return 0
	}

	out := func(format string, a ...any) { fmt.Printf(format, a...) }
	if f.bools["md"] {
		out("## 复盘 %s · %d 篇\n\n", today(), len(notes))
		out("中位数：曝光 %s，观看 %s，点击率 %s，收藏率 %s，涨粉率 %s\n\n", fmtCount(med["impressions"]), fmtCount(med["views"]), pct(med["ctr"]), pct(med["save"]), pct(med["follow"]))
		out("| 倍数 | 点击率 | 收藏率 | 涨粉率 | 句式 | 类型 | 标题 |\n|---|---|---|---|---|---|---|\n")
		for _, n := range sorted {
			out("| %.1fx | %s | %s | %s | #%d | %s | %s |\n", n.Multiple, pct(n.CTR), pct(n.SaveRate), pct(n.FollowRate), n.FormulaID, n.Category, n.Title)
		}
		out("\n")
		for _, fd := range findings {
			out("- %s\n", fd)
		}
		for _, n := range sorted {
			for _, d := range n.Diagnosis {
				out("- **%s**：%s\n", n.Title, d)
			}
		}
		return 0
	}

	out("\n复盘  ·  %d 篇（可比 %d 篇）  ·  %s\n", len(notes), mature, source)
	out("  中位数：曝光 %s / 观看 %s / 点击率 %s / 收藏率 %s / 涨粉率 %s\n",
		fmtCount(med["impressions"]), fmtCount(med["views"]), pct(med["ctr"]), pct(med["save"]), pct(med["follow"]))
	if len(unknown) > 0 {
		out("  （没认出的列：%s）\n", strings.Join(unknown, "、"))
	}
	out("%s\n", strings.Repeat("=", 90))
	out("  %s %s %s %s %s %s  %s\n", padLeft("倍数", 6), padLeft("曝光", 7), padLeft("点击率", 7), padLeft("收藏率", 7), padLeft("涨粉率", 7), padRight("句式", 14), "标题")
	for _, n := range sorted {
		fid := "-"
		if n.FormulaID > 0 {
			fid = fmt.Sprintf("#%d %s", n.FormulaID, n.Formula)
		}
		mark := ""
		if !n.Mature {
			mark = fmt.Sprintf("（才 %.0f 小时，不参与比较）", n.AgeHours)
		}
		out("  %s %s %s %s %s %s  %s%s\n", padLeft(fmt.Sprintf("%.1fx", n.Multiple), 6), padLeft(fmtCount(n.Impressions), 7), padLeft(pct(n.CTR), 7),
			padLeft(pct(n.SaveRate), 7), padLeft(pct(n.FollowRate), 7), padRight(truncWidth(fid, 14), 14), truncWidth(n.Title, 30), mark)
	}
	out("\n诊断（和本账号中位数比）\n")
	if mature < 3 {
		out("  可比的不到 3 篇，没有中位数可比，只列数据。\n")
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
	if mature >= 3 && !any {
		out("  没有明显掉下去的环节，这批都在本账号的正常范围内。\n")
	}
	if len(findings) > 0 {
		out("\n选题层面\n")
		for _, fd := range findings {
			out("  - %s\n", fd)
		}
	}
	if len(cats) > 1 {
		out("\n按选题类型（倍数中位数，n = 篇数）\n")
		for _, c := range cats {
			out("  %s  %.1fx  n=%d  句式 %d 种\n", padRight(c.Name, 16), c.Median, c.N, c.Formulas)
		}
	}
	if mature >= 6 && len(forms) > 1 {
		out("\n按句式\n")
		for _, c := range forms {
			out("  %s  %.1fx  n=%d\n", padRight(truncWidth(c.Name, 16), 16), c.Median, c.N)
		}
	}
	if tr.Points > 0 {
		out("\n目标：%s / %s，每天 %+.1f", fmtCount(tr.Current), fmtCount(g.Target), tr.PerDay)
		if tr.ETA != "" {
			out("，按这个速度 %s 到", tr.ETA)
		}
		out("\n")
	}
	out("\n%s\n", strings.Repeat("-", 90))
	switch {
	case mature < 10:
		out("  %d 篇：只能看单篇哪一段掉了，看不出规律。别据此改方向。\n", mature)
	case mature < 30:
		out("  %d 篇：能看出线索。结论写成「下 5 篇验证 X」（xhs exp new），不要写成「X 有效」。\n", mature)
	default:
		out("  %d 篇：可以下结论了，但要附上证据和 n。\n", mature)
	}
	out("  发布时间最后才看：它几乎从来不是原因。\n")
	if f.bools["record"] {
		out("  已写入 reviews.jsonl。\n")
	}
	out("\n")
	return 0
}
