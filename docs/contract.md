# 状态文件和接口约定

这份文档给两类人看：想用定时任务 / 编排器（cron、Claude 的定时任务、任意 agent 平台）驱动这套 skill 的人，和想给它加功能的人。

原则只有一条：**所有状态都在文件里，不在对话里。** 每次启动的都可能是一个全新的会话，它靠这些文件知道发生过什么、现在该做什么。

## 目录

`$XHS_HOME`，默认 `~/.xiaohongshu`。`xhs init` 生成用户要填的那几个文件。

```
$XHS_HOME/
  voice.md           用户填   账号人设：写给谁、领域、选题类型、语气、节奏、不能碰的
  sources.md         用户填   信息源：去哪找选题、读者在哪说话
  goal.json          用户填   目标、记数据的时间点、复盘周期、「没起色」的定义
  topic.json         用户填   选题打分表（可选，没有就用内置）
  house.json         用户填   账号规则：卡片数、话题数、身份句、卡片风格（可选，只写要改的字段）
  audience.md        xhs-audience 写      读者画像、搜索词、问题、用词
  playbook.md        xhs exp close 追加   验证过的规则，带实验编号和证据；人也可以改
  swipe.md           xhs swipe 写         对标排序结果
  plan.md            xhs-plan 写          周计划
  review.md          xhs review --md 追加 复盘记录（给人看）
  log.jsonl          xhs log add          发布记录
  data.jsonl         xhs data add         每篇笔记的数据快照
  account.jsonl      xhs goal log         粉丝数快照
  reviews.jsonl      xhs review --record  复盘摘要（给 xhs status 判断有没有起色）
  experiments.json   xhs exp              实验账本
  pool.json          xhs pool             候选选题池
  posts/<日期>-<slug>/post.json           草稿和已发笔记；cards/ 在同目录
  data/*.json        可选                 覆盖内置词库 / 句式库 / 平台规则（xhs defaults --dump 导出）
```

`.jsonl` 每行一个 JSON 对象，只追加不改写。`.json` 整个文件重写（先写临时文件再改名）。

## post.json

```jsonc
{
  "slug": "claude-code-verify",        // 英文短名，也是笔记 id；不填就用标题哈希
  "date": "2026-11-26",                // 写稿日期
  "topic": "……",
  "insight": "……",                     // 洞察句
  "testing": "……",                     // 本篇在测什么
  "category": "一线做法",               // voice.md 里的选题类型
  "exp": "E2",                         // 挂的实验（对照组留空）
  "keywords": ["……"],                  // 搜索词
  "cover": {"big": "……", "sub": "……"}, // 封面大字 / 副标题
  "titles": ["……", "……", "……"],        // 3 个标题备选
  "title": "",                         // 用户选定的标题（可选）
  "body": "……",                        // 正文
  "tags": ["……"],                      // 话题
  "cards": [{"head": "……", "text": "……", "code": "……", "note": "示意"}],
  "pinned_comment": "……",              // 置顶评论草稿
  "sources": [{"name": "……", "url": ""}],
  "first_person_confirmed": ["……"],    // 用户确认过的第一人称原句
  "ai_disclosure": true                // AI 参与了就是 true，发布时在 App 里声明
}
```

卡片文字里 `==重点==` 渲染成荧光笔划线。`content` / `topics` / `slides` 作为 `body` / `tags` / `cards` 的别名也认。

## 记录格式

**log.jsonl**

```json
{"id":"n3","logged":"2026-11-18 11:30","published":"2026-11-18 09:30","slug":"n3","title":"……","cover":"……",
 "formula_id":9,"formula":"一张图讲清","category":"概念","exp":"E1","insight":"……","testing":"……",
 "keywords":["……"],"cards":4,"dir":"/…/posts/2026-11-18-n3"}
```

**data.jsonl**：同一篇可以多行，`hours` 是采集时距发布的小时数。

```json
{"note":"n3","title":"……","captured":"2026-11-20 10:00","hours":48.5,"impressions":30000,"views":4800,
 "ctr":0.16,"avg_time":33,"likes":410,"collects":900,"comments":61,"shares":120,"follows":22,"source":"screenshot"}
```

**account.jsonl**：`{"captured":"2026-11-26 09:00","followers":668}`

**reviews.jsonl**：`medians` 是全部可比笔记的中位数，`recent` 是最近 `recent_days` 天发布的笔记的中位数（判断有没有起色用它）。

```json
{"at":"2026-11-26 11:24","notes":6,"mature":6,"compare_hours":48,
 "medians":{"impressions":11500,"views":1400,"ctr":0.122,"avg_time":30,"save":0.194,"follow":0.018,"comment":0.007},
 "recent":{"views":1400,"follow":0.018},"recent_notes":3,"followers":668,"followers_per_day":12,"findings":["……"]}
```

**experiments.json**

```json
[{"id":"E1","created":"…","hypothesis":"…","variable":"第2张卡","change":"…","metric":"save_rate","min_notes":5,
  "status":"closed","result":"supported","decision":"…","rule":"…","evidence":"实验组 n=5 …","closed":"…"}]
```

`metric` 可选：`views` `impressions` `ctr` `save_rate` `follow_rate` `follows` `avg_time`。

**pool.json**

```json
[{"id":"P1","added":"…","title":"…","insight":"…","source":"…","category":"…",
  "scores":{"有用":3,"新":2},"total":9,"pass":true,"expires":"2026-12-10","status":"open","used_by":""}]
```

`status`：`open` / `used` / `dropped`。

## 配置

**goal.json**

| 字段 | 默认 | 含义 |
| --- | --- | --- |
| `metric` / `target` / `deadline` | followers / 0 / "" | 目标；target 为 0 表示还没定 |
| `capture_hours` | [48, 168] | 发布后第几小时记数据 |
| `compare_hours` | 48 | 复盘和实验在哪个时间点比较 |
| `review_every_days` | 2 | 多久复盘一次 |
| `recent_days` | 7 | 「近期」窗口，判断有没有起色用 |
| `min_notes_per_experiment` | 5 | 实验至少几篇才读数 |
| `stall.reviews` / `stall.min_gain` | 3 / 0.1 | 连续几次复盘、三个指标都没涨过 10% 就算没起色 |

**topic.json**：`{"threshold": 8, "items": [{"key":"有用","max":3,"guide":"…"}, …]}`

**house.json**：见 `xhs defaults --dump` 导出的 house.json，每个字段都有说明。

## 命令约定

- 所有命令都支持 `--json`，输出一个 JSON 值到 stdout；人读的提示和错误走 stderr。
- 退出码：0 成功 / 通过；1 有问题（检查没过、有卡片溢出、自检不是 PASS）；2 用法错误或输入有误。
- 不联网，不登录任何平台，不发布任何东西。

## xhs status --json

编排器每次启动先跑它。关键字段：

| 字段 | 含义 |
| --- | --- |
| `missing_config` | 缺哪些配置文件 |
| `goal` | 目标轨迹：current / per_day / eta / need_per_day / on_track |
| `drafts` | posts/ 里还没 log 的草稿 |
| `posts_today` | 今天发了几篇 |
| `data_due` | 到了记数据时间点的笔记（`late` 表示已经错过窗口） |
| `review_due` / `days_since_review` | 该不该复盘 |
| `stall` | 连续没起色的次数、阈值、是否触发 |
| `open_experiments` | 在跑的实验和读数（`enough` 表示够篇数了） |
| `pool_open` / `pool_top` | 选题池 |
| `audience_age_days` | audience.md 多少天没更新（-1 表示没有） |
| `next` | 建议的下一步，按优先级排 |

`next[].action` 的取值：

| action | 交给 | 含义 |
| --- | --- | --- |
| `setup` | 用户 | 缺 voice.md / goal.json / sources.md |
| `capture_data` | xhs-review | 有笔记到了记数据的时间点 |
| `log_followers` | xhs-review | 36 小时没记粉丝数 |
| `review` | xhs-review | 到复盘周期了 |
| `deep_diagnosis` | xhs-review → xhs-audience → xhs-viral | 连续没起色 |
| `close_experiment` | xhs-review | 实验够篇数了 |
| `start_experiment` | xhs-review | 没有在跑的实验 |
| `await_publish` | 用户 | 有草稿没记发布 |
| `write_today` | xhs-topic → xhs-note | 今天还没稿子 |
| `refresh_audience` | xhs-audience | 没有或 30 天没更新读者分析 |

`status` 只给建议。节奏（几点跑）、闸门（什么要用户点头）、要不要照做，由编排器决定。
