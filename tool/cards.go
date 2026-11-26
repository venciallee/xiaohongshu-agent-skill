package main

import (
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// xhs cards - 把 post.json 里的封面和卡片排成 1080x1440（3:4）的 HTML，
// 加 --render 时调本机 Chrome 无头截图成 PNG，并检查有没有字溢出卡片。
//
// 风格是刻意收着的：白底、粗黑体、一个强调色、没有渐变、没有阴影、没有 emoji 图标、
// 没有条形图。卡片上只放做法，来源和口径放置顶评论。溢出的卡片会在图上盖一条红色
// 「溢出」横幅，同时命令行报错：删字或拆卡，别缩字号。

const cardTemplate = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8">
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  html, body { width: 1080px; height: 1440px; background: {{bg}}; overflow: hidden; }
  body { font-family: {{font}}; color: {{ink}}; -webkit-font-smoothing: antialiased; }
  .card { position: relative; width: 1080px; height: 1440px; padding: 120px 96px 140px; display: flex; flex-direction: column; }
  .page { position: absolute; top: 56px; right: 96px; font-size: 30px; color: {{muted}}; font-weight: 500; letter-spacing: 2px; }
  .handle { position: absolute; bottom: 60px; left: 96px; font-size: 28px; color: {{muted}}; font-weight: 500; }
  .fit { flex: 1; overflow: hidden; display: flex; flex-direction: column; }
  .fit { justify-content: center; padding-bottom: 60px; }
  .big { font-weight: 900; line-height: 1.16; letter-spacing: 2px; text-wrap: balance; word-break: break-all; }
  .sub { margin-top: 64px; font-size: 54px; font-weight: 500; line-height: 1.45; color: {{ink}}; text-wrap: balance; }
  .rule { width: 120px; height: 14px; background: {{accent}}; margin-bottom: 64px; }
  .head { font-size: 88px; font-weight: 900; line-height: 1.22; margin-bottom: 64px; letter-spacing: 1px; text-wrap: balance; }
  .text { font-size: 56px; font-weight: 500; line-height: 1.6; white-space: pre-wrap; text-wrap: pretty; }
  .code { margin-top: 48px; background: {{codebg}}; padding: 44px 48px; font-family: {{mono}}; font-size: 42px; line-height: 1.6; white-space: pre-wrap; word-break: break-all; border-left: 10px solid {{accent}}; }
  .note { margin-top: 40px; font-size: 38px; color: {{muted}}; line-height: 1.5; }
  mark { background: linear-gradient(transparent 58%, {{accent}}55 58%); color: inherit; padding: 0 2px; }
  .overflow-banner { position: absolute; left: 0; right: 0; top: 0; height: 120px; background: #D40000; color: #fff; font-size: 48px; font-weight: 900; display: flex; align-items: center; justify-content: center; }
</style></head>
<body><div class="card {{kind}}">
  <div class="page">{{page}}</div>
  <div class="fit" id="fit">{{content}}</div>
  <div class="handle">{{handle}}</div>
</div>
<script>
  (function () {
    var fit = document.getElementById('fit');
    var over = fit.scrollHeight > fit.clientHeight + 2;
    var kids = fit.querySelectorAll('*');
    for (var i = 0; i < kids.length; i++) { if (kids[i].scrollWidth > kids[i].clientWidth + 2 && kids[i].tagName !== 'MARK') over = true; }
    if (over) {
      document.body.setAttribute('data-overflow', '1');
      var b = document.createElement('div'); b.className = 'overflow-banner'; b.textContent = '!! 溢出：删字或拆卡';
      document.querySelector('.card').appendChild(b);
    } else { document.body.setAttribute('data-overflow', '0'); }
  })();
</script>
</body></html>`

var markRE = regexp.MustCompile(`==([^=\n]+)==`)

func richText(s string) string {
	esc := html.EscapeString(s)
	return markRE.ReplaceAllString(esc, "<mark>$1</mark>")
}

// coverFontSize picks the largest size that fits the cover text in at most
// two balanced lines across the 888px text column. ASCII is about half a Han
// character wide.
func coverFontSize(big string) int {
	w := 0.0
	for _, r := range big {
		if r < 128 {
			w += 0.56
		} else {
			w += 1
		}
	}
	perLine := w
	if w > 5 {
		perLine = float64(int(w/2 + 0.99))
	}
	if perLine < 1 {
		perLine = 1
	}
	size := int(860 / perLine)
	switch {
	case size > 200:
		size = 200
	case size < 96:
		size = 96
	}
	return size
}

func fillTemplate(st CardStyle, kind, page, handle, content string) string {
	r := strings.NewReplacer(
		"{{bg}}", st.Background, "{{ink}}", st.Ink, "{{accent}}", st.Accent, "{{muted}}", st.Muted,
		"{{codebg}}", st.CodeBg, "{{font}}", st.Font, "{{mono}}", st.Mono,
		"{{kind}}", kind, "{{page}}", page, "{{handle}}", html.EscapeString(handle), "{{content}}", content,
	)
	return r.Replace(cardTemplate)
}

type builtCard struct {
	HTML string
	PNG  string
}

func buildCards(p *Post, h House, outDir string) ([]builtCard, error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	st := h.CardStyle
	total := len(p.Cards)
	if p.Cover.Big != "" {
		total++
	}
	var out []builtCard
	idx := 0
	write := func(doc string) error {
		idx++
		name := filepath.Join(outDir, fmt.Sprintf("card-%02d.html", idx))
		if err := os.WriteFile(name, []byte(doc), 0o644); err != nil {
			return err
		}
		out = append(out, builtCard{HTML: name, PNG: strings.TrimSuffix(name, ".html") + ".png"})
		return nil
	}
	if p.Cover.Big != "" {
		content := fmt.Sprintf(`<div class="rule"></div><div class="big" style="font-size:%dpx">%s</div>`, coverFontSize(p.Cover.Big), richText(p.Cover.Big))
		if p.Cover.Sub != "" {
			content += `<div class="sub">` + richText(p.Cover.Sub) + `</div>`
		}
		if err := write(fillTemplate(st, "cover", "", h.HandleOnCards, content)); err != nil {
			return nil, err
		}
	}
	for i, c := range p.Cards {
		var b strings.Builder
		if c.Head != "" {
			b.WriteString(`<div class="head">` + richText(c.Head) + `</div>`)
		}
		if c.Text != "" {
			b.WriteString(`<div class="text">` + richText(c.Text) + `</div>`)
		}
		if c.Code != "" {
			b.WriteString(`<div class="code">` + html.EscapeString(strings.TrimRight(c.Code, "\n")) + `</div>`)
		}
		if c.Note != "" {
			b.WriteString(`<div class="note">` + richText(c.Note) + `</div>`)
		}
		pageNo := i + 1
		if p.Cover.Big != "" {
			pageNo = i + 2
		}
		page := fmt.Sprintf("%d / %d", pageNo, total)
		if err := write(fillTemplate(st, "body", page, h.HandleOnCards, b.String())); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func findChrome(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if env := os.Getenv("CHROME"); env != "" {
		return env
	}
	cands := []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"}
	if runtime.GOOS == "darwin" {
		for _, p := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	for _, c := range cands {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

func chromeArgs(extra ...string) []string {
	base := []string{"--headless=new", "--disable-gpu", "--hide-scrollbars", "--no-first-run",
		"--no-default-browser-check", "--force-device-scale-factor=1", "--virtual-time-budget=3000"}
	if os.Geteuid() == 0 {
		base = append(base, "--no-sandbox")
	}
	return append(base, extra...)
}

func renderCard(chrome string, c builtCard) (overflow bool, err error) {
	abs, _ := filepath.Abs(c.HTML)
	url := "file://" + abs
	shot := exec.Command(chrome, chromeArgs("--window-size=1080,1440", "--screenshot="+c.PNG, url)...)
	if out, err := shot.CombinedOutput(); err != nil {
		return false, fmt.Errorf("chrome 截图失败: %v\n%s", err, string(out))
	}
	dom := exec.Command(chrome, chromeArgs("--window-size=1080,1440", "--dump-dom", url)...)
	b, err := dom.Output()
	if err != nil {
		return false, fmt.Errorf("chrome dump-dom 失败: %v", err)
	}
	return strings.Contains(string(b), `data-overflow="1"`), nil
}

// contactSheet puts every card side by side at quarter size, so the whole
// note can be judged in one glance before anything is sent.
func contactSheet(pngs []string, out string) error {
	const cw, ch, gap = 270, 360, 16
	n := len(pngs)
	if n == 0 {
		return nil
	}
	sheet := image.NewRGBA(image.Rect(0, 0, n*cw+(n+1)*gap, ch+2*gap))
	draw.Draw(sheet, sheet.Bounds(), &image.Uniform{color.RGBA{0xE6, 0xE6, 0xE6, 0xFF}}, image.Point{}, draw.Src)
	for i, p := range pngs {
		fh, err := os.Open(p)
		if err != nil {
			return err
		}
		img, err := png.Decode(fh)
		fh.Close()
		if err != nil {
			return err
		}
		small := boxDownscale(img, cw, ch)
		x := gap + i*(cw+gap)
		draw.Draw(sheet, image.Rect(x, gap, x+cw, gap+ch), small, image.Point{}, draw.Src)
	}
	fh, err := os.Create(out)
	if err != nil {
		return err
	}
	defer fh.Close()
	return png.Encode(fh, sheet)
}

func boxDownscale(src image.Image, w, h int) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	sx := float64(b.Dx()) / float64(w)
	sy := float64(b.Dy()) / float64(h)
	for y := 0; y < h; y++ {
		y0, y1 := b.Min.Y+int(float64(y)*sy), b.Min.Y+int(float64(y+1)*sy)
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < w; x++ {
			x0, x1 := b.Min.X+int(float64(x)*sx), b.Min.X+int(float64(x+1)*sx)
			if x1 <= x0 {
				x1 = x0 + 1
			}
			var r, g, bl, a, cnt uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := src.At(xx, yy).RGBA()
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
					cnt++
				}
			}
			dst.Set(x, y, color.RGBA64{uint16(r / cnt), uint16(g / cnt), uint16(bl / cnt), uint16(a / cnt)})
		}
	}
	return dst
}

func cmdCards(args []string) int {
	f := parseFlags(args, "o", "out", "chrome")
	path := f.arg(0, ".")
	p, real, err := loadPost(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	h := loadHouse()
	outDir := f.vals["o"]
	if outDir == "" {
		outDir = f.vals["out"]
	}
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(real), "cards")
	}
	cards, err := buildCards(p, h, outDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\n卡片  ·  %d 张  ·  1080x1440  ·  %s\n", len(cards), outDir)
	fmt.Println(strings.Repeat("=", 60))
	if !f.bools["render"] {
		for _, c := range cards {
			fmt.Println("  " + filepath.Base(c.HTML))
		}
		fmt.Println("\n  加 --render 用本机 Chrome 出 PNG 并检查溢出。")
		return 0
	}
	chrome := findChrome(f.vals["chrome"])
	if chrome == "" {
		fmt.Fprintln(os.Stderr, "找不到 Chrome / Chromium。装一个，或者用 --chrome 指定路径，或设置 CHROME 环境变量。")
		return 2
	}
	bad := 0
	var pngs []string
	for _, c := range cards {
		over, err := renderCard(chrome, c)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		status := "ok  "
		if over {
			status = "!!  溢出"
			bad++
		}
		fmt.Printf("  %s %s\n", padRight(status, 10), filepath.Base(c.PNG))
		pngs = append(pngs, c.PNG)
	}
	sheet := filepath.Join(outDir, "preview.png")
	if err := contactSheet(pngs, sheet); err != nil {
		fmt.Fprintln(os.Stderr, "拼预览图失败:", err)
	} else {
		fmt.Printf("\n  预览：%s\n", sheet)
	}
	fmt.Println(strings.Repeat("-", 60))
	if bad > 0 {
		fmt.Printf("  %d 张溢出。删字或拆卡，别缩字号：手机上看的是三分之一大小。\n\n", bad)
		return 1
	}
	fmt.Println("  全部放得下。打开每张图看一遍再交付。")
	fmt.Println()
	return 0
}
