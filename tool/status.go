package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// xhs status - 现在该做什么。
//
// 编排器（定时任务、Duet、cron、人）每次启动先跑 `xhs status --json`，按 next 里的
// 动作往下做，不依赖对话记忆。它只读状态文件、只给建议；节奏和闸门由编排器决定。
//
// 它看这些东西：
//   配置齐不齐（voice.md / goal.json / sources.md …）
//   草稿（posts/*/post.json 里还没 log 的）和今天发了没有
//   哪些已发笔记到了该记数据的时间点（goal.json 的 capture_hours）
//   离上次复盘几天（review_every_days）、连续几次复盘没起色（stall）
//   实验够不够篇数、选题池里还有什么、目标轨迹

type Action struct {
	Action  string `json:"action"`
	Reason  string `json:"reason"`
	Skill   string `json:"skill,omitempty"`
	Command string `json:"command,omitempty"`
}

type DueCapture struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	AtHours  int     `json:"at_hours"`
	AgeHours float64 `json:"age_hours"`
	Late     bool    `json:"late"`
}

type Draft struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
	Date  string `json:"date"`
}

type StallInfo struct {
	Streak    int      `json:"streak"`
	Threshold int      `json:"threshold"`
	Stalled   bool     `json:"stalled"`
	Reviews   int      `json:"reviews"`
	Last      []string `json:"last_comparison,omitempty"`
}

type Status struct {
	Now         string       `json:"now"`
	Home        string       `json:"home"`
	Missing     []string     `json:"missing_config"`
	Present     []string     `json:"present_config"`
	Goal        Trajectory   `json:"goal"`
	Published   int          `json:"published"`
	LastPost    *LogEntry    `json:"last_published,omitempty"`
	PostsToday  int          `json:"posts_today"`
	Drafts      []Draft      `json:"drafts"`
	DataDue     []DueCapture `json:"data_due"`
	LastReview  string       `json:"last_review,omitempty"`
	DaysSince   float64      `json:"days_since_review"`
	ReviewDue   bool         `json:"review_due"`
	Stall       StallInfo    `json:"stall"`
	Experiments []ExpResult  `json:"open_experiments"`
	PoolOpen    int          `json:"pool_open"`
	PoolTop     []PoolItem   `json:"pool_top"`
	AudienceAge float64      `json:"audience_age_days"`
	Next        []Action     `json:"next"`
}

func improved(prev, cur ReviewRecord, minGain float64) (bool, []string) {
	var notes []string
	ok := false
	cmp := func(name string, a, b float64) {
		if a <= 0 || b <= 0 {
			return
		}
		chg := b/a - 1
		notes = append(notes, fmt.Sprintf("%s %+.0f%%", name, chg*100))
		if chg >= minGain {
			ok = true
		}
	}
	cmp("近期观看中位数", prev.Recent["views"], cur.Recent["views"])
	cmp("近期涨粉率", prev.Recent["follow"], cur.Recent["follow"])
	cmp("每天涨粉", prev.FollowersPerDay, cur.FollowersPerDay)
	return ok, notes
}

func stallInfo(g GoalConfig) StallInfo {
	rs := readJSONL[ReviewRecord]("reviews.jsonl")
	si := StallInfo{Threshold: g.Stall.Reviews, Reviews: len(rs)}
	for i := len(rs) - 1; i >= 1; i-- {
		ok, notes := improved(rs[i-1], rs[i], g.Stall.MinGain)
		if i == len(rs)-1 {
			si.Last = notes
		}
		if ok {
			break
		}
		si.Streak++
	}
	si.Stalled = si.Threshold > 0 && si.Streak >= si.Threshold
	return si
}

func scanDrafts(logged map[string]bool) []Draft {
	var out []Draft
	matches, _ := filepath.Glob(filepath.Join(xhsHome(), "posts", "*", "post.json"))
	for _, m := range matches {
		p, _, err := loadPost(m)
		if err != nil {
			continue
		}
		title := p.Title
		if title == "" && len(p.Titles) > 0 {
			title = p.Titles[0]
		}
		id := noteID(p.Slug, title)
		if logged[id] {
			continue
		}
		d := Draft{ID: id, Title: title, Path: m, Date: p.Date}
		if d.Date == "" {
			if st, err := os.Stat(m); err == nil {
				d.Date = st.ModTime().Format("2006-01-02")
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date > out[j].Date })
	return out
}

func buildStatus() Status {
	now := time.Now()
	g, goalOK := loadGoal()
	st := Status{Now: nowStr(), Home: xhsHome(), Drafts: []Draft{}, DataDue: []DueCapture{}, Experiments: []ExpResult{}, PoolTop: []PoolItem{}, Next: []Action{}}

	// 配置
	for _, name := range []string{"voice.md", "goal.json", "sources.md", "house.json", "topic.json", "audience.md", "playbook.md"} {
		if fileExists(homePath(name)) {
			st.Present = append(st.Present, name)
		} else {
			st.Missing = append(st.Missing, name)
		}
	}
	add := func(a Action) { st.Next = append(st.Next, a) }
	if !fileExists(homePath("voice.md")) {
		add(Action{"setup", "没有 voice.md：所有 skill 都不知道这个号写给谁、怎么说话", "", "xhs init，然后填 voice.md"})
	}
	if !goalOK || g.Target <= 0 {
		add(Action{"setup", "goal.json 没有目标：复盘没法回答「离目标还有多远」", "", "xhs init，然后填 goal.json 的 target"})
	}
	if !fileExists(homePath("sources.md")) {
		add(Action{"setup", "没有 sources.md：选题不知道去哪找", "xhs-topic", "xhs init，然后填 sources.md"})
	}

	// 发布记录、今天、草稿
	logs := readLogEntries()
	st.Published = len(logs)
	logged := map[string]bool{}
	for i := range logs {
		logged[logs[i].ID] = true
		if t, ok := logs[i].publishedAt(); ok && t.Format("2006-01-02") == today() {
			st.PostsToday++
		}
	}
	if len(logs) > 0 {
		st.LastPost = &logs[len(logs)-1]
	}
	st.Drafts = scanDrafts(logged)

	// 该记数据的
	pts := readData()
	maxH := 0
	for _, h := range g.CaptureHours {
		if h > maxH {
			maxH = h
		}
	}
	for _, e := range logs {
		age := e.ageHours(now)
		if age < 0 || age > float64(maxH)*2+48 {
			continue
		}
		for _, h := range g.CaptureHours {
			hf := float64(h)
			if age < hf {
				continue
			}
			upper := hf * 1.5
			if upper < hf+24 {
				upper = hf + 24
			}
			have := false
			for _, p := range pts {
				if p.Note == e.ID && p.Hours >= hf*0.9 && p.Hours <= upper {
					have = true
					break
				}
			}
			if !have {
				st.DataDue = append(st.DataDue, DueCapture{ID: e.ID, Title: e.Title, AtHours: h, AgeHours: float64(int(age)), Late: age > upper})
			}
		}
	}

	// 粉丝数
	acc := accountPoints()
	st.Goal = trajectory(g, acc, now)
	accStale := true
	if len(acc) > 0 {
		if t, ok := parseTime(acc[len(acc)-1].Captured); ok && now.Sub(t).Hours() < 36 {
			accStale = false
		}
	}

	// 复盘
	rs := readJSONL[ReviewRecord]("reviews.jsonl")
	if len(rs) > 0 {
		st.LastReview = rs[len(rs)-1].At
		if t, ok := parseTime(st.LastReview); ok {
			st.DaysSince = float64(int(now.Sub(t).Hours()/24*10)) / 10
		}
	}
	matureNotes := 0
	for _, n := range notesFromStore(g.CompareHours, loadTitles()) {
		if n.Mature {
			matureNotes++
		}
	}
	st.ReviewDue = matureNotes > 0 && (len(rs) == 0 || st.DaysSince >= g.ReviewEveryDays)
	st.Stall = stallInfo(g)

	// 实验、选题池、读者画像
	for _, e := range loadExps() {
		if e.Status == "open" {
			st.Experiments = append(st.Experiments, evalExp(e, g))
		}
	}
	for _, p := range loadPool() {
		if p.Status == "open" && !p.expired(now) {
			st.PoolOpen++
			if p.Pass && len(st.PoolTop) < 3 {
				st.PoolTop = append(st.PoolTop, p)
			}
		}
	}
	sort.SliceStable(st.PoolTop, func(i, j int) bool { return st.PoolTop[i].Total > st.PoolTop[j].Total })
	st.AudienceAge = -1
	if fi, err := os.Stat(homePath("audience.md")); err == nil {
		st.AudienceAge = float64(int(now.Sub(fi.ModTime()).Hours() / 24))
	}

	// 下一步（按优先级）
	if len(st.DataDue) > 0 {
		var ids []string
		for _, d := range st.DataDue {
			ids = append(ids, fmt.Sprintf("%s@%dh", d.ID, d.AtHours))
		}
		add(Action{"capture_data", fmt.Sprintf("%d 个数据点到时间了：%s", len(st.DataDue), strings.Join(uniq(ids), "、")), "xhs-review", "xhs data add <id> --views ... 或 --file rows.csv"})
	}
	if accStale && len(logs) > 0 {
		add(Action{"log_followers", "超过 36 小时没记粉丝数，目标轨迹会不准", "xhs-review", "xhs goal log --followers N"})
	}
	if st.ReviewDue {
		why := fmt.Sprintf("距上次复盘 %.1f 天（每 %.0f 天一次）", st.DaysSince, g.ReviewEveryDays)
		if len(rs) == 0 {
			why = fmt.Sprintf("还没复盘过，已有 %d 篇可比", matureNotes)
		}
		add(Action{"review", why, "xhs-review", "xhs review --record"})
	}
	if st.Stall.Stalled {
		add(Action{"deep_diagnosis", fmt.Sprintf("连续 %d 次复盘没起色（%s）。别再逐篇小修：重做读者分析、重跑对标，只提一个原因一个实验", st.Stall.Streak, strings.Join(st.Stall.Last, "，")), "xhs-review", "xhs-audience → xhs-viral → xhs exp new"})
	}
	for _, e := range st.Experiments {
		if e.Enough {
			add(Action{"close_experiment", fmt.Sprintf("%s 够篇数了：%s", e.ID, e.Reading), "xhs-review", "xhs exp show " + e.ID + " 然后 xhs exp close"})
		}
	}
	if len(st.Experiments) == 0 && matureNotes >= 6 {
		add(Action{"start_experiment", "没有在跑的实验：数据只在积累，没在验证任何东西", "xhs-review", "xhs exp new --hypothesis ..."})
	}
	if len(st.Drafts) > 0 {
		add(Action{"await_publish", fmt.Sprintf("%d 篇草稿还没记发布（最新：%s）。用户发了就 xhs log add", len(st.Drafts), st.Drafts[0].Title), "xhs-note", "xhs log add " + st.Drafts[0].Path})
	}
	draftToday := false
	for _, d := range st.Drafts {
		if d.Date == today() {
			draftToday = true
		}
	}
	if st.PostsToday == 0 && !draftToday {
		reason := "今天还没有稿子"
		if st.PoolOpen > 0 {
			reason += fmt.Sprintf("；选题池里有 %d 个候选", st.PoolOpen)
		}
		add(Action{"write_today", reason, "xhs-topic → xhs-note", "xhs pool list"})
	}
	if st.AudienceAge < 0 || st.AudienceAge > 30 {
		why := "没有 audience.md：选题里的「有人搜」只能凭感觉"
		if st.AudienceAge > 30 {
			why = fmt.Sprintf("audience.md %.0f 天没更新", st.AudienceAge)
		}
		add(Action{"refresh_audience", why, "xhs-audience", "xhs words comments.txt"})
	}
	return st
}

func cmdStatus(args []string) int {
	f := parseFlags(args)
	st := buildStatus()
	if f.bools["json"] {
		printJSON(st)
		return 0
	}
	fmt.Printf("\n状态  ·  %s  ·  %s\n%s\n", st.Now, st.Home, strings.Repeat("=", 70))
	if st.Goal.Points > 0 {
		fmt.Printf("  目标      %s / %s，每天 %+.1f", fmtCount(st.Goal.Current), fmtCount(st.Goal.Target), st.Goal.PerDay)
		if st.Goal.ETA != "" {
			fmt.Printf("，预计 %s", st.Goal.ETA)
		}
		fmt.Println()
	}
	fmt.Printf("  已发      %d 篇，今天 %d 篇；草稿 %d 篇\n", st.Published, st.PostsToday, len(st.Drafts))
	if st.LastReview != "" {
		fmt.Printf("  复盘      上次 %s（%.1f 天前），连续没起色 %d / %d 次\n", st.LastReview, st.DaysSince, st.Stall.Streak, st.Stall.Threshold)
	} else {
		fmt.Println("  复盘      还没有记录")
	}
	fmt.Printf("  实验      %d 个在跑；选题池 %d 个候选\n", len(st.Experiments), st.PoolOpen)
	if len(st.Missing) > 0 {
		fmt.Printf("  缺的配置  %s\n", strings.Join(st.Missing, "、"))
	}
	fmt.Println("\n下一步")
	if len(st.Next) == 0 {
		fmt.Println("  没有要做的。")
	}
	for i, a := range st.Next {
		fmt.Printf("  %d. %s  %s\n", i+1, padRight(a.Action, 18), a.Reason)
		if a.Command != "" {
			fmt.Printf("     %s %s\n", strings.Repeat(" ", 18), a.Command)
		}
	}
	fmt.Println()
	return 0
}
