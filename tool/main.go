// xhs - 小红书 agent skill pack 的本地工具。
//
// 一个二进制，九个子命令，只用 Go 标准库，不联网，不上传任何东西，
// 只读你给它的文本和表格。它不登录小红书，也不发布任何内容：
// 这个 pack 里所有 skill 的最后一步都是「本人在 App 里手动发」。
//
//	xhs human   draft.txt --report         去 AI 腔：删填充语、标记套话和句式
//	xhs detect  draft.txt [after.txt]      五项读者视角自检，可对比前后
//	xhs title   titles.txt --keywords ..   封面大字 / 标题打分 + 句式归类
//	xhs check   post.json                  发布前检查：平台限制、导流、诱导、AI 声明……
//	xhs cards   post.json -o out --render  生成 1080x1440 白底卡片（可调 Chrome 出 PNG）
//	xhs swipe   captured.tsv --out f.md    对标笔记：按「赞藏 / 该账号中位数」排序
//	xhs review  notes.csv                  72 小时复盘：漏斗拆解，分清是封面、内容还是主页的问题
//	xhs log     add post.json | list       发布记录，给同质化检查和复盘用
//	xhs eval    swipe.tsv                  用你自己的对标数据检验标题打分到底准不准
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

//go:embed data/*.json
var embedded embed.FS

const version = "1.0.0"

func usage() {
	fmt.Fprint(os.Stderr, `xhs `+version+` - 小红书图文的本地检查工具（不联网、不发布）

用法:
  xhs human   <file|-> [--report] [-o out.txt] [--json]
  xhs detect  <file|-> [compare-file] [--json]
  xhs title   <file|-> [--keywords a,b] [--cover] [--json]
  xhs check   <post.json|dir> [--keywords a,b] [--json]
  xhs cards   <post.json|dir> [-o outdir] [--render] [--chrome path]
  xhs swipe   <captured.tsv> [--out swipe.md] [--json]
  xhs review  <notes.csv|tsv> [--json] [--md]
  xhs log     add <post.json> [--published "2026-11-26 09:30"] | list [-n 10]
  xhs eval    <swipe.tsv> [--keywords a,b]
  xhs data    [--dump dir]       查看 / 导出内置词库和规则，导出后可以改

环境变量:
  XHS_HOME   状态目录，默认 ~/.xiaohongshu。里面的 house.json 覆盖账号规则，
             data/*.json 覆盖内置词库，log.jsonl 是发布记录。
`)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	var code int
	switch cmd {
	case "human":
		code = cmdHuman(args)
	case "detect":
		code = cmdDetect(args)
	case "title":
		code = cmdTitle(args)
	case "check", "lint":
		code = cmdCheck(args)
	case "cards":
		code = cmdCards(args)
	case "swipe":
		code = cmdSwipe(args)
	case "review":
		code = cmdReview(args)
	case "log":
		code = cmdLog(args)
	case "eval":
		code = cmdEval(args)
	case "data":
		code = cmdData(args)
	case "version", "--version", "-v":
		fmt.Println("xhs", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "未知子命令: %s\n\n", cmd)
		usage()
		code = 2
	}
	os.Exit(code)
}

// ---------- state & data ----------

func xhsHome() string {
	if h := os.Getenv("XHS_HOME"); h != "" {
		return expandHome(h)
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".xiaohongshu")
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}

// loadData reads a data file: $XHS_HOME/data/<name> wins over the embedded copy.
func loadData(name string, v any) error {
	override := filepath.Join(xhsHome(), "data", name)
	if b, err := os.ReadFile(override); err == nil {
		if err := json.Unmarshal(b, v); err != nil {
			return fmt.Errorf("%s: %w", override, err)
		}
		return nil
	}
	b, err := embedded.ReadFile("data/" + name)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func mustLoad(name string, v any) {
	if err := loadData(name, v); err != nil {
		fmt.Fprintf(os.Stderr, "读取 %s 失败: %v\n", name, err)
		os.Exit(2)
	}
}

// loadHouse merges $XHS_HOME/house.json on top of the embedded defaults,
// so an override only needs the fields it changes.
func loadHouse() House {
	var h House
	b, _ := embedded.ReadFile("data/house.json")
	_ = json.Unmarshal(b, &h)
	path := filepath.Join(xhsHome(), "house.json")
	if ob, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(ob, &h); err != nil {
			fmt.Fprintf(os.Stderr, "警告: %s 解析失败，沿用默认规则: %v\n", path, err)
		}
	}
	return h
}

func readInput(path string) (string, error) {
	if path == "-" || path == "" {
		b, err := readAllStdin()
		return string(b), err
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

func readAllStdin() ([]byte, error) { return io.ReadAll(os.Stdin) }

func printJSON(v any) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func cmdData(args []string) int {
	dump := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--dump" && i+1 < len(args) {
			dump = args[i+1]
			i++
		}
	}
	entries, _ := embedded.ReadDir("data")
	if dump == "" {
		fmt.Println("内置数据文件（用 --dump <dir> 导出后修改，放进 $XHS_HOME/data/ 即可覆盖）:")
		for _, e := range entries {
			b, _ := embedded.ReadFile("data/" + e.Name())
			fmt.Printf("  %-14s %6d bytes\n", e.Name(), len(b))
		}
		fmt.Printf("\nXHS_HOME = %s\n", xhsHome())
		return 0
	}
	if err := os.MkdirAll(dump, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, e := range entries {
		b, _ := embedded.ReadFile("data/" + e.Name())
		out := filepath.Join(dump, e.Name())
		if _, err := os.Stat(out); err == nil {
			fmt.Fprintf(os.Stderr, "跳过 %s（已存在）\n", out)
			continue
		}
		if err := os.WriteFile(out, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("wrote", out)
	}
	return 0
}

// ---------- tiny flag parser ----------
// The stdlib flag package stops at the first positional argument, which is
// awkward for "xhs human draft.txt --report". This one takes flags anywhere.

type flags struct {
	pos   []string
	bools map[string]bool
	vals  map[string]string
}

func parseFlags(args []string, valued ...string) flags {
	f := flags{bools: map[string]bool{}, vals: map[string]string{}}
	isValued := map[string]bool{}
	for _, v := range valued {
		isValued[v] = true
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-" || !strings.HasPrefix(a, "-") {
			f.pos = append(f.pos, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if k, v, ok := strings.Cut(name, "="); ok {
			f.vals[k] = v
			continue
		}
		if isValued[name] && i+1 < len(args) {
			f.vals[name] = args[i+1]
			i++
			continue
		}
		f.bools[name] = true
	}
	return f
}

func (f flags) arg(i int, def string) string {
	if i < len(f.pos) {
		return f.pos[i]
	}
	return def
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == '，' || r == '、' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
