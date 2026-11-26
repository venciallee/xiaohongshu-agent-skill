// xhs - 小红书 agent skill pack 的本地工具。
//
// 一个二进制，只用 Go 标准库，不联网，不上传，只读写你给它的文件和 $XHS_HOME 下的状态。
// 它不登录小红书，也不发布任何内容：发布永远由用户在 App 里手动完成。
//
// 写稿和把关
//
//	xhs title    titles.txt --keywords ..   封面大字 / 标题打分 + 句式归类
//	xhs human    draft.txt --report         去 AI 腔
//	xhs detect   draft.txt [after.txt]      五项读者视角自检
//	xhs check    post.json                  发布前检查
//	xhs cards    post.json --render         1080x1440 卡片 PNG + 溢出检查
//
// 研究
//
//	xhs words    comments.txt               读者原话里反复出现的词和问句
//	xhs swipe    captured.tsv               对标笔记按「赞藏 / 账号中位数」排序
//	xhs eval     captured.tsv               检验标题打分在你的领域准不准
//
// loop 状态（$XHS_HOME，格式见 docs/contract.md）
//
//	xhs status                              现在该做什么（编排器每次先跑它）
//	xhs pool     add|list|use|drop          候选选题池
//	xhs log      add|list                   发布记录
//	xhs data     add|list                   每篇笔记的数据快照
//	xhs goal     [log --followers N]        目标和涨粉轨迹
//	xhs review   [--record]                 复盘
//	xhs exp      new|list|show|close        实验账本，结论写进 playbook.md
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

//go:embed data/*
var embedded embed.FS

const version = "1.1.0"

func usage() {
	fmt.Fprint(os.Stderr, `xhs `+version+` - 小红书图文的本地工具（不联网、不发布）

写稿和把关
  xhs title    <file|-> [--keywords a,b] [--cover]
  xhs human    <file|-> [--report] [-o out.txt]
  xhs detect   <file|-> [compare-file]
  xhs check    <post.json|dir> [--keywords a,b]
  xhs cards    <post.json|dir> [-o outdir] [--render] [--chrome path]

研究
  xhs words    <file...> [--top 40]
  xhs swipe    <captured.tsv> [--out swipe.md]
  xhs eval     <captured.tsv> [--keywords a,b]

loop 状态（都在 $XHS_HOME 下）
  xhs status
  xhs pool     add --title .. --insight .. --scores "有用=3,..." | list [--all] | use <id> | drop <id>
  xhs log      add <post.json> [--published "2026-11-26 09:30"] [--exp E1] | list [-n 10]
  xhs data     add <id|标题> --views N --impressions N ... | add --file rows.csv | list [id]
  xhs goal     [show] | log --followers N
  xhs review   [notes.csv] [--at 48] [--record] [--md]
  xhs exp      new --hypothesis .. --variable .. --change .. [--metric save_rate] | list | show <id> | close <id> --result ..

其他
  xhs init                       在 $XHS_HOME 生成配置模板（已有的不覆盖）
  xhs defaults [--dump dir]      查看 / 导出内置词库、规则和配置模板

所有命令都支持 --json（机器可读输出），失败时退出码非 0。

环境变量
  XHS_HOME   状态目录，默认 ~/.xiaohongshu
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
		code = cmdDataStore(args)
	case "defaults":
		code = cmdDefaults(args)
	case "init":
		code = cmdInit(args)
	case "status":
		code = cmdStatus(args)
	case "goal":
		code = cmdGoal(args)
	case "exp":
		code = cmdExp(args)
	case "pool":
		code = cmdPool(args)
	case "words":
		code = cmdWords(args)
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

func cmdDefaults(args []string) int {
	dump := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--dump" && i+1 < len(args) {
			dump = args[i+1]
			i++
		}
	}
	entries, _ := embedded.ReadDir("data")
	if dump == "" {
		fmt.Println("内置数据文件（xhs defaults --dump <dir> 导出后修改。slop/titles/platform 放进 $XHS_HOME/data/ 覆盖；house/goal/topic 放在 $XHS_HOME/ 下）:")
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

// accountTemplates are the files a user fills in for their own account.
var accountTemplates = []string{"voice.md", "sources.md", "goal.json", "topic.json", "house.json"}

func cmdInit(args []string) int {
	f := parseFlags(args)
	if err := os.MkdirAll(filepath.Join(xhsHome(), "posts"), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var created, kept []string
	for _, name := range accountTemplates {
		dst := homePath(name)
		if fileExists(dst) {
			kept = append(kept, name)
			continue
		}
		b, err := embedded.ReadFile("data/" + name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		created = append(created, name)
	}
	if f.bools["json"] {
		printJSON(map[string]any{"home": xhsHome(), "created": created, "kept": kept})
		return 0
	}
	fmt.Printf("XHS_HOME = %s\n", xhsHome())
	for _, n := range created {
		fmt.Println("  新建  " + n)
	}
	for _, n := range kept {
		fmt.Println("  已有  " + n + "（没动）")
	}
	fmt.Println("\n接下来：")
	fmt.Println("  1. 填 voice.md（这个号写给谁、怎么说话）和 sources.md（去哪找选题）")
	fmt.Println("  2. goal.json 填目标；house.json / topic.json 按需改")
	fmt.Println("  3. xhs status 看下一步")
	return 0
}
