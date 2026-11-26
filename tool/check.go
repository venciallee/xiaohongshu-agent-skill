package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// xhs check - 发布前检查。读 post.json，查三类东西：
//
//   平台    标题 20 字、正文 1000 字、图片数量、Markdown 不渲染、AI 声明
//   风险    导流（联系方式、私信领取、网址、站外平台）、诱导互动、极限词、标题党
//   本账号  house.json 里的规则：卡片数、话题数、自我介绍句、第一人称经历待确认、
//           卡片上不放来源口径、洞察句和「本篇在测什么」必须写
//
// 先打印双列流里这篇大概长什么样，再逐项 PASS / WARN / FAIL。
// 它不登录、不上传、不发布。READY 的意思是「可以交给本人去 App 里发了」。

type Card struct {
	Head string `json:"head"`
	Text string `json:"text"`
	Code string `json:"code"`
	Note string `json:"note"`
}

type Source struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type Post struct {
	Slug     string   `json:"slug"`
	Date     string   `json:"date"`
	Topic    string   `json:"topic"`
	Insight  string   `json:"insight"`
	Testing  string   `json:"testing"`
	Keywords []string `json:"keywords"`
	Cover    struct {
		Big string `json:"big"`
		Sub string `json:"sub"`
	} `json:"cover"`
	Titles               []string `json:"titles"`
	Title                string   `json:"title"`
	Body                 string   `json:"body"`
	Tags                 []string `json:"tags"`
	Cards                []Card   `json:"cards"`
	PinnedComment        string   `json:"pinned_comment"`
	Sources              []Source `json:"sources"`
	FirstPersonConfirmed []string `json:"first_person_confirmed"`
	AIDisclosure         *bool    `json:"ai_disclosure"`
}

func loadPost(path string) (*Post, string, error) {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		path = filepath.Join(path, "post.json")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	var p Post
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, path, fmt.Errorf("%s 不是合法 JSON: %w", path, err)
	}
	// 兼容几个常见别名
	var alt map[string]json.RawMessage
	_ = json.Unmarshal(b, &alt)
	if p.Body == "" {
		if v, ok := alt["content"]; ok {
			_ = json.Unmarshal(v, &p.Body)
		}
	}
	if len(p.Tags) == 0 {
		if v, ok := alt["topics"]; ok {
			_ = json.Unmarshal(v, &p.Tags)
		}
	}
	if len(p.Cards) == 0 {
		if v, ok := alt["slides"]; ok {
			_ = json.Unmarshal(v, &p.Cards)
		}
	}
	return &p, path, nil
}

type CheckItem struct {
	Status string `json:"status"`
	Check  string `json:"check"`
	Detail string `json:"detail"`
}

type CheckReport struct {
	Path    string      `json:"path"`
	Items   []CheckItem `json:"checks"`
	Confirm []string    `json:"needs_confirmation"`
	Verdict string      `json:"verdict"`
}

func (r *CheckReport) add(status, check, detail string) {
	r.Items = append(r.Items, CheckItem{status, check, detail})
}

var (
	tagInBodyRE    = regexp.MustCompile(`#([^\s#\[\]，。！？]{1,20})(\[话题\])?#?`)
	firstLineBadRE = regexp.MustCompile(`^(大家好|哈喽|嗨|今天|我是|作为|分享|最近)`)
	numberClaimRE  = regexp.MustCompile(`[0-9]+(\.[0-9]+)?\s*(%|倍|万|亿)|[0-9]+\s*(个|位)?(用户|人|开发者)`)
	sentenceRE     = regexp.MustCompile(`[^。！？!?\n]+[。！？!?]?`)
)

func runCheck(p *Post, keywords []string, plat *Platform, h House, lex *Lexicon, tb *TitleBook) CheckReport {
	rep := CheckReport{}
	if len(keywords) == 0 {
		keywords = p.Keywords
	}

	titles := p.Titles
	if p.Title != "" {
		titles = append([]string{p.Title}, titles...)
	}
	titles = uniq(titles)

	// ---- 标题 ----
	if len(titles) == 0 {
		rep.add("FAIL", "标题", "没有标题。写 3 个备选放进 titles，本人选一个。")
	}
	for i, t := range titles {
		n := utf16Len(t)
		label := fmt.Sprintf("标题 %d", i+1)
		switch {
		case n > plat.Limits.TitleMax:
			rep.add("FAIL", label, fmt.Sprintf("%d / %d 字，超了：%s", n, plat.Limits.TitleMax, t))
		case n < 8:
			rep.add("WARN", label, fmt.Sprintf("%d 字，搜索位没用上：%s", n, t))
		default:
			rep.add("PASS", label, fmt.Sprintf("%d / %d 字：%s", n, plat.Limits.TitleMax, t))
		}
		r := scoreTitle(t, false, keywords, tb, plat, h)
		for _, d := range r.Dealbreaker {
			// 长度、极限词、标题党、诱导在别处统一报，这里只报标题独有的
			if strings.HasPrefix(d, "超过") || strings.HasPrefix(d, "极限词") || strings.HasPrefix(d, "标题党") || strings.HasPrefix(d, "诱导") {
				continue
			}
			rep.add("WARN", label, d)
		}
	}
	if len(keywords) > 0 && len(titles) > 0 {
		var miss []string
		for _, t := range titles {
			if len(containsAnyFold(t, keywords)) == 0 {
				miss = append(miss, t)
			}
		}
		if len(miss) == len(titles) {
			rep.add("WARN", "标题搜索词", "3 个标题都没有搜索词（"+strings.Join(keywords, "、")+"）")
		} else if len(miss) > 0 {
			rep.add("PASS", "标题搜索词", fmt.Sprintf("%d / %d 个标题带搜索词", len(titles)-len(miss), len(titles)))
		} else {
			rep.add("PASS", "标题搜索词", "每个标题都带搜索词")
		}
	}

	// ---- 同质化：和最近发过的比 ----
	if logs := readLog(); len(logs) > 0 && len(titles) > 0 {
		recent := logs
		if len(recent) > 5 {
			recent = recent[len(recent)-5:]
		}
		fid, fname := tb.classify(titles[0])
		same := 0
		for _, e := range recent {
			if v, ok := e["formula_id"].(float64); ok && int(v) == fid && fid != 0 {
				same++
			}
			if c, ok := e["cover"].(string); ok && c != "" && c == p.Cover.Big {
				rep.add("WARN", "同质化", "封面大字和之前发过的一篇一模一样："+c)
			}
		}
		if same >= 3 {
			rep.add("WARN", "同质化", fmt.Sprintf("最近 %d 篇里有 %d 篇也是「%s」句式。换一个，平台把「套模板批量生产同质化内容」列为 AI 低质。", len(recent), same, fname))
		}
	}

	// ---- 封面 ----
	if p.Cover.Big == "" {
		rep.add("WARN", "封面大字", "没写封面大字。双列流里先被看到的是它，不是标题。")
	} else {
		n := utf16Len(p.Cover.Big)
		if n > h.CoverBigMax {
			rep.add("WARN", "封面大字", fmt.Sprintf("%d 字，超过 %d，缩略图上读不完：%s", n, h.CoverBigMax, p.Cover.Big))
		} else {
			rep.add("PASS", "封面大字", fmt.Sprintf("%d 字：%s", n, p.Cover.Big))
		}
		for _, t := range titles {
			if strings.TrimSpace(t) == strings.TrimSpace(p.Cover.Big) {
				rep.add("WARN", "封面大字", "和标题是同一句。封面给眼睛，标题给搜索，分开写。")
				break
			}
		}
	}

	// ---- 正文 ----
	body := strings.TrimSpace(p.Body)
	bodyAll := body
	if len(p.Tags) > 0 && !strings.Contains(body, "#") {
		bodyAll = body + "\n" + "#" + strings.Join(p.Tags, " #")
	}
	bn := runeLen(bodyAll)
	switch {
	case body == "":
		rep.add("FAIL", "正文", "正文是空的。")
	case bn > plat.Limits.BodyMax:
		rep.add("FAIL", "正文长度", fmt.Sprintf("%d / %d 字（含话题），超了 %d 字", bn, plat.Limits.BodyMax, bn-plat.Limits.BodyMax))
	default:
		rep.add("PASS", "正文长度", fmt.Sprintf("%d / %d 字（含话题）", bn, plat.Limits.BodyMax))
	}

	lines := []string{}
	for _, l := range strings.Split(body, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	if len(lines) > 0 {
		first := lines[0]
		switch {
		case firstLineBadRE.MatchString(first):
			rep.add("WARN", "开头第一句", "开场白 / 自我介绍开头。第一句写读者正在经历的场景："+truncWidth(first, 40))
		case len(keywords) > 0 && len(containsAnyFold(first, keywords)) == 0:
			rep.add("WARN", "开头第一句", "没有搜索词。小红书搜索会读正文开头："+truncWidth(first, 40))
		default:
			rep.add("PASS", "开头第一句", truncWidth(first, 48))
		}
		if p.Insight != "" && len(lines) > 1 {
			second := lines[1]
			if !sharesChunk(second, p.Insight, 4) && !sharesChunk(first, p.Insight, 4) {
				rep.add("WARN", "开头第二句", "前两句里看不到洞察句。第二句直接亮洞察，并说这篇能拿走什么。")
			} else {
				rep.add("PASS", "开头第二句", "洞察句在开头")
			}
		}
	}

	// ---- 话题 ----
	tags := p.Tags
	for _, m := range tagInBodyRE.FindAllStringSubmatch(body, -1) {
		tags = append(tags, m[1])
	}
	tags = uniq(tags)
	switch {
	case len(tags) == 0:
		rep.add("WARN", "话题", "没有话题。")
	case len(tags) < h.TagsMin || len(tags) > h.TagsMax:
		rep.add("WARN", "话题", fmt.Sprintf("%d 个，本账号规则是 %d-%d 个：%s", len(tags), h.TagsMin, h.TagsMax, strings.Join(tags, " ")))
	default:
		rep.add("PASS", "话题", fmt.Sprintf("%d 个：%s", len(tags), strings.Join(tags, " ")))
	}
	if len(tags) > 0 {
		rep.add("INFO", "话题插入", "在 App 里输入 # 从联想里选话题，纯文本 # 不一定会变成话题链接。")
	}
	if len(keywords) > 0 && len(tags) > 0 {
		if len(containsAnyFold(strings.Join(tags, " "), keywords)) == 0 {
			rep.add("WARN", "话题搜索词", "话题里没有搜索词。")
		}
	}

	// ---- 卡片 ----
	nc := len(p.Cards)
	if p.Cover.Big != "" {
		nc++
	}
	switch {
	case nc == 0:
		rep.add("WARN", "卡片", "没有卡片。")
	case nc > plat.Limits.ImagesMax:
		rep.add("FAIL", "卡片", fmt.Sprintf("%d 张，平台最多 %d 张", nc, plat.Limits.ImagesMax))
	case nc > h.MaxCards:
		rep.add("WARN", "卡片", fmt.Sprintf("%d 张（含封面），本账号规则最多 %d 张", nc, h.MaxCards))
	default:
		rep.add("PASS", "卡片", fmt.Sprintf("%d 张（含封面）", nc))
	}
	for i, c := range p.Cards {
		label := fmt.Sprintf("卡片 %d", i+2)
		if utf16Len(c.Head) > h.CardHeadMax {
			rep.add("WARN", label, fmt.Sprintf("大标题 %d 字，超过 %d：%s", utf16Len(c.Head), h.CardHeadMax, c.Head))
		}
		if runeLen(c.Text) > h.CardTextMax {
			rep.add("WARN", label, fmt.Sprintf("正文 %d 字，超过 %d。一张卡一个做法，多了就拆。", runeLen(c.Text), h.CardTextMax))
		}
		if c.Code != "" {
			if n := len(strings.Split(strings.TrimRight(c.Code, "\n"), "\n")); n > h.CardCodeLinesMax {
				rep.add("WARN", label, fmt.Sprintf("代码 %d 行，超过 %d 行在手机上看不清。全文放正文，卡片放能截图的那几行。", n, h.CardCodeLinesMax))
			}
		}
		if hit := containsAnyFold(c.Head+c.Text+c.Note, h.CardForbidden); len(hit) > 0 {
			rep.add("WARN", label, "卡片上有「"+strings.Join(hit, "、")+"」。来源、口径、免责挪到置顶评论。")
		}
	}

	// ---- 风险：导流 / 诱导 / 极限词 / 标题党 / Markdown ----
	all := strings.Join(append(append([]string{body, p.Cover.Big, p.Cover.Sub}, titles...), cardTexts(p.Cards)...), "\n")
	divHits := matchRules(plat.Diversion, all)
	if len(divHits) == 0 {
		rep.add("PASS", "导流", "没有联系方式、网址、站外平台")
	}
	for _, hit := range divHits {
		rep.add(hit.Severity, "导流", hit.Name+"："+strings.Join(hit.Matches, "、"))
	}
	if p.PinnedComment != "" {
		for _, hit := range matchRules(plat.Diversion, p.PinnedComment) {
			sev := "WARN"
			if hit.Severity == "FAIL" {
				sev = "FAIL"
			}
			rep.add(sev, "置顶评论导流", hit.Name+"："+strings.Join(hit.Matches, "、")+"。来源写名称和标题就够，评论区网址同样可能被判导流。")
		}
	}
	indHits := matchRules(plat.Inducement, all)
	if len(indHits) == 0 {
		rep.add("PASS", "诱导互动", "没有")
	}
	for _, hit := range indHits {
		rep.add(hit.Severity, "诱导互动", hit.Name+"："+strings.Join(hit.Matches, "、"))
	}
	if a := plat.findAbsolute(all); len(a) > 0 {
		rep.add(plat.AbsoluteWords.Severity, "极限词", strings.Join(a, "、"))
	} else {
		rep.add("PASS", "极限词", "没有")
	}
	if hy := plat.findHype(all); len(hy) > 0 {
		rep.add(plat.HypeWords.Severity, "标题党", strings.Join(hy, "、"))
	}
	if plat.Markdown.Regex != "" {
		if m := regexp.MustCompile(plat.Markdown.Regex).FindAllString(body, -1); len(m) > 0 {
			rep.add(plat.Markdown.Severity, "Markdown", fmt.Sprintf("%s：%d 处（%s）。先跑 xhs human 去掉。", plat.Markdown.Name, len(m), truncWidth(strings.Join(uniq(m), " "), 30)))
		}
	}

	// ---- 本账号规则：身份句、第一人称、洞察句 ----
	for _, s := range lex.Structures {
		if s.ID == "identity" && s.re != nil {
			if m := s.re.FindAllString(all, -1); len(m) > 0 {
				sev := h.IdentitySentence
				if sev == "" {
					sev = "WARN"
				}
				rep.add(sev, "身份句", "「"+strings.Join(uniq(m), "」「")+"」。正文不写自我介绍，可信度靠洞察、原文和踩过的坑。")
			}
		}
	}
	confirmed := map[string]bool{}
	for _, c := range p.FirstPersonConfirmed {
		confirmed[strings.TrimSpace(c)] = true
	}
	for _, sent := range sentenceRE.FindAllString(all, -1) {
		sent = strings.TrimSpace(sent)
		if len(containsAnyFold(sent, h.FirstPersonMarkers)) == 0 {
			continue
		}
		ok := false
		for c := range confirmed {
			if c != "" && strings.Contains(sent, c) {
				ok = true
				break
			}
		}
		if !ok {
			rep.Confirm = append(rep.Confirm, sent)
		}
	}
	rep.Confirm = uniq(rep.Confirm)
	if len(rep.Confirm) > 0 {
		rep.add("WARN", "第一人称经历", fmt.Sprintf("%d 句「我试过 / 实测」类的话等本人确认。没确认的改成机制说法，确认过的写进 first_person_confirmed。", len(rep.Confirm)))
	}
	for _, field := range h.RequireFields {
		v := map[string]string{"insight": p.Insight, "testing": p.Testing, "topic": p.Topic}[field]
		name := map[string]string{"insight": "洞察句", "testing": "本篇在测什么", "topic": "选题"}[field]
		if strings.TrimSpace(v) == "" {
			rep.add("WARN", name, "没写。没有它，72 小时后复盘时不知道这篇在验证什么。")
		}
	}
	if numberClaimRE.MatchString(all) && len(p.Sources) == 0 {
		rep.add("WARN", "来源", "正文里有数字，但 sources 是空的。查不到出处的数字不写。")
	}

	// ---- AI 声明 ----
	switch {
	case p.AIDisclosure == nil:
		rep.add("FAIL", "AI 声明", "post.json 没有 ai_disclosure。AI 参与了（含润色）就设 true，并在 App 发布页勾选 AI 声明。")
	case !*p.AIDisclosure:
		rep.add("WARN", "AI 声明", "ai_disclosure=false。只有完全没用 AI 写和润色时才这么设。")
	default:
		rep.add("INFO", "AI 声明", "发布时在 App 里勾选 AI 声明（平台规则：未主动声明的会被加标识并限流）。")
	}

	fails, warns := 0, 0
	for _, it := range rep.Items {
		switch it.Status {
		case "FAIL":
			fails++
		case "WARN":
			warns++
		}
	}
	switch {
	case fails > 0:
		rep.Verdict = "FIX"
	case warns > 0:
		rep.Verdict = "REVIEW"
	default:
		rep.Verdict = "READY"
	}
	return rep
}

func cardTexts(cs []Card) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Head, c.Text, c.Code, c.Note)
	}
	return out
}

// sharesChunk reports whether a and b share any run of n Han characters.
func sharesChunk(a, b string, n int) bool {
	ra := []rune(b)
	for i := 0; i+n <= len(ra); i++ {
		chunk := string(ra[i : i+n])
		hasHan := false
		for _, r := range chunk {
			if isHan(r) {
				hasHan = true
			}
		}
		if hasHan && strings.Contains(a, chunk) {
			return true
		}
	}
	return false
}

func renderFeed(p *Post, h House) {
	title := p.Title
	if title == "" && len(p.Titles) > 0 {
		title = p.Titles[0]
	}
	const w = 22
	fmt.Println("\n  双列流里大概长这样（示意，字号按缩略图估算）")
	fmt.Println("  +" + strings.Repeat("-", w+2) + "+")
	big := p.Cover.Big
	if big == "" {
		big = "（没有封面大字）"
	}
	bl := wrapWidth(big, 14)
	rows := []string{""}
	for _, l := range bl {
		rows = append(rows, "  "+l)
	}
	if p.Cover.Sub != "" {
		rows = append(rows, "")
		for _, l := range wrapWidth(p.Cover.Sub, w-2) {
			rows = append(rows, " "+l)
		}
	}
	for len(rows) < 8 {
		rows = append(rows, "")
	}
	for _, r := range rows[:8] {
		fmt.Printf("  | %s |\n", padRight(truncWidth(r, w), w))
	}
	fmt.Println("  +" + strings.Repeat("-", w+2) + "+")
	tl := wrapWidth(title, w)
	for i, l := range tl {
		if i == 2 {
			fmt.Printf("    %s\n", "…（后面大概率被截掉）")
			break
		}
		fmt.Printf("    %s\n", l)
	}
	acct := h.Account
	if acct == "" {
		acct = "你的昵称"
	}
	fmt.Printf("    %s            ♡ 赞\n", acct)
}

func cmdCheck(args []string) int {
	f := parseFlags(args, "keywords")
	path := f.arg(0, ".")
	p, real, err := loadPost(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	plat, h, lex, tb := loadPlatform(), loadHouse(), loadLexicon(), loadTitles()
	rep := runCheck(p, splitList(f.vals["keywords"]), plat, h, lex, tb)
	rep.Path = real
	if f.bools["json"] {
		printJSON(rep)
		if rep.Verdict == "READY" {
			return 0
		}
		return 1
	}
	name := p.Slug
	if name == "" {
		name, _ = filepath.Abs(filepath.Dir(real))
		name = filepath.Base(name)
	}
	head := fmt.Sprintf("发布前检查  ·  %s  ·  平台规则核实于 %s", name, plat.Verified)
	fmt.Println("\n" + head)
	fmt.Println(strings.Repeat("=", 74))
	renderFeed(p, h)
	fmt.Println()
	for _, it := range rep.Items {
		fmt.Printf("  %s %s %s\n", padRight(it.Status, 6), padRight(it.Check, 14), it.Detail)
	}
	if len(rep.Confirm) > 0 {
		fmt.Println("\n  等本人确认的第一人称句：")
		for _, c := range rep.Confirm {
			fmt.Println("    - " + truncWidth(c, 64))
		}
	}
	fmt.Println(strings.Repeat("-", 74))
	fmt.Printf("  结论  %s\n\n", rep.Verdict)
	if rep.Verdict == "READY" {
		return 0
	}
	return 1
}

// readLog loads $XHS_HOME/log.jsonl (one JSON object per line).
func readLog() []map[string]any {
	path := filepath.Join(xhsHome(), "log.jsonl")
	fh, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer fh.Close()
	var out []map[string]any
	sc := bufio.NewScanner(fh)
	sc.Buffer(make([]byte, 1024*1024), 4*1024*1024)
	for sc.Scan() {
		var m map[string]any
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}
