package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// xhs log - 发布记录（log.jsonl）。用户在 App 里发完之后记一笔。
//
//	xhs log add post.json [--published "2026-11-26 09:30"] [--exp E3]
//	xhs log list [-n 10] [--json]

func cmdLog(args []string) int {
	f := parseFlags(args, "published", "n", "exp")
	switch f.arg(0, "list") {
	case "add":
		pp := f.arg(1, "")
		if pp == "" {
			fmt.Fprintln(os.Stderr, "用法: xhs log add post.json [--published \"2026-11-26 09:30\"] [--exp E3]")
			return 2
		}
		p, real, err := loadPost(pp)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		title := p.Title
		if title == "" && len(p.Titles) > 0 {
			title = p.Titles[0]
		}
		id := noteID(p.Slug, title)
		for _, e := range readLogEntries() {
			if e.ID == id {
				fmt.Fprintf(os.Stderr, "已经记过了：%s（%s）。同一篇不重复记。\n", id, e.Published)
				return 2
			}
		}
		pub := time.Now()
		if v := f.vals["published"]; v != "" {
			t, ok := parseTime(v)
			if !ok {
				fmt.Fprintf(os.Stderr, "--published 看不懂：%s\n", v)
				return 2
			}
			pub = t
		}
		exp := f.vals["exp"]
		if exp == "" {
			exp = p.Exp
		}
		if exp != "" {
			if _, ok := findExp(loadExps(), exp); !ok {
				fmt.Fprintf(os.Stderr, "实验 %s 不存在（xhs exp list 看有哪些）\n", exp)
				return 2
			}
		}
		fid, fname := loadTitles().classify(title)
		dir, _ := filepath.Abs(filepath.Dir(real))
		e := LogEntry{ID: id, Logged: nowStr(), Published: pub.Format("2006-01-02 15:04"), Slug: p.Slug, Title: title,
			Cover: p.Cover.Big, FormulaID: fid, Formula: fname, Category: p.Category, Exp: exp, Insight: p.Insight,
			Testing: p.Testing, Keywords: p.Keywords, Cards: len(p.Cards), Dir: dir}
		if err := appendJSONL("log.jsonl", e); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if f.bools["json"] {
			printJSON(e)
			return 0
		}
		g, _ := loadGoal()
		fmt.Printf("记下了：%s  ·  id %s  ·  #%d %s  ·  发布 %s\n", title, id, fid, fname, e.Published)
		if exp != "" {
			fmt.Printf("计入实验 %s\n", exp)
		}
		for _, h := range g.CaptureHours {
			fmt.Printf("  发布后 %d 小时（%s）记一次数据：xhs data add %s ...\n", h, pub.Add(time.Duration(h)*time.Hour).Format("01-02 15:04"), id)
		}
		return 0
	case "list":
		logs := readLogEntries()
		n := 10
		if v, ok := num(f.vals["n"]); ok && v > 0 {
			n = int(v)
		}
		if len(logs) > n {
			logs = logs[len(logs)-n:]
		}
		if f.bools["json"] {
			if logs == nil {
				logs = []LogEntry{}
			}
			printJSON(logs)
			return 0
		}
		if len(logs) == 0 {
			fmt.Println("还没有发布记录：", homePath("log.jsonl"))
			return 0
		}
		for _, e := range logs {
			exp := ""
			if e.Exp != "" {
				exp = " [" + e.Exp + "]"
			}
			fmt.Printf("  %s  %s  #%d %s  %s%s\n", e.Published, padRight(truncWidth(e.ID, 20), 20), e.FormulaID, padRight(e.Formula, 10), e.Title, exp)
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "用法: xhs log add <post.json> | list [-n 10]")
	return 2
}
