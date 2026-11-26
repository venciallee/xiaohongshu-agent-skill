package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// xhs pool - 候选选题池（pool.json）。
//
// 每天扫信息源时，过了洞察三问的候选都先进池子，按 topic.json 的打分表算总分。
// 当天没用上的好题留着，过期（默认 14 天）自动不再推荐：新东西的窗口很短。
//
//	xhs pool add --title "..." --insight "..." --source "..." --scores "有用=3,新=2,..." [--category ...] [--expires 14]
//	xhs pool list [--all] [--json]
//	xhs pool use P3 [--slug xxx]
//	xhs pool drop P3

type PoolItem struct {
	ID       string         `json:"id"`
	Added    string         `json:"added"`
	Title    string         `json:"title"`
	Insight  string         `json:"insight"`
	Source   string         `json:"source,omitempty"`
	Category string         `json:"category,omitempty"`
	Scores   map[string]int `json:"scores"`
	Total    int            `json:"total"`
	Pass     bool           `json:"pass"`
	Expires  string         `json:"expires,omitempty"`
	Status   string         `json:"status"`
	UsedBy   string         `json:"used_by,omitempty"`
}

func loadPool() []PoolItem {
	var ps []PoolItem
	if _, err := readJSONFile("pool.json", &ps); err != nil {
		fmt.Fprintf(os.Stderr, "警告: pool.json 解析失败: %v\n", err)
	}
	return ps
}

func (p PoolItem) expired(now time.Time) bool {
	if t, ok := parseTime(p.Expires); ok {
		return now.After(t.Add(24 * time.Hour))
	}
	return false
}

// scoreTopic validates "有用=3,新=2" against the rubric and returns the total.
func scoreTopic(spec string, r TopicRubric) (map[string]int, int, error) {
	scores := map[string]int{}
	for _, part := range splitList(spec) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			k, v, ok = strings.Cut(part, ":")
		}
		if !ok {
			return nil, 0, fmt.Errorf("「%s」要写成 项=分", part)
		}
		x, ok2 := num(v)
		if !ok2 {
			return nil, 0, fmt.Errorf("「%s」的分数不是数字", part)
		}
		scores[strings.TrimSpace(k)] = int(x)
	}
	total := 0
	var missing []string
	for _, it := range r.Items {
		v, ok := scores[it.Key]
		if !ok {
			missing = append(missing, it.Key)
			continue
		}
		if v < 0 || v > it.Max {
			return nil, 0, fmt.Errorf("「%s」要在 0-%d 之间，给的是 %d", it.Key, it.Max, v)
		}
		total += v
	}
	if len(missing) > 0 {
		return nil, 0, fmt.Errorf("打分表里这些项没打分：%s", strings.Join(missing, "、"))
	}
	for k := range scores {
		found := false
		for _, it := range r.Items {
			if it.Key == k {
				found = true
			}
		}
		if !found {
			return nil, 0, fmt.Errorf("打分表里没有「%s」这一项（topic.json）", k)
		}
	}
	return scores, total, nil
}

func cmdPool(args []string) int {
	f := parseFlags(args, "title", "insight", "source", "category", "scores", "expires", "slug")
	rubric, _ := loadTopicRubric()
	ps := loadPool()
	now := time.Now()
	maxTotal := 0
	for _, it := range rubric.Items {
		maxTotal += it.Max
	}
	switch f.arg(0, "list") {
	case "add":
		title, insight := f.vals["title"], f.vals["insight"]
		if title == "" || insight == "" || f.vals["scores"] == "" {
			var keys []string
			for _, it := range rubric.Items {
				keys = append(keys, fmt.Sprintf("%s=0-%d", it.Key, it.Max))
			}
			fmt.Fprintf(os.Stderr, "用法: xhs pool add --title \"...\" --insight \"洞察句\" --source \"出处\" --scores \"%s\"\n", strings.Join(keys, ","))
			return 2
		}
		scores, total, err := scoreTopic(f.vals["scores"], rubric)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		days := 14.0
		if v, ok := num(f.vals["expires"]); ok && v > 0 {
			days = v
		}
		for _, p := range ps {
			if p.Status == "open" && p.Title == title {
				fmt.Fprintf(os.Stderr, "池子里已经有了：%s %s\n", p.ID, p.Title)
				return 2
			}
		}
		item := PoolItem{ID: fmt.Sprintf("P%d", len(ps)+1), Added: nowStr(), Title: title, Insight: insight,
			Source: f.vals["source"], Category: f.vals["category"], Scores: scores, Total: total,
			Pass: total >= rubric.Threshold, Expires: now.AddDate(0, 0, int(days)).Format("2006-01-02"), Status: "open"}
		ps = append(ps, item)
		if err := writeJSONFile("pool.json", ps); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if f.bools["json"] {
			printJSON(item)
			return 0
		}
		verdict := "过线"
		if !item.Pass {
			verdict = fmt.Sprintf("没过线（及格 %d）", rubric.Threshold)
		}
		fmt.Printf("入池 %s  %d/%d %s  ·  %s\n", item.ID, total, maxTotal, verdict, title)
		return 0
	case "list":
		var show []PoolItem
		for _, p := range ps {
			if f.bools["all"] || (p.Status == "open" && !p.expired(now)) {
				show = append(show, p)
			}
		}
		sort.SliceStable(show, func(i, j int) bool {
			if show[i].Pass != show[j].Pass {
				return show[i].Pass
			}
			if show[i].Total != show[j].Total {
				return show[i].Total > show[j].Total
			}
			return show[i].Added > show[j].Added
		})
		if f.bools["json"] {
			if show == nil {
				show = []PoolItem{}
			}
			printJSON(show)
			return 0
		}
		if len(show) == 0 {
			fmt.Println("池子是空的（或者都过期 / 用掉了）。--all 看全部。")
			return 0
		}
		for _, p := range show {
			mark := "  "
			if p.Pass {
				mark = "✓ "
			}
			state := ""
			if p.Status != "open" {
				state = "  [" + p.Status + "]"
			} else if p.expired(now) {
				state = "  [过期]"
			}
			fmt.Printf("%s%s  %2d/%d  %s%s\n      洞察：%s\n", mark, padRight(p.ID, 4), p.Total, maxTotal, p.Title, state, p.Insight)
		}
		return 0
	case "use", "drop":
		id := f.arg(1, "")
		for i := range ps {
			if strings.EqualFold(ps[i].ID, id) {
				if f.arg(0, "") == "use" {
					ps[i].Status, ps[i].UsedBy = "used", f.vals["slug"]
				} else {
					ps[i].Status = "dropped"
				}
				if err := writeJSONFile("pool.json", ps); err != nil {
					fmt.Fprintln(os.Stderr, err)
					return 1
				}
				if f.bools["json"] {
					printJSON(ps[i])
				} else {
					fmt.Printf("%s → %s\n", ps[i].ID, ps[i].Status)
				}
				return 0
			}
		}
		fmt.Fprintf(os.Stderr, "池子里没有 %s\n", id)
		return 2
	}
	fmt.Fprintln(os.Stderr, "用法: xhs pool add|list|use|drop")
	return 2
}
