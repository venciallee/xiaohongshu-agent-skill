package main

import (
	"fmt"
	"math"
	"os"
	"strings"
)

// xhs exp - 实验账本（experiments.json）。
//
// 每次想改一样东西（标题句式、选题类型、卡片张数、发布时间……），先开一个实验：
// 写清假设、改哪个变量、看哪个指标、至少几篇。发布时 `xhs log add --exp E3`
// 把笔记挂到实验上。够篇数之后 `xhs exp show` 拿实验组和其余笔记比，
// `xhs exp close` 写结论；成立的结论用 --rule 追加进 playbook.md，
// 以后选题和写稿都读它。一次只改一个变量，不然说不清是谁起的作用。
//
//	xhs exp new --hypothesis "..." --variable "标题句式" --change "#1 直接抄" [--metric save_rate] [--min 5]
//	xhs exp list [--json]
//	xhs exp show E3 [--json]
//	xhs exp close E3 --result supported|rejected|inconclusive --decision "..." [--rule "..."]

type Experiment struct {
	ID         string `json:"id"`
	Created    string `json:"created"`
	Hypothesis string `json:"hypothesis"`
	Variable   string `json:"variable"`
	Change     string `json:"change"`
	Control    string `json:"control,omitempty"`
	Metric     string `json:"metric"`
	MinNotes   int    `json:"min_notes"`
	Status     string `json:"status"`
	Result     string `json:"result,omitempty"`
	Decision   string `json:"decision,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	Closed     string `json:"closed,omitempty"`
}

var expMetrics = map[string]struct {
	name string
	get  func(NoteStat) float64
	pct  bool
}{
	"views":       {"观看", func(n NoteStat) float64 { return n.Views }, false},
	"impressions": {"曝光", func(n NoteStat) float64 { return n.Impressions }, false},
	"ctr":         {"点击率", func(n NoteStat) float64 { return n.CTR }, true},
	"save_rate":   {"收藏率", func(n NoteStat) float64 { return n.SaveRate }, true},
	"follow_rate": {"涨粉率", func(n NoteStat) float64 { return n.FollowRate }, true},
	"follows":     {"涨粉", func(n NoteStat) float64 { return n.Follows }, false},
	"avg_time":    {"平均观看时长", func(n NoteStat) float64 { return n.AvgTime }, false},
}

func loadExps() []Experiment {
	var es []Experiment
	if _, err := readJSONFile("experiments.json", &es); err != nil {
		fmt.Fprintf(os.Stderr, "警告: experiments.json 解析失败: %v\n", err)
	}
	return es
}

func findExp(es []Experiment, id string) (int, bool) {
	for i := range es {
		if strings.EqualFold(es[i].ID, id) {
			return i, true
		}
	}
	return -1, false
}

type ExpResult struct {
	Experiment
	Notes        []string `json:"notes"`
	N            int      `json:"n"`
	Mature       int      `json:"mature"`
	BaselineN    int      `json:"baseline_n"`
	ExpMedian    float64  `json:"exp_median"`
	BaseMedian   float64  `json:"baseline_median"`
	Ratio        float64  `json:"ratio"`
	AUC          float64  `json:"auc"`
	Enough       bool     `json:"enough"`
	Reading      string   `json:"reading"`
	MetricName   string   `json:"metric_name"`
	MetricIsRate bool     `json:"metric_is_rate"`
}

func evalExp(e Experiment, g GoalConfig) ExpResult {
	r := ExpResult{Experiment: e}
	m, ok := expMetrics[e.Metric]
	if !ok {
		m = expMetrics["views"]
	}
	r.MetricName, r.MetricIsRate = m.name, m.pct
	notes := notesFromStore(g.CompareHours, loadTitles())
	var in, base []float64
	for _, l := range readLogEntries() {
		if strings.EqualFold(l.Exp, e.ID) {
			r.Notes = append(r.Notes, l.ID)
		}
	}
	r.N = len(r.Notes)
	for _, n := range notes {
		if !n.Mature {
			continue
		}
		v := m.get(n)
		if v <= 0 {
			continue
		}
		if strings.EqualFold(n.Exp, e.ID) {
			in = append(in, v)
		} else if n.Exp == "" {
			base = append(base, v)
		}
	}
	r.Mature, r.BaselineN = len(in), len(base)
	r.ExpMedian, r.BaseMedian = median(in), median(base)
	r.Ratio = ratio(r.ExpMedian, r.BaseMedian)
	r.AUC = auc(in, base)
	if math.IsNaN(r.AUC) {
		r.AUC = 0.5
	}
	r.Enough = r.Mature >= e.MinNotes && r.BaselineN >= 3
	switch {
	case !r.Enough:
		r.Reading = fmt.Sprintf("还不够：实验组可比 %d / %d 篇，对照 %d 篇（至少 3）。", r.Mature, e.MinNotes, r.BaselineN)
	case r.Ratio >= 1.15 && r.AUC >= 0.65:
		r.Reading = fmt.Sprintf("支持假设：实验组%s中位数是对照的 %.2f 倍，AUC %.2f。", m.name, r.Ratio, r.AUC)
	case r.Ratio <= 0.87 && r.AUC <= 0.35:
		r.Reading = fmt.Sprintf("不支持：实验组%s中位数只有对照的 %.2f 倍，AUC %.2f。", m.name, r.Ratio, r.AUC)
	default:
		r.Reading = fmt.Sprintf("看不出差别：%.2f 倍，AUC %.2f。再加几篇，或者承认这个变量不重要。", r.Ratio, r.AUC)
	}
	return r
}

func fmtMetric(v float64, isPct bool) string {
	if isPct {
		return pct(v)
	}
	return fmtCount(v)
}

func appendPlaybook(line string) error {
	path := homePath("playbook.md")
	if !fileExists(path) {
		if err := ensureHome(); err != nil {
			return err
		}
		head := "# Playbook\n\n验证过的规则。选题和写稿前必读；每条都带实验编号和证据，可以被新的实验推翻。\n\n## 规则\n\n"
		if err := os.WriteFile(path, []byte(head), 0o644); err != nil {
			return err
		}
	}
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.WriteString(line + "\n")
	return err
}

func cmdExp(args []string) int {
	f := parseFlags(args, "hypothesis", "variable", "change", "control", "metric", "min", "result", "decision", "rule")
	g, _ := loadGoal()
	es := loadExps()
	switch f.arg(0, "list") {
	case "new":
		h, v, c := f.vals["hypothesis"], f.vals["variable"], f.vals["change"]
		if h == "" || v == "" || c == "" {
			fmt.Fprintln(os.Stderr, "用法: xhs exp new --hypothesis \"...\" --variable \"标题句式\" --change \"#1 直接抄\" [--metric save_rate] [--min 5]")
			return 2
		}
		metric := f.vals["metric"]
		if metric == "" {
			metric = "views"
		}
		if _, ok := expMetrics[metric]; !ok {
			var ks []string
			for k := range expMetrics {
				ks = append(ks, k)
			}
			fmt.Fprintf(os.Stderr, "--metric 只能是：%s\n", strings.Join(ks, " / "))
			return 2
		}
		min := g.MinNotesPerExp
		if x, ok := num(f.vals["min"]); ok && x > 0 {
			min = int(x)
		}
		for _, e := range es {
			if e.Status == "open" && strings.EqualFold(e.Variable, v) {
				fmt.Fprintf(os.Stderr, "已经有一个改「%s」的实验在跑：%s。同一个变量同时只测一个。\n", v, e.ID)
				return 2
			}
		}
		e := Experiment{ID: fmt.Sprintf("E%d", len(es)+1), Created: nowStr(), Hypothesis: h, Variable: v, Change: c,
			Control: f.vals["control"], Metric: metric, MinNotes: min, Status: "open"}
		es = append(es, e)
		if err := writeJSONFile("experiments.json", es); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if f.bools["json"] {
			printJSON(e)
			return 0
		}
		fmt.Printf("开了实验 %s：%s\n  改：%s → %s，看 %s，至少 %d 篇\n  发布时用 xhs log add post.json --exp %s\n", e.ID, h, v, c, expMetrics[metric].name, min, e.ID)
		open := 0
		for _, x := range es {
			if x.Status == "open" {
				open++
			}
		}
		if open > 1 {
			fmt.Printf("  注意：现在有 %d 个实验同时在跑。同一篇只能挂一个，变量之间也会互相干扰。\n", open)
		}
		return 0
	case "list":
		var rs []ExpResult
		for _, e := range es {
			rs = append(rs, evalExp(e, g))
		}
		if f.bools["json"] {
			if rs == nil {
				rs = []ExpResult{}
			}
			printJSON(rs)
			return 0
		}
		if len(rs) == 0 {
			fmt.Println("还没有实验。xhs exp new --hypothesis ... 开一个。")
			return 0
		}
		for _, r := range rs {
			state := r.Status
			if r.Status == "open" {
				state = fmt.Sprintf("进行中 %d/%d 篇", r.N, r.MinNotes)
			} else {
				state = "已结束：" + r.Result
			}
			fmt.Printf("  %s  %s  %s → %s  ·  %s\n      %s\n", r.ID, padRight(state, 16), r.Variable, r.Change, r.Hypothesis, r.Reading)
		}
		return 0
	case "show":
		i, ok := findExp(es, f.arg(1, ""))
		if !ok {
			fmt.Fprintln(os.Stderr, "用法: xhs exp show E3")
			return 2
		}
		r := evalExp(es[i], g)
		if f.bools["json"] {
			printJSON(r)
			return 0
		}
		fmt.Printf("\n实验 %s  ·  %s\n%s\n", r.ID, r.Status, strings.Repeat("=", 60))
		fmt.Printf("  假设      %s\n  改        %s → %s\n  看        %s（发布后约 %.0f 小时）\n", r.Hypothesis, r.Variable, r.Change, r.MetricName, g.CompareHours)
		fmt.Printf("  实验组    %d 篇已挂，%d 篇可比，中位数 %s\n", r.N, r.Mature, fmtMetric(r.ExpMedian, r.MetricIsRate))
		fmt.Printf("  对照      %d 篇（没挂实验的笔记），中位数 %s\n", r.BaselineN, fmtMetric(r.BaseMedian, r.MetricIsRate))
		fmt.Printf("  读数      %s\n\n", r.Reading)
		return 0
	case "close":
		i, ok := findExp(es, f.arg(1, ""))
		res := f.vals["result"]
		if !ok || (res != "supported" && res != "rejected" && res != "inconclusive") {
			fmt.Fprintln(os.Stderr, "用法: xhs exp close E3 --result supported|rejected|inconclusive --decision \"...\" [--rule \"...\"]")
			return 2
		}
		if es[i].Status != "open" {
			fmt.Fprintf(os.Stderr, "%s 已经结束了。\n", es[i].ID)
			return 2
		}
		r := evalExp(es[i], g)
		es[i].Status, es[i].Result, es[i].Decision, es[i].Closed = "closed", res, f.vals["decision"], nowStr()
		es[i].Evidence = fmt.Sprintf("实验组 n=%d %s中位 %s vs 对照 n=%d %s（%.2f 倍，AUC %.2f）",
			r.Mature, r.MetricName, fmtMetric(r.ExpMedian, r.MetricIsRate), r.BaselineN, fmtMetric(r.BaseMedian, r.MetricIsRate), r.Ratio, r.AUC)
		if rule := f.vals["rule"]; rule != "" {
			if res != "supported" {
				fmt.Fprintln(os.Stderr, "注意：结论不是 supported 还要写规则。这条会标成「未验证」。")
			}
			es[i].Rule = rule
			tag := ""
			if res != "supported" {
				tag = "（未验证）"
			}
			line := fmt.Sprintf("- %s%s　[%s · %s · %s]", rule, tag, es[i].ID, today(), es[i].Evidence)
			if err := appendPlaybook(line); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 1
			}
		}
		if err := writeJSONFile("experiments.json", es); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if f.bools["json"] {
			printJSON(es[i])
			return 0
		}
		fmt.Printf("%s 结束：%s。%s\n", es[i].ID, res, es[i].Evidence)
		if es[i].Rule != "" {
			fmt.Println("已追加到 playbook.md：" + es[i].Rule)
		}
		if !r.Enough {
			fmt.Println("注意：样本还不够就结束了，结论要打折扣。")
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "用法: xhs exp new|list|show|close")
	return 2
}
