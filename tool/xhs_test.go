package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUTF16Len(t *testing.T) {
	cases := map[string]int{
		"Claude Code先跑测试再交活": 18,
		"改好了≠测过了":            7,
		"加一行🔥":               5,
	}
	for in, want := range cases {
		if got := utf16Len(in); got != want {
			t.Errorf("utf16Len(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestHumanizeDeletesFillerKeepsMeaning(t *testing.T) {
	lex := loadLexicon()
	in := "值得注意的是，在当今AI飞速发展的时代，很多人能够通过使用提示词进行优化——但没跑测试。"
	out, rep := humanize(in, lex)
	for _, gone := range []string{"值得注意的是", "在当今", "能够", "通过使用", "进行", "——"} {
		if strings.Contains(out, gone) {
			t.Errorf("%q should be removed, got %q", gone, out)
		}
	}
	if !strings.Contains(out, "没跑测试") {
		t.Errorf("meaning lost: %q", out)
	}
	if rep.fixedCount() == 0 {
		t.Error("report should count fixes")
	}
}

func TestHumanizeLeavesCodeAndURLsAlone(t *testing.T) {
	lex := loadLexicon()
	in := "运行 `npm test -- --watch=false` 看 https://example.com/a—b 能够通过"
	out, _ := humanize(in, lex)
	if !strings.Contains(out, "`npm test -- --watch=false`") || !strings.Contains(out, "https://example.com/a—b") {
		t.Errorf("protected spans were changed: %q", out)
	}
}

func TestStructuresFlagged(t *testing.T) {
	lex := loadLexicon()
	hits := scanStructures("这不是工具的问题，而是用法的问题。更快，更稳，更省。", lex)
	names := ""
	for _, h := range hits {
		names += h.Family + " "
	}
	for _, want := range []string{"not-but", "triad"} {
		if !strings.Contains(names, want) {
			t.Errorf("expected %s in %s", want, names)
		}
	}
}

func TestAbsoluteWordsSkipOrdinals(t *testing.T) {
	p := loadPlatform()
	if hits := p.findAbsolute("第一次用的时候，第一步先打开设置"); len(hits) != 0 {
		t.Errorf("ordinals flagged as 极限词: %v", hits)
	}
	if hits := p.findAbsolute("全网第一的工具"); len(hits) == 0 {
		t.Error("全网第一 should be flagged")
	}
}

func TestClassify(t *testing.T) {
	tb := loadTitles()
	cases := map[string]int{
		"Claude Code总说改好了？这段直接抄": 1,
		"别再让AI一口气改十个文件了":         4,
		"3个设置，让Cursor少犯错":        6,
		"一张图看懂MCP到底是什么":          9,
		"为什么AI总说测过了？":            10,
		"今天的学习记录":                0,
	}
	for in, want := range cases {
		if got, _ := tb.classify(in); got != want {
			t.Errorf("classify(%q) = #%d, want #%d", in, got, want)
		}
	}
}

func TestDiversionAndInducement(t *testing.T) {
	p := loadPlatform()
	text := "点赞过100就更新，评论区扣1领资料，电话13812345678"
	div := matchRules(p.Diversion, text)
	ind := matchRules(p.Inducement, text)
	if len(div) < 2 {
		t.Errorf("diversion not caught: %+v", div)
	}
	if len(ind) == 0 {
		t.Errorf("inducement not caught")
	}
}

func TestAUC(t *testing.T) {
	if a := auc([]float64{3, 4}, []float64{1, 2}); a != 1 {
		t.Errorf("perfect split auc = %v", a)
	}
	if a := auc([]float64{1, 2}, []float64{1, 2}); a != 0.5 {
		t.Errorf("identical auc = %v", a)
	}
}

func TestNum(t *testing.T) {
	cases := map[string]float64{"1,234": 1234, "1.2万": 12000, "3.5k": 3500, "12%": 0.12, "38s": 38}
	for in, want := range cases {
		if got, ok := num(in); !ok || got != want {
			t.Errorf("num(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestScoreTopic(t *testing.T) {
	r := TopicRubric{Threshold: 3}
	r.Items = append(r.Items, struct {
		Key   string `json:"key"`
		Max   int    `json:"max"`
		Guide string `json:"guide"`
	}{"有用", 3, ""}, struct {
		Key   string `json:"key"`
		Max   int    `json:"max"`
		Guide string `json:"guide"`
	}{"新", 2, ""})
	if _, total, err := scoreTopic("有用=3，新=1", r); err != nil || total != 4 {
		t.Errorf("total=%d err=%v", total, err)
	}
	if _, _, err := scoreTopic("有用=4,新=1", r); err == nil {
		t.Error("out of range score accepted")
	}
	if _, _, err := scoreTopic("有用=1", r); err == nil {
		t.Error("missing item accepted")
	}
	if _, _, err := scoreTopic("有用=1,新=1,话题=1", r); err == nil {
		t.Error("unknown item accepted")
	}
}

func TestSnapshotAtPrefersAtOrAfter(t *testing.T) {
	pts := []DataPoint{{Note: "a", Hours: 20, Views: 1}, {Note: "a", Hours: 50, Views: 2}, {Note: "a", Hours: 170, Views: 3}, {Note: "b", Hours: 48, Views: 9}}
	if p, _ := snapshotAt(pts, "a", 48); p.Views != 2 {
		t.Errorf("want the 50h snapshot, got %+v", p)
	}
	if p, _ := snapshotAt(pts, "a", 168); p.Views != 3 {
		t.Errorf("want the 170h snapshot, got %+v", p)
	}
	if _, ok := snapshotAt(pts, "c", 48); ok {
		t.Error("unknown note found")
	}
}

func TestImproved(t *testing.T) {
	a := ReviewRecord{Recent: map[string]float64{"views": 1000, "follow": 0.02}, FollowersPerDay: 10}
	b := ReviewRecord{Recent: map[string]float64{"views": 1050, "follow": 0.02}, FollowersPerDay: 10}
	if ok, _ := improved(a, b, 0.1); ok {
		t.Error("+5% should not count as progress at min_gain 0.1")
	}
	b.FollowersPerDay = 12
	if ok, _ := improved(a, b, 0.1); !ok {
		t.Error("+20% followers/day should count")
	}
}

func TestWordsQuestions(t *testing.T) {
	res := analyzeWords("小白写代码用什么工具\n小白写代码好难\n今天天气不错", 10)
	if len(res.Questions) != 1 {
		t.Errorf("questions = %+v", res.Questions)
	}
	found := false
	for _, w := range res.Phrases {
		if w.Text == "小白写代码" && w.Count == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("phrases = %+v", res.Phrases)
	}
}

// 每个句式的示例必须能被自己的正则认出来。改 titles.json 或加领域包时，这条测试会拦住写错的正则。
func TestFormulaExamplesSelfClassify(t *testing.T) {
	files := []string{"data/titles.json"}
	packs, _ := filepath.Glob("../packs/*/titles.json")
	files = append(files, packs...)
	for _, path := range files {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var tb TitleBook
		if err := json.Unmarshal(b, &tb); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		byID := map[int]*Formula{}
		for i := range tb.Formulas {
			f := &tb.Formulas[i]
			re, err := regexp.Compile(f.Match)
			if err != nil {
				t.Errorf("%s #%d 正则无效: %v", path, f.ID, err)
				continue
			}
			f.re = re
			byID[f.ID] = f
		}
		for _, id := range tb.ClassifyOrder {
			if f, ok := byID[id]; ok {
				tb.ordered = append(tb.ordered, f)
			}
		}
		if len(tb.ordered) != len(tb.Formulas) {
			t.Errorf("%s: classify_order 里漏了句式（%d / %d）", path, len(tb.ordered), len(tb.Formulas))
		}
		for _, f := range tb.Formulas {
			if got, _ := tb.classify(f.Example); got != f.ID {
				t.Errorf("%s: #%d 的示例「%s」被认成了 #%d", path, f.ID, f.Example, got)
			}
		}
	}
}
