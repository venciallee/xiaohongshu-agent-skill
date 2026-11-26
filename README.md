# 小红书 agent skill

11 个 Claude / Codex skill，加一个 Go 写的本地检查工具 `xhs`，用来做小红书图文：选题、写笔记、封面和标题打分、去 AI 腔、发布前检查、出卡片图、找对标、72 小时复盘、排周计划、看主页、整理评论区、把长内容拆成几篇。

结构参考了 [instagram-agent-skill](https://github.com/Jakeschincariol/instagram-agent-skill)：单一职责的 skill、共享一份 voice 档案、真能运行的本地工具、人工发布闸门、不编造。但小红书和 Instagram 差得够多，几乎每个 skill 都重新想过，下面「和 Instagram 版不一样的地方」写了为什么。

**什么都不会被自动发出去。** 这些 skill 只写。发布、评论、改资料，都是你自己在 App 里做。

![示例笔记的 5 张卡片](docs/sample-preview.png)

## 安装

需要 Go 1.22+（编译 `xhs`），出图需要本机有 Chrome / Chromium / Edge。

```bash
git clone https://github.com/venciallee/xiaohongshu-agent-skill.git && cd xiaohongshu-agent-skill
(cd tool && go install .)                       # 装 xhs 到 $(go env GOPATH)/bin
cp -r skills/xhs-* ~/.claude/skills/            # Claude Code
cp -r skills/xhs-* ~/.codex/skills/             # Codex（可选）
```

或者在 Claude Code 里当插件装（`xhs` 仍然要按上面装一次）：

```
/plugin marketplace add venciallee/xiaohongshu-agent-skill
/plugin install xiaohongshu-agent
```

不想全局装 `xhs`：直接用仓库里的 `bin/xhs`，第一次运行会自动编译。

然后花十分钟在 voice 档案上：

```bash
mkdir -p ~/.xiaohongshu
cp templates/voice.md ~/.xiaohongshu/voice.md   # 填它，或者把三篇旧笔记发给 Claude 让它照着写
cp examples/house.rsi-engineering.json ~/.xiaohongshu/house.json   # 账号规则，按需改
```

状态目录默认 `~/.xiaohongshu`，用 `XHS_HOME` 改。里面会有 `voice.md`、`house.json`、`log.jsonl`（发布记录）、`swipe.md`（对标）、`review.md`（复盘）、`plan.md`（周计划），以及可选的 `data/*.json`（覆盖内置词库，`xhs data --dump ~/.xiaohongshu/data` 导出来改）。

## 11 个 skill

| skill | 做什么 |
| --- | --- |
| `xhs-topic` | 从 X、官方 changelog / 博客 / 论文、小红书搜索里找候选；每个先写洞察句、过三问，再按打分表筛，给 3 个候选。 |
| `xhs-note` | 主流程。选题 → 封面大字 + 3 个打过分的标题 + 正文 + 话题 + 卡片文案 + 置顶评论 → `post.json` → 去 AI 腔 → 发布前检查 → 交付。 |
| `xhs-cards` | 1080×1440 白底卡片，调本机 Chrome 出 PNG，检查溢出，拼预览图。 |
| `xhs-check` | 发布前检查。先画出双列流里的样子，再查平台限制、导流、诱导、极限词、AI 声明和账号自己的规则。 |
| `xhs-human` | 去 AI 腔。能安全改的自动改，套话和句式标出来你改，再用五项读者视角自检打分。 |
| `xhs-viral` | 找对标。按「赞藏 ÷ 该账号中位数」排低粉爆文，算收藏/点赞比，归到 18 个句式，写 swipe file。 |
| `xhs-review` | 72 小时复盘。漏斗拆成四段，和本账号中位数比，分清是分发、封面标题、内容兑现还是主页的问题。 |
| `xhs-plan` | 排一周。每篇写清「在测什么」，一篇只测一个变量。 |
| `xhs-profile` | 主页 11 项 100 分评分，按丢分排序重写。 |
| `xhs-reply` | 整理自己笔记下的评论：七类分拣，写短草稿由你改了亲手发，问题标成下一篇。 |
| `xhs-repurpose` | 把一篇长文 / 论文 / 视频拆成几篇各自独立的笔记。 |

一篇笔记的路径：

```
xhs-topic ─▶ xhs-note ─▶ xhs-human ─▶ xhs-check ─▶ xhs-cards ─▶ 你在 App 里发
                 ▲                                                      │
   xhs-viral ────┤                                               xhs log add
   xhs-plan  ────┘                                                      │
                 ◀──── xhs-plan / xhs-profile / xhs-reply ◀──── xhs-review（72h）
```

## 和 Instagram 版不一样的地方

**1. 钩子在别的地方。** Reels 的钩子是前两秒，所以 IG 版的核心是 hook 打分和节拍表，caption 检查的是 feed 折叠前的 125 个字符。小红书图文的钩子在双列流里：封面大字和标题同时出现，读者在一秒里决定点不点。所以这里把「125 字可见窗」换成了一个双列流预览框，把 hook 打分换成封面 / 标题打分，并且把两者分开：封面给眼睛（≤14 字，最好 3-8 字），标题给搜索（≤20 字，前半句放搜索词）。

**2. 能看到的数据不一样。** 小红书上别人的笔记只看得到点赞、收藏、评论，看不到浏览量。所以「爆了多少倍」用赞藏 ÷ 该账号最近笔记赞藏的中位数，这就是小红书圈子说的「低粉爆文」，这里算成一个数。另外加了收藏 / 点赞比：科普和工具类内容里，收藏远多于点赞说明读者是存起来要用的，这比赞藏总量更能说明一个句式值不值得学。

**3. 去 AI 腔，不去 AI 标识。** IG 版的 humanizer 会删掉零宽字符这类不可见字符。这里刻意不这么做：《人工智能生成合成内容标识办法》（2025-09-01 施行）第十条禁止删除、隐匿 AI 标识；小红书 2026-02-12 的公告说未主动标识的 AI 内容会被加标、限制分发；2026-04-27 的 AI 治理规则要求 AI 生成或润色的内容主动声明，并把「传授规避审核的方法」列为违规。所以 `xhs human` 只处理读者能看到的模板感，不碰任何不可见字符和元数据；`xhs check` 把 AI 声明列为 FAIL 项。目标是读着像人话，不是骗过谁。

**4. 删了三个 skill。** IG 版的 `ig-comment`（每天去 10 个账号下面评论）、`ig-dm`、`ig-story` 没有搬过来。小红书 2026-03-10 的《关于打击 AI 托管运营账号的治理公告》点名了「在评论、私信、群聊等场景中模拟真人互动」，偶发会被警告、限流，全流程托管直接封号。用 AI 批量给别人写评论，正好落在这条线上。私信在小红书上和导流规则贴得太近；Story 没有对应物。`xhs-reply` 只处理自己评论区，只出草稿，你改了亲手一条条发。

**5. 加了两个 skill。** `xhs-topic`：技术科普号最贵的一步是选题，IG 版没有这一步。`xhs-check`：中文平台的风险词（导流、诱导互动、极限词）和 IG 的 hashtag 上限完全是两套东西。

**6. 中文不能用正则硬换词。** 英文里 leverage → use 换完句子还通顺；中文里「赋能」换成什么都要看整句。所以 `xhs human` 只自动处理删了一定通顺的（「值得注意的是」「在当今……时代」「进行优化」→「优化」），其余全部标记。代价是自动处理的那部分涨分很少：一段刻意写烂的稿子，跑完自动处理只从 15.6 涨到 16.3。中文 AI 腔主要在句式上（「不是 A 而是 B」、排比三连、首先其次最后），那一步只能人改。

**7. 工具是一个 Go 二进制。** 九个子命令，只用标准库，词库和规则用 `go:embed` 打进去，`$XHS_HOME/data/` 下放同名文件就能覆盖。

## 工具

```bash
xhs title   titles.txt --keywords "Claude Code"    # 标题打分 + 句式归类（--cover 给封面大字打分）
xhs human   draft.txt --report -o clean.txt        # 去 AI 腔
xhs detect  draft.txt [clean.txt]                  # 五项自检，可对比前后
xhs check   post.json                              # 发布前检查
xhs cards   post.json --render                     # 出卡片 PNG + 溢出检查 + 预览图
xhs swipe   captured.tsv --out swipe.md            # 对标排序
xhs eval    captured.tsv                           # 用你自己的对标数据检验标题打分准不准
xhs review  notes.csv                              # 72 小时复盘
xhs log     add post.json | list                   # 发布记录
```

不联网，不上传，不登录，只读你给它的文本和表格。

### 发布前检查

一篇刻意写坏的笔记（`examples/bad-post.json`）：

```
  双列流里大概长这样（示意，字号按缩略图估算）
  +------------------------+
  |   全网最全的AI编       |
  |   程保姆级教程合       |
  |   集                   |
  +------------------------+
    Anthropic发布Claude新
    功能！！程序员要失业了
    …（后面大概率被截掉）

  FAIL   标题 1         40 / 20 字，超了：Anthropic发布Claude新功能！！程序员要失业了，史上最强AI编程工具
  WARN   标题 1         公司做主语（Anthropic……发布），本账号规则不这么写。把读者能用的那件事放前面。
  WARN   封面大字       16 字，超过 14，缩略图上读不完
  WARN   开头第一句     开场白 / 自我介绍开头。第一句写读者正在经历的场景
  FAIL   导流           「评论扣 1 领资料」：评论区扣 1 领
  WARN   导流           联系方式字样：加我、vx
  FAIL   诱导互动       「点赞过 100 就更新」：点赞过 100
  WARN   极限词         最强、最全、全网最、史上最
  WARN   标题党         震惊、程序员要失业
  FAIL   身份句         「我是一名做 agent 工程的程序员」。正文不写自我介绍……
  WARN   第一人称经历   1 句「我试过 / 实测」类的话等本人确认……
  FAIL   AI 声明        post.json 没有 ai_disclosure……
  ...
  结论  FIX
```

示例笔记（`examples/sample-post/post.json`）同一套检查：`READY`。

### 标题和封面

```
标题排序  ·  3 条
->  66.2 可用  Claude Code先跑测试再交活
           句式 - 未归类   最弱：信息差（0 个反差 / 代价词）
    63.2 可用  CLAUDE.md加这段，改完先跑测试
           句式 - 未归类   最弱：信息差（0 个反差 / 代价词）
    50.2 可用  AI说改好了却没跑测试？抄这段
           句式 #1 直接抄   最弱：搜索词（没有你要的搜索词（Claude Code、CLAUDE.md、AI编程））

封面大字排序  ·  3 条
->  73.2 强    改好了≠测过了
    73.2 强    它没跑测试
    59.0 可用  AI编程的验证问题
```

五项：长度用得对不对、有没有具体的东西、有没有搜索词（越靠前越好）、有没有信息差或代价、关键信息是否在前 8 个字。一票否决：超字数、极限词、标题党、诱导互动、两个以上 emoji、连续感叹号、公司做主语（按 `house.json`）。

### 去 AI 腔

```
去 AI 腔报告
自动处理 8 处，9 个词标记待改，10 类句式需要你重写

4. 套话（没改，逐个看）
    1x  赋能  [黑话]  写清楚帮谁做成了什么。
    1x  保姆级  [模板感]  满屏都是，已经不是信号了。说具体能照着做的那件事。
5. 句式（没改，你来重写）
    1x  「不是 A，而是 B」
        直接说 B。先立一个靶子再推倒，是最典型的中文 AI 腔。
    1x  排比三连（同字开头的三个短分句）
    3x  emoji 开头的列表
```

五项自检，示例笔记正文：

```
  句长起伏   ##################......  73.1
  具体度     ######################## 100.0
  套话密度   ######################## 100.0
  格式指纹   ######################## 100.0
  口语感     #################.......  70.2
  总分       ####################....  81.3   PASS
```

### 复盘

```
复盘  ·  6 篇  ·  中位数：曝光 1.1万 / 观看 1250 / 点击率 12.0% / 收藏率 16.0% / 涨粉率 1.2%

诊断（和本账号中位数比）
  一张图看懂MCP到底是什么
    - 主页：看的人多、关注的少。这篇没失败，是主页没接住，去 xhs-profile。
    - 信号：观看是中位数的两倍以上，收藏率也不低。把它的选题类型和句式记下来，下周复用一次。
  AI编程入门的一些思考
    - 封面 / 标题：曝光正常，点击率只有中位数的 35%。问题在双列流那一秒，不在正文。
  为什么AI总说测过了？
    - 分发：曝光只有中位数的 24%。先看这篇有没有违规 / 限流提示，再看话题和标题里的搜索词。
```

（数据是 `examples/notes.sample.csv` 里编的示例。）

## 诚实的部分

**标题打分没有验证过。** IG 原版拿 74 个真实 hook 测过，结论是能挡烂 hook（AUC 0.83），挑不出爆款（AUC 0.56）。这里的标题打分还没在真实小红书数据上测过，所以加了 `xhs eval`：用 `xhs-viral` 收的对标数据（≥40 条）跑一遍，看它在你的领域能不能分开上 1/3 和下 1/3。在示例数据上总分 AUC 是 0.56，接近抛硬币，只有「搜索词」一项有点区分度（0.75），而且 12 条编出来的样本本身什么都说明不了。拿到真实数据之前，把它当成挡烂标题的工具，别拿它挑标题。

**五项自检是启发式。** 量的是读者觉得像模板的那些东西，阈值是拍的，没有用真实数据校准过。

**平台规则会变。** `platform.json` 里写了核实日期（2026-11-26）和出处。标题 20 字、正文 1000 字、18 张图、AI 声明、AI 托管治理都有来源；话题数量没找到官方上限，所以按账号规则（`house.json`）查，不当平台规则。和 App 当下提示冲突时，App 是对的，改文件。

**导流和极限词清单不完整。** 平台不公开完整规则，这里是根据公开报道和二手整理写的。命中了不一定违规（「微信」可能是在讲微信小程序），没命中也不代表安全。

**什么都不发。** 不登录小红书，不点发布，不设定时，不自动评论或私信，不抓取。`xhs-viral` 和 `xhs-review` 读数据，用的是你自己已登录的浏览器、你在场，或者你截图粘贴。

**不编造。** 不编数字、经历、出处。缺一个数字，草稿里就留 `{{你的数字}}` 并标出来。「我试过」这类句子没你点头不会出现在定稿里。

## 接到已有的每日流水线

已经有自己的每日出稿 skill（比如每天早上定时跑的那种），不用换掉它，在对应步骤调这几个命令就行：写完正文跑 `xhs human` 和 `xhs detect`，标题跑 `xhs title`，交付前跑 `xhs check`，出图用 `xhs cards --render`，发完 `xhs log add`，72 小时后 `xhs review`。

## 文件

```
skills/xhs-*/SKILL.md          11 个 skill
skills/xhs-profile/rubric.json 主页 100 分评分表
tool/                          xhs 的 Go 源码（标准库，无依赖）
tool/data/slop.json            去 AI 腔词库：填充语、套话、句式（改它）
tool/data/titles.json          18 个封面 / 标题句式，带模板、示例、陷阱和归类正则
tool/data/platform.json        平台限制、导流、诱导、极限词，带核实日期和出处
tool/data/house.json           账号规则默认值（$XHS_HOME/house.json 覆盖）
templates/voice.md             voice 档案模板
examples/                      示例笔记、坏笔记、对标和复盘的示例表、RSI Engineering 的预填档案
bin/xhs                        启动器：第一次运行自动编译
```

## License

MIT。结构参考 instagram-agent-skill（MIT），见 `NOTICE`。
