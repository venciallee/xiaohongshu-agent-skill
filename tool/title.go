package main

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// xhs title - 封面大字和标题打分，并按 titles.json 的 18 个句式归类。
//
// 这是什么：五个本地启发式，量的是小红书双列流里一个标题要在不到一秒里做到的事：
// 长度用得对、有具体的东西、有人会搜的词、有一个信息差或代价、关键信息在前面。
//
// 这不是什么：点击率预测器。和 instagram-agent-skill 不同，这套打分还没有在真实
// 小红书数据上验证过。能验证的办法在 `xhs eval`：用 xhs-viral 收集到的对标笔记
// （≥40 条）跑一遍，看高分组和低分组能不能分开。AUC 接近 0.5 就说明它在你的领域
// 只能挡掉明显的烂标题，别指望它挑出爆款。

type Formula struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Template string `json:"template"`
	Example  string `json:"example"`
	Cover    string `json:"cover"`
	BestFor  string `json:"best_for"`
	Trap     string `json:"trap"`
	Match    string `json:"match"`
	re       *regexp.Regexp
}

type TitleBook struct {
	Version       string    `json:"version"`
	Note          string    `json:"note"`
	Rules         []string  `json:"rules"`
	ClassifyOrder []int     `json:"classify_order"`
	Formulas      []Formula `json:"formulas"`
	ordered       []*Formula
}

func loadTitles() *TitleBook {
	var tb TitleBook
	mustLoad("titles.json", &tb)
	byID := map[int]*Formula{}
	for i := range tb.Formulas {
		f := &tb.Formulas[i]
		re, err := regexp.Compile(f.Match)
		if err != nil {
			fmt.Fprintf(os.Stderr, "titles.json #%d 正则无效: %v\n", f.ID, err)
			continue
		}
		f.re = re
		byID[f.ID] = f
	}
	order := tb.ClassifyOrder
	if len(order) == 0 {
		for _, f := range tb.Formulas {
			order = append(order, f.ID)
		}
	}
	for _, id := range order {
		if f, ok := byID[id]; ok && f.re != nil {
			tb.ordered = append(tb.ordered, f)
		}
	}
	return &tb
}

func (tb *TitleBook) classify(s string) (int, string) {
	for _, f := range tb.ordered {
		if f.re.MatchString(s) {
			return f.ID, f.Name
		}
	}
	return 0, "未归类"
}

var (
	searchIntentRE = regexp.MustCompile(`(?i)怎么|如何|教程|攻略|技巧|方法|步骤|入门|新手|小白|避坑|推荐|测评|对比|区别|是什么|清单|合集|模板|直接抄|用法|设置|配置|做法|配方|vs`)
	hookRE         = regexp.MustCompile(`≠|→|(?i)\bvs\b|却|没|假装|别|不要|千万|错|坑|白用|浪费|省|少走|才发现|才知道|原来|其实|竟然|居然|没想到|大部分人|很多人|没人|终于|一直|总是|老是|反了|偷偷|悄悄|为什么`)
	weakOpenRE     = regexp.MustCompile(`^(关于|今天|分享|一篇|浅谈|聊聊|我的|一些|简单|记录|随便|小记|日常)`)
	concreteNounRE = regexp.MustCompile(`步骤|清单|模板|公式|配方|做法|设置|配置|开关|命令|提示词|文件|快捷键|一行|一句话|一张图|表格|工具`)
	exclaimRE      = regexp.MustCompile(`[！!]{2,}|。$`)
)

type TitleCheck struct {
	Score  float64 `json:"score"`
	Detail string  `json:"detail"`
}

type TitleResult struct {
	Text        string                `json:"text"`
	Mode        string                `json:"mode"`
	Checks      map[string]TitleCheck `json:"checks"`
	Score       float64               `json:"score"`
	Grade       string                `json:"grade"`
	Weakest     string                `json:"weakest"`
	Dealbreaker []string              `json:"dealbreakers,omitempty"`
	FormulaID   int                   `json:"formula_id"`
	Formula     string                `json:"formula"`
}

var titleChecks = []string{"长度", "具体", "搜索词", "信息差", "前置"}

func scoreTitle(s string, cover bool, keywords []string, tb *TitleBook, p *Platform, h House) TitleResult {
	s = strings.TrimSpace(s)
	r := TitleResult{Text: s, Mode: "标题", Checks: map[string]TitleCheck{}}
	if cover {
		r.Mode = "封面"
	}
	n := utf16Len(s)

	// 长度
	var ls float64
	if cover {
		switch {
		case n < 3:
			ls = 40
		case n <= 8:
			ls = 100
		case n <= 11:
			ls = 85
		case n <= h.CoverBigMax:
			ls = 65
		default:
			ls = clamp(65 - float64(n-h.CoverBigMax)*15)
		}
		r.Checks["长度"] = TitleCheck{ls, fmt.Sprintf("%d 字（封面想要 3-8，最多 %d）", n, h.CoverBigMax)}
	} else {
		switch {
		case n > p.Limits.TitleMax:
			ls = 0
		case n >= 12:
			ls = 100
		case n >= 8:
			ls = 80
		default:
			ls = clamp(100 - float64(12-n)*15)
		}
		r.Checks["长度"] = TitleCheck{ls, fmt.Sprintf("%d / %d 字（标题想要 12-20，把搜索位用上）", n, p.Limits.TitleMax)}
	}

	// 具体
	nums := len(numberRE.FindAllString(s, -1)) + len(cnNumberRE.FindAllString(s, -1))
	names := 0
	for _, t := range asciiTokenRE.FindAllString(s, -1) {
		if len(t) >= 2 {
			names++
		}
	}
	nouns := len(concreteNounRE.FindAllString(s, -1)) + len(containsAnyFold(s, h.ConcreteWords))
	spec := nums + names + nouns
	ss := 20.0
	switch {
	case spec >= 2:
		ss = 100
	case spec == 1:
		ss = 75
	}
	if cover && ss < 60 {
		ss = 60 // 封面可以只是一个反差，具体的东西留给标题和第 2 张卡
	}
	r.Checks["具体"] = TitleCheck{ss, fmt.Sprintf("%d 个数字、%d 个工具名、%d 个具体名词", nums, names, nouns)}

	// 搜索词
	var sk float64
	var skDetail string
	if len(keywords) > 0 {
		hits := containsAnyFold(s, keywords)
		front := containsAnyFold(firstUnits(s, 10), keywords)
		switch {
		case len(front) > 0:
			sk, skDetail = 100, "搜索词在前 10 字："+strings.Join(front, "、")
		case len(hits) > 0:
			sk, skDetail = 70, "有搜索词但靠后："+strings.Join(hits, "、")
		default:
			sk, skDetail = 10, "没有你要的搜索词（"+strings.Join(keywords, "、")+"）"
		}
	} else {
		intent := append(searchIntentRE.FindAllString(s, -1), containsAnyFold(s, h.SearchWords)...)
		sk = 40
		if len(intent) > 0 {
			sk = 80
		}
		if names > 0 {
			sk += 20
		}
		sk = clamp(sk)
		skDetail = fmt.Sprintf("%d 个搜索意图词、%d 个工具名（传 --keywords 会更准）", len(intent), names)
	}
	if cover {
		// 封面不靠搜索，这一项只做参考，权重在总分里降低
		skDetail += "；封面不靠搜索，这项只参考"
	}
	r.Checks["搜索词"] = TitleCheck{sk, skDetail}

	// 信息差
	hk := hookRE.FindAllString(s, -1)
	hs := 35.0
	if len(hk) == 1 {
		hs = 80
	} else if len(hk) >= 2 {
		hs = 100
	}
	if strings.ContainsAny(s, "？?") {
		hs = clamp(hs + 15)
	}
	r.Checks["信息差"] = TitleCheck{hs, fmt.Sprintf("%d 个反差 / 代价词%s", len(hk), map[bool]string{true: "，带问号", false: ""}[strings.ContainsAny(s, "？?")])}

	// 前置
	head := firstUnits(s, 8)
	fs := 30.0
	frontHit := numberRE.MatchString(head) || asciiTokenRE.MatchString(head) || len(containsAnyFold(head, keywords)) > 0
	if frontHit {
		fs = 100
	} else if hookRE.MatchString(head) {
		fs = 80
	} else if h12 := firstUnits(s, 12); numberRE.MatchString(h12) || asciiTokenRE.MatchString(h12) {
		fs = 70
	}
	if cover && n <= 8 {
		fs = 100 // 封面就这么几个字，没有「前后」之分
	}
	if weakOpenRE.MatchString(s) {
		fs = clamp(fs - 40)
	}
	r.Checks["前置"] = TitleCheck{fs, "前 8 字：「" + head + "」"}

	// 总分
	var scores []float64
	for _, c := range titleChecks {
		v := r.Checks[c].Score
		if cover && c == "搜索词" {
			v = 50 + v/2
		}
		scores = append(scores, v)
	}
	r.Score = mean(scores)*0.6 + minf(scores)*0.4

	// 一票否决
	if !cover && n > p.Limits.TitleMax {
		r.Dealbreaker = append(r.Dealbreaker, fmt.Sprintf("超过 %d 字，发不出去。", p.Limits.TitleMax))
	}
	if cover && n > h.CoverBigMax+4 {
		r.Dealbreaker = append(r.Dealbreaker, "封面字太多，缩略图上读不完。")
	}
	if a := p.findAbsolute(s); len(a) > 0 {
		r.Dealbreaker = append(r.Dealbreaker, "极限词："+strings.Join(a, "、")+"。")
	}
	if hy := p.findHype(s); len(hy) > 0 {
		r.Dealbreaker = append(r.Dealbreaker, "标题党词："+strings.Join(hy, "、")+"。")
	}
	for _, hit := range matchRules(p.Inducement, s) {
		r.Dealbreaker = append(r.Dealbreaker, "诱导互动："+strings.Join(hit.Matches, "、")+"。")
	}
	if countEmoji(s) >= 2 {
		r.Dealbreaker = append(r.Dealbreaker, "两个以上 emoji，挤掉了字。")
	}
	if exclaimRE.MatchString(s) {
		r.Dealbreaker = append(r.Dealbreaker, "连续感叹号或句号结尾。")
	}
	for _, c := range h.CompanyNotSubject {
		if strings.HasPrefix(strings.ToLower(s), strings.ToLower(c)) {
			rest := strings.TrimSpace(s[len(c):])
			if regexp.MustCompile(`^(又|刚刚|正式|终于)?(发布|推出|上线|宣布|出了|放出|开源)`).MatchString(rest) {
				r.Dealbreaker = append(r.Dealbreaker, "公司做主语（"+c+"……发布），本账号规则不这么写。把读者能用的那件事放前面。")
			}
		}
	}
	if len(r.Dealbreaker) > 0 && r.Score > 45 {
		r.Score = 45
	}

	switch {
	case r.Score >= 70:
		r.Grade = "强"
	case r.Score >= 50:
		r.Grade = "可用"
	default:
		r.Grade = "弱"
	}
	ws := 101.0
	for _, c := range titleChecks {
		if cover && c == "搜索词" {
			continue
		}
		if v := r.Checks[c].Score; v < ws {
			ws, r.Weakest = v, c
		}
	}
	r.FormulaID, r.Formula = tb.classify(s)
	return r
}

func cmdTitle(args []string) int {
	f := parseFlags(args, "keywords", "t")
	keywords := splitList(f.vals["keywords"])
	var lines []string
	if t := f.vals["t"]; t != "" {
		lines = []string{t}
	} else {
		raw, err := readInput(f.arg(0, "-"))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		for _, l := range strings.Split(raw, "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "#") {
				lines = append(lines, l)
			}
		}
	}
	if len(lines) == 0 {
		fmt.Fprintln(os.Stderr, "没有可打分的标题（一行一个）")
		return 2
	}
	tb, p, h := loadTitles(), loadPlatform(), loadHouse()
	cover := f.bools["cover"]
	var res []TitleResult
	for _, l := range lines {
		res = append(res, scoreTitle(l, cover, keywords, tb, p, h))
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Score > res[j].Score })
	if f.bools["json"] {
		printJSON(res)
		return 0
	}
	mode := "标题"
	if cover {
		mode = "封面大字"
	}
	fmt.Printf("\n%s排序  ·  %d 条\n", mode, len(res))
	fmt.Println(strings.Repeat("=", 74))
	for i, r := range res {
		mark := "  "
		if i == 0 {
			mark = "->"
		}
		fid := "-"
		if r.FormulaID > 0 {
			fid = fmt.Sprintf("#%d", r.FormulaID)
		}
		fmt.Printf("%s %5.1f %s  %s\n", mark, r.Score, padRight(r.Grade, 4), r.Text)
		fmt.Printf("           句式 %s %s   最弱：%s（%s）\n", fid, r.Formula, r.Weakest, r.Checks[r.Weakest].Detail)
		for _, d := range r.Dealbreaker {
			fmt.Printf("           一票否决：%s\n", d)
		}
	}
	fmt.Println()
	if res[0].Score < 50 {
		fmt.Println("  最高分不到 50：还没有能用的标题，换角度重写，别在这几条上抠字。")
		fmt.Println()
	}
	return 0
}
