package main

import (
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
