# 领域包：AI 编程 / AI 提效科普

和通用版用同一套 18 个句式，示例换成了 AI 编程领域（Claude Code、Cursor、MCP……），
「新手入门」「内行做法」等几条的匹配规则也更贴这个领域的说法（比如「没写过代码」「工程师自己这么用」）。

```bash
mkdir -p ~/.xiaohongshu/data
cp packs/ai-tech/titles.json ~/.xiaohongshu/data/titles.json
```

想做别的领域的包：复制 `tool/data/titles.json`，把 example / cover / trap 换成你领域的真实说法，
match 正则补上你领域读者的口头语，放进 `packs/<领域>/`，见 CONTRIBUTING.md。
