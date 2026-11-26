package main

// 状态文件读写层。所有 loop 用到的状态都在 $XHS_HOME 下，格式见 docs/contract.md。
//
//   log.jsonl        发布记录，一篇一行（xhs log add）
//   data.jsonl       每篇笔记的数据快照，同一篇可以有多行（xhs data add）
//   account.jsonl    账号粉丝数快照（xhs goal log）
//   reviews.jsonl    每次复盘的摘要（xhs review --record）
//   experiments.json 实验账本（xhs exp）
//   pool.json        候选选题池（xhs pool）
//   playbook.md      验证过的规则（xhs exp close --rule 追加；人也可以直接改）
//   goal.json        目标和节奏（手写）
//   topic.json       选题打分表（手写，可选）
//   house.json       账号规则（手写，可选）
//   posts/<目录>/post.json  草稿和已发笔记

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func homePath(name string) string { return filepath.Join(xhsHome(), name) }

func ensureHome() error { return os.MkdirAll(xhsHome(), 0o755) }

func appendJSONL(name string, v any) error {
	if err := ensureHome(); err != nil {
		return err
	}
	fh, err := os.OpenFile(homePath(name), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fh.Write(append(b, '\n'))
	return err
}

func readJSONL[T any](name string) []T {
	fh, err := os.Open(homePath(name))
	if err != nil {
		return nil
	}
	defer fh.Close()
	var out []T
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1024*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v T
		if json.Unmarshal([]byte(line), &v) == nil {
			out = append(out, v)
		}
	}
	return out
}

// readJSONFile reports whether the file existed.
func readJSONFile(name string, v any) (bool, error) {
	b, err := os.ReadFile(homePath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return true, json.Unmarshal(b, v)
}

// writeJSONFile writes atomically: temp file then rename, so a crash never
// leaves a half-written ledger.
func writeJSONFile(name string, v any) error {
	if err := ensureHome(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := homePath(name + ".tmp")
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, homePath(name))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// ---------- time ----------

var timeLayouts = []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02T15:04", "2006/01/02 15:04", "2006-01-02", "2006/01/02", "01-02 15:04"}

func parseTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, l := range timeLayouts {
		if t, err := time.ParseInLocation(l, s, time.Local); err == nil {
			if l == "01-02 15:04" {
				t = t.AddDate(time.Now().Year(), 0, 0)
			}
			return t, true
		}
	}
	return time.Time{}, false
}

func nowStr() string { return time.Now().Format("2006-01-02 15:04") }

func today() string { return time.Now().Format("2006-01-02") }

// ---------- log.jsonl ----------

type LogEntry struct {
	ID        string   `json:"id"`
	Logged    string   `json:"logged"`
	Published string   `json:"published"`
	Slug      string   `json:"slug"`
	Title     string   `json:"title"`
	Cover     string   `json:"cover"`
	FormulaID int      `json:"formula_id"`
	Formula   string   `json:"formula"`
	Category  string   `json:"category,omitempty"`
	Exp       string   `json:"exp,omitempty"`
	Insight   string   `json:"insight"`
	Testing   string   `json:"testing"`
	Keywords  []string `json:"keywords"`
	Cards     int      `json:"cards"`
	Dir       string   `json:"dir,omitempty"`
}

func noteID(slug, title string) string {
	if slug != "" {
		return slug
	}
	h := sha1.Sum([]byte(title))
	return "n-" + hex.EncodeToString(h[:])[:8]
}

func readLogEntries() []LogEntry {
	es := readJSONL[LogEntry]("log.jsonl")
	for i := range es {
		if es[i].ID == "" {
			es[i].ID = noteID(es[i].Slug, es[i].Title)
		}
	}
	return es
}

// findNote matches an id, slug, exact title, or a unique title substring.
func findNote(es []LogEntry, key string) (*LogEntry, error) {
	key = strings.TrimSpace(key)
	for i := range es {
		if es[i].ID == key || es[i].Slug == key || es[i].Title == key {
			return &es[i], nil
		}
	}
	var hits []int
	for i := range es {
		if key != "" && strings.Contains(es[i].Title, key) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 1:
		return &es[hits[0]], nil
	case 0:
		return nil, fmt.Errorf("发布记录里找不到「%s」（先 xhs log add）", key)
	default:
		var ts []string
		for _, i := range hits {
			ts = append(ts, es[i].ID+" "+es[i].Title)
		}
		return nil, fmt.Errorf("「%s」匹配到多篇，用 id 指定：\n  %s", key, strings.Join(ts, "\n  "))
	}
}

func (e LogEntry) publishedAt() (time.Time, bool) { return parseTime(e.Published) }

func (e LogEntry) ageHours(now time.Time) float64 {
	if t, ok := e.publishedAt(); ok {
		return now.Sub(t).Hours()
	}
	return -1
}

// ---------- data.jsonl ----------

type DataPoint struct {
	Note        string  `json:"note"`
	Title       string  `json:"title,omitempty"`
	Captured    string  `json:"captured"`
	Hours       float64 `json:"hours"`
	Impressions float64 `json:"impressions,omitempty"`
	Views       float64 `json:"views,omitempty"`
	CTR         float64 `json:"ctr,omitempty"`
	AvgTime     float64 `json:"avg_time,omitempty"`
	Likes       float64 `json:"likes,omitempty"`
	Collects    float64 `json:"collects,omitempty"`
	Comments    float64 `json:"comments,omitempty"`
	Shares      float64 `json:"shares,omitempty"`
	Follows     float64 `json:"follows,omitempty"`
	Source      string  `json:"source,omitempty"`
}

func readData() []DataPoint { return readJSONL[DataPoint]("data.jsonl") }

// snapshotAt picks, for one note, the snapshot closest to `at` hours after
// publishing, preferring ones at or past that point. Comparing a note at 20
// hours with one at 160 hours says nothing about either.
func snapshotAt(points []DataPoint, note string, at float64) (DataPoint, bool) {
	var best DataPoint
	found := false
	score := func(p DataPoint) float64 {
		d := p.Hours - at
		if d < 0 {
			return -d*3 + 1000 // before the point: only if nothing after it
		}
		return d
	}
	for _, p := range points {
		if p.Note != note {
			continue
		}
		if !found || score(p) < score(best) {
			best, found = p, true
		}
	}
	return best, found
}

// ---------- account.jsonl ----------

type AccountPoint struct {
	Captured  string  `json:"captured"`
	Followers float64 `json:"followers"`
	Source    string  `json:"source,omitempty"`
}

// ---------- reviews.jsonl ----------

type ReviewRecord struct {
	At              string             `json:"at"`
	Notes           int                `json:"notes"`
	Mature          int                `json:"mature"`
	CompareHours    float64            `json:"compare_hours"`
	Medians         map[string]float64 `json:"medians"`
	Recent          map[string]float64 `json:"recent"`
	RecentNotes     int                `json:"recent_notes"`
	Followers       float64            `json:"followers,omitempty"`
	FollowersPerDay float64            `json:"followers_per_day,omitempty"`
	Findings        []string           `json:"findings,omitempty"`
}

// ---------- goal.json ----------

type GoalConfig struct {
	Note            string  `json:"note,omitempty"`
	Metric          string  `json:"metric"`
	Target          float64 `json:"target"`
	Deadline        string  `json:"deadline"`
	ReviewEveryDays float64 `json:"review_every_days"`
	CaptureHours    []int   `json:"capture_hours"`
	CompareHours    float64 `json:"compare_hours"`
	RecentDays      float64 `json:"recent_days"`
	MinNotesPerExp  int     `json:"min_notes_per_experiment"`
	Stall           struct {
		Reviews int     `json:"reviews"`
		MinGain float64 `json:"min_gain"`
	} `json:"stall"`
}

func loadGoal() (GoalConfig, bool) {
	var g GoalConfig
	b, _ := embedded.ReadFile("data/goal.json")
	_ = json.Unmarshal(b, &g)
	ok, err := readJSONFile("goal.json", &g)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: goal.json 解析失败，沿用默认值: %v\n", err)
	}
	return g, ok
}

// ---------- topic.json ----------

type TopicRubric struct {
	Note      string `json:"note,omitempty"`
	Threshold int    `json:"threshold"`
	Items     []struct {
		Key   string `json:"key"`
		Max   int    `json:"max"`
		Guide string `json:"guide"`
	} `json:"items"`
}

func loadTopicRubric() (TopicRubric, bool) {
	var r TopicRubric
	ok, err := readJSONFile("topic.json", &r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "警告: topic.json 解析失败，用内置打分表: %v\n", err)
		ok = false
	}
	if !ok {
		r = TopicRubric{}
		b, _ := embedded.ReadFile("data/topic.json")
		_ = json.Unmarshal(b, &r)
	}
	return r, ok
}
