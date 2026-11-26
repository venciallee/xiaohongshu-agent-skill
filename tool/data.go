package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// xhs data - 记录每篇笔记的数据快照（data.jsonl）。
//
// 同一篇可以记多次（发后 48 小时一次、7 天一次……），每行都带「发布后第几小时」。
// 复盘时按同一个时间点比较，避免拿 20 小时的笔记和 7 天的笔记比。
//
//	xhs data add <id|标题> --views 1500 --impressions 12000 --collects 340 ...
//	xhs data add --file rows.csv        一次记多篇，表头中英文都认，要有「标题」或「id」列
//	xhs data list [id|标题] [--json]

var metricFlags = []string{"impressions", "views", "ctr", "avg-time", "likes", "collects", "comments", "shares", "follows", "at", "source", "file"}

func pointFromValues(get func(string) string) (DataPoint, int) {
	var p DataPoint
	n := 0
	set := func(dst *float64, key string) {
		if v, ok := num(get(key)); ok {
			*dst = v
			n++
		}
	}
	set(&p.Impressions, "impressions")
	set(&p.Views, "views")
	set(&p.AvgTime, "avg_time")
	set(&p.Likes, "likes")
	set(&p.Collects, "collects")
	set(&p.Comments, "comments")
	set(&p.Shares, "shares")
	set(&p.Follows, "follows")
	if p.Impressions > 0 && p.Views > 0 {
		p.CTR = p.Views / p.Impressions
	} else if v, ok := num(get("ctr")); ok {
		if v > 1 {
			v /= 100
		}
		p.CTR = v
		n++
	}
	return p, n
}

func cmdDataStore(args []string) int {
	f := parseFlags(args, metricFlags...)
	sub := f.arg(0, "list")
	switch sub {
	case "add":
		return dataAdd(f)
	case "list":
		return dataList(f)
	}
	fmt.Fprintln(os.Stderr, "用法: xhs data add <id|标题> --views N ... | add --file rows.csv | list [id] [--json]")
	return 2
}

func dataAdd(f flags) int {
	logs := readLogEntries()
	if len(logs) == 0 {
		fmt.Fprintln(os.Stderr, "还没有发布记录。先 xhs log add post.json。")
		return 2
	}
	captured := time.Now()
	if v := f.vals["at"]; v != "" {
		t, ok := parseTime(v)
		if !ok {
			fmt.Fprintf(os.Stderr, "--at 看不懂：%s\n", v)
			return 2
		}
		captured = t
	}
	source := f.vals["source"]
	var added []DataPoint
	record := func(e *LogEntry, p DataPoint, at time.Time) {
		p.Note, p.Title = e.ID, e.Title
		p.Captured = at.Format("2006-01-02 15:04")
		if pub, ok := e.publishedAt(); ok {
			p.Hours = float64(int(at.Sub(pub).Hours()*10)) / 10
		}
		p.Source = source
		added = append(added, p)
	}

	if file := f.vals["file"]; file != "" {
		raw, err := readInput(file)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		aliases := map[string][]string{}
		for k, v := range reviewAliases {
			aliases[k] = v
		}
		aliases["id"] = []string{"note", "slug", "笔记id"}
		aliases["captured"] = []string{"采集时间", "抓取时间", "截图时间"}
		rows, _, err := readTable(raw, aliases)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		for i, m := range rows {
			key := m["id"]
			if key == "" {
				key = m["title"]
			}
			e, err := findNote(logs, key)
			if err != nil {
				fmt.Fprintf(os.Stderr, "第 %d 行：%v\n", i+1, err)
				return 2
			}
			p, n := pointFromValues(func(k string) string { return m[k] })
			if n == 0 {
				fmt.Fprintf(os.Stderr, "第 %d 行没有任何数字\n", i+1)
				return 2
			}
			at := captured
			if t, ok := parseTime(m["captured"]); ok && f.vals["at"] == "" {
				at = t // 行里自带的采集时间优先
			}
			record(e, p, at)
		}
	} else {
		key := f.arg(1, "")
		if key == "" {
			fmt.Fprintln(os.Stderr, "用法: xhs data add <id|标题> --views N --impressions N --collects N ...")
			return 2
		}
		e, err := findNote(logs, key)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		p, n := pointFromValues(func(k string) string { return f.vals[strings.ReplaceAll(k, "_", "-")] })
		if n == 0 {
			fmt.Fprintln(os.Stderr, "没有数字。至少给 --views，最好连曝光、收藏、涨粉一起给。")
			return 2
		}
		record(e, p, captured)
	}
	for _, p := range added {
		if err := appendJSONL("data.jsonl", p); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if f.bools["json"] {
		printJSON(added)
		return 0
	}
	for _, p := range added {
		fmt.Printf("记下了：%s  ·  发布后 %.0f 小时  ·  观看 %s  收藏 %s  涨粉 %s\n", truncWidth(p.Title, 30), p.Hours, fmtCount(p.Views), fmtCount(p.Collects), fmtCount(p.Follows))
	}
	return 0
}

func dataList(f flags) int {
	pts := readData()
	if key := f.arg(1, ""); key != "" {
		e, err := findNote(readLogEntries(), key)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		var keep []DataPoint
		for _, p := range pts {
			if p.Note == e.ID {
				keep = append(keep, p)
			}
		}
		pts = keep
	}
	sort.SliceStable(pts, func(i, j int) bool {
		if pts[i].Note != pts[j].Note {
			return pts[i].Note < pts[j].Note
		}
		return pts[i].Hours < pts[j].Hours
	})
	if f.bools["json"] {
		if pts == nil {
			pts = []DataPoint{}
		}
		printJSON(pts)
		return 0
	}
	if len(pts) == 0 {
		fmt.Println("还没有数据快照：", homePath("data.jsonl"))
		return 0
	}
	fmt.Printf("  %s %s %s %s %s %s  %s\n", padLeft("小时", 6), padLeft("曝光", 7), padLeft("观看", 7), padLeft("点击率", 7), padLeft("收藏", 6), padLeft("涨粉", 5), "笔记")
	for _, p := range pts {
		fmt.Printf("  %s %s %s %s %s %s  %s\n", padLeft(fmt.Sprintf("%.0f", p.Hours), 6), padLeft(fmtCount(p.Impressions), 7), padLeft(fmtCount(p.Views), 7),
			padLeft(pct(p.CTR), 7), padLeft(fmtCount(p.Collects), 6), padLeft(fmtCount(p.Follows), 5), truncWidth(p.Title, 30))
	}
	return 0
}
