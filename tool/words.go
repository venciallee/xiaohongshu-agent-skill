package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// xhs words - 读者用的词。
//
// 把评论区、搜索联想词、私信问题这类原话贴成一个文本文件，它统计反复出现的
// 2-6 字词组和英文词，并把问句单独挑出来。用来写 audience.md：
// 读者怎么说，比你怎么说重要。
//
//	xhs words comments.txt [--top 40] [--json]

var (
	questionRE  = regexp.MustCompile(`[？?]|怎么|为什么|为啥|什么|求|有没有|能不能|可不可以|哪个|哪里|多少|如何|是不是|咋|吗$`)
	stopRunes   = "的了是在我你他她它这那就都也还和与吗呢吧啊呀哦嗯个一不有很太好要会能让把被给从到对说看想去来上下里么啦嘛"
	asciiWordRE = regexp.MustCompile(`[A-Za-z][A-Za-z0-9.+#\-]*[A-Za-z0-9+#]|[A-Za-z]{2,}`)
)

type wordCount struct {
	Text  string `json:"text"`
	Count int    `json:"count"`
}

type WordsResult struct {
	Lines     int         `json:"lines"`
	Phrases   []wordCount `json:"phrases"`
	Terms     []wordCount `json:"terms"`
	Questions []wordCount `json:"questions"`
}

func allStop(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune(stopRunes, r) {
			return false
		}
	}
	return true
}

func analyzeWords(raw string, top int) WordsResult {
	var res WordsResult
	grams := map[string]int{}
	terms := map[string]int{}
	qs := map[string]int{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res.Lines++
		for _, w := range asciiWordRE.FindAllString(line, -1) {
			terms[strings.ToLower(w)]++
		}
		// 问句：按句切开，只保留像问题的那句
		for _, s := range regexp.MustCompile(`[。！!；;\n]`).Split(line, -1) {
			s = strings.TrimSpace(s)
			if runeLen(s) >= 3 && runeLen(s) <= 60 && questionRE.MatchString(s) {
				qs[s]++
			}
		}
		// 汉字连续段里的 2-6 字词组，每行同一个词组只算一次
		seen := map[string]bool{}
		var seg []rune
		flush := func() {
			for n := 2; n <= 6; n++ {
				for i := 0; i+n <= len(seg); i++ {
					g := string(seg[i : i+n])
					first, last := seg[i], seg[i+n-1]
					if allStop(g) || strings.ContainsRune("的了是在和与也就都", first) || strings.ContainsRune("的了是在和与吗呢吧啊", last) {
						continue
					}
					if !seen[g] {
						seen[g] = true
						grams[g]++
					}
				}
			}
			seg = seg[:0]
		}
		for _, r := range line {
			if unicode.Is(unicode.Han, r) {
				seg = append(seg, r)
			} else {
				flush()
			}
		}
		flush()
	}
	// 去掉被更长词组「包含且次数差不多」的短词组：「写代码」出现几次，「代码」「写代」就不再单列
	var keys []string
	for k, v := range grams {
		if v >= 2 {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return runeLen(keys[i]) > runeLen(keys[j]) })
	kept := map[string]int{}
	for _, k := range keys {
		redundant := false
		for longer, c := range kept {
			if strings.Contains(longer, k) && float64(c) >= 0.8*float64(grams[k]) {
				redundant = true
				break
			}
		}
		if !redundant {
			kept[k] = grams[k]
		}
	}
	toList := func(m map[string]int, min int) []wordCount {
		var out []wordCount
		for k, v := range m {
			if v >= min {
				out = append(out, wordCount{k, v})
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Count != out[j].Count {
				return out[i].Count > out[j].Count
			}
			return runeLen(out[i].Text) > runeLen(out[j].Text)
		})
		if len(out) > top {
			out = out[:top]
		}
		return out
	}
	res.Phrases = toList(kept, 2)
	res.Terms = toList(terms, 2)
	res.Questions = toList(qs, 1)
	return res
}

func cmdWords(args []string) int {
	f := parseFlags(args, "top")
	top := 40
	if v, ok := num(f.vals["top"]); ok && v > 0 {
		top = int(v)
	}
	var raw strings.Builder
	paths := f.pos
	if len(paths) == 0 {
		paths = []string{"-"}
	}
	for _, p := range paths {
		s, err := readInput(p)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		raw.WriteString(s + "\n")
	}
	res := analyzeWords(raw.String(), top)
	if f.bools["json"] {
		printJSON(res)
		return 0
	}
	fmt.Printf("\n读者用的词  ·  %d 行原话\n%s\n", res.Lines, strings.Repeat("=", 60))
	fmt.Println("反复出现的词组（同一行只算一次）")
	for _, w := range res.Phrases {
		fmt.Printf("  %3d  %s\n", w.Count, w.Text)
	}
	if len(res.Terms) > 0 {
		fmt.Println("\n英文词 / 工具名")
		for _, w := range res.Terms {
			fmt.Printf("  %3d  %s\n", w.Count, w.Text)
		}
	}
	if len(res.Questions) > 0 {
		fmt.Println("\n问句（选题和标题的原材料）")
		for _, w := range res.Questions {
			fmt.Printf("  %3d  %s\n", w.Count, w.Text)
		}
	}
	fmt.Println("\n频次是原话里的出现次数，不是搜索量。样本是谁的评论区，结论就只代表谁的读者。")
	fmt.Println()
	return 0
}
