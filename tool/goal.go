package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"time"
)

// xhs goal - 目标和涨粉轨迹。
//
//	xhs goal                     看离目标还有多远、按最近的速度多久到
//	xhs goal log --followers N   记一次粉丝数（account.jsonl）
//
// 目标写在 $XHS_HOME/goal.json。速度用最近 14 天的快照算；少于两个快照就没有速度。

type Trajectory struct {
	Metric       string  `json:"metric"`
	Target       float64 `json:"target"`
	Current      float64 `json:"current"`
	CapturedAt   string  `json:"captured_at,omitempty"`
	Points       int     `json:"points"`
	WindowDays   float64 `json:"window_days"`
	PerDay       float64 `json:"per_day"`
	DaysToTarget float64 `json:"days_to_target,omitempty"`
	ETA          string  `json:"eta,omitempty"`
	Deadline     string  `json:"deadline,omitempty"`
	NeedPerDay   float64 `json:"need_per_day,omitempty"`
	OnTrack      *bool   `json:"on_track,omitempty"`
	Note         string  `json:"note,omitempty"`
}

func accountPoints() []AccountPoint {
	pts := readJSONL[AccountPoint]("account.jsonl")
	sort.SliceStable(pts, func(i, j int) bool {
		a, _ := parseTime(pts[i].Captured)
		b, _ := parseTime(pts[j].Captured)
		return a.Before(b)
	})
	return pts
}

func trajectory(g GoalConfig, pts []AccountPoint, now time.Time) Trajectory {
	tr := Trajectory{Metric: g.Metric, Target: g.Target, Deadline: g.Deadline, Points: len(pts)}
	if len(pts) == 0 {
		tr.Note = "还没有粉丝数记录：xhs goal log --followers N"
		return tr
	}
	last := pts[len(pts)-1]
	lastT, _ := parseTime(last.Captured)
	tr.Current, tr.CapturedAt = last.Followers, last.Captured
	// 最近 14 天窗口里最早的一个点
	var first *AccountPoint
	for i := range pts {
		t, ok := parseTime(pts[i].Captured)
		if ok && lastT.Sub(t).Hours() <= 14*24 {
			first = &pts[i]
			break
		}
	}
	if first != nil {
		ft, _ := parseTime(first.Captured)
		days := lastT.Sub(ft).Hours() / 24
		if days >= 0.9 {
			tr.WindowDays = math.Round(days*10) / 10
			tr.PerDay = math.Round((last.Followers-first.Followers)/days*10) / 10
		}
	}
	if tr.WindowDays == 0 {
		tr.Note = "粉丝数快照不足两天，还算不出速度"
	}
	if g.Target > 0 && tr.Current < g.Target && tr.PerDay > 0 {
		tr.DaysToTarget = math.Ceil((g.Target - tr.Current) / tr.PerDay)
		tr.ETA = lastT.AddDate(0, 0, int(tr.DaysToTarget)).Format("2006-01-02")
	}
	if g.Target > 0 && g.Deadline != "" {
		if dl, ok := parseTime(g.Deadline); ok {
			left := dl.Sub(now).Hours() / 24
			if left > 0 {
				tr.NeedPerDay = math.Ceil((g.Target - tr.Current) / left)
				if tr.WindowDays > 0 {
					on := tr.PerDay >= tr.NeedPerDay
					tr.OnTrack = &on
				}
			}
		}
	}
	return tr
}

func renderTrajectory(tr Trajectory, g GoalConfig) {
	fmt.Println("\n目标")
	fmt.Println(strings.Repeat("=", 60))
	if g.Target <= 0 {
		fmt.Println("  goal.json 里还没定目标（target）。")
	} else {
		fmt.Printf("  目标      %s %s", fmtCount(g.Target), g.Metric)
		if g.Deadline != "" {
			fmt.Printf("，截止 %s", g.Deadline)
		}
		fmt.Println()
	}
	if tr.Points == 0 {
		fmt.Println("  " + tr.Note)
		fmt.Println()
		return
	}
	fmt.Printf("  现在      %s（%s）\n", fmtCount(tr.Current), tr.CapturedAt)
	if g.Target > 0 {
		fmt.Printf("  完成度    %.1f%%\n", tr.Current/g.Target*100)
	}
	if tr.WindowDays > 0 {
		fmt.Printf("  速度      每天 %+.1f（最近 %.1f 天）\n", tr.PerDay, tr.WindowDays)
	} else {
		fmt.Println("  速度      " + tr.Note)
	}
	if tr.ETA != "" {
		fmt.Printf("  按这个速度 %s 到（还要 %.0f 天）\n", tr.ETA, tr.DaysToTarget)
	} else if g.Target > 0 && tr.WindowDays > 0 && tr.PerDay <= 0 {
		fmt.Println("  按这个速度 到不了：最近没有净增长")
	}
	if tr.NeedPerDay > 0 {
		fmt.Printf("  要按期到   每天需要 +%.0f", tr.NeedPerDay)
		if tr.OnTrack != nil {
			if *tr.OnTrack {
				fmt.Print("，现在的速度够")
			} else {
				fmt.Printf("，现在差 %.0f 倍", tr.NeedPerDay/math.Max(tr.PerDay, 0.1))
			}
		}
		fmt.Println()
	}
	fmt.Println()
}

func cmdGoal(args []string) int {
	f := parseFlags(args, "followers", "at", "source")
	g, _ := loadGoal()
	switch f.arg(0, "show") {
	case "log":
		v, ok := num(f.vals["followers"])
		if !ok {
			fmt.Fprintln(os.Stderr, "用法: xhs goal log --followers 1234 [--at \"2026-11-26 09:00\"]")
			return 2
		}
		at := nowStr()
		if s := f.vals["at"]; s != "" {
			t, ok := parseTime(s)
			if !ok {
				fmt.Fprintf(os.Stderr, "--at 看不懂：%s\n", s)
				return 2
			}
			at = t.Format("2006-01-02 15:04")
		}
		p := AccountPoint{Captured: at, Followers: v, Source: f.vals["source"]}
		if err := appendJSONL("account.jsonl", p); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		tr := trajectory(g, accountPoints(), time.Now())
		if f.bools["json"] {
			printJSON(tr)
			return 0
		}
		fmt.Printf("记下了：%s 粉丝（%s）\n", fmtCount(v), at)
		renderTrajectory(tr, g)
		return 0
	case "show":
		tr := trajectory(g, accountPoints(), time.Now())
		if f.bools["json"] {
			printJSON(tr)
			return 0
		}
		renderTrajectory(tr, g)
		return 0
	}
	fmt.Fprintln(os.Stderr, "用法: xhs goal [show] | log --followers N")
	return 2
}
