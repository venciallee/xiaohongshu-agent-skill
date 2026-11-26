# 参考 loop

这套 skill 本身不带调度：每个 skill 只管一件事，状态都在 `$XHS_HOME` 的文件里（见 [contract.md](contract.md)）。怎么把它们串成一个持续运行的 loop，由你的编排方式决定：手动、cron、Claude 的定时任务、或者任何 agent 平台。

下面是一个能直接改着用的参考。

## 一圈长什么样

```
          ┌─────────────────────────────────────────────────────────┐
          ▼                                                         │
  xhs status ──▶ write_today ──▶ xhs-topic ──▶ xhs-note ──▶ 用户在 App 里发
      │                              ▲                         │
      │                        audience.md                 xhs log add
      │                        playbook.md                     │
      │                                                        ▼
      ├──▶ capture_data ──▶ xhs data add / xhs goal log ◀── 48h / 7d
      │
      ├──▶ review ──▶ xhs review --record ──▶ 诊断 ──▶ xhs exp new
      │                                                    │
      ├──▶ close_experiment ──▶ xhs exp close --rule ──▶ playbook.md
      │
      └──▶ deep_diagnosis ──▶ xhs-audience ──▶ xhs-viral ──▶ 一个原因、一个实验
```

## 两个定时任务

**每天（比如早上 8 点）**：

1. `xhs status --json`
2. 有 `capture_data` / `log_followers`：请用户发创作中心截图（或在用户已登录的浏览器里读），`xhs data add`、`xhs goal log`。
3. 有 `write_today`：`xhs-topic` → `xhs-note` → `xhs-cards`，把卡片、标题备选、正文、话题、置顶评论发给用户。
4. 有 `await_publish`：提醒用户；用户说「发了」就 `xhs log add`。

**每 N 天（`goal.json` 的 `review_every_days`）**：

1. `xhs status --json`
2. `review`：`xhs review --record`，按漏斗诊断，必要时开实验。
3. `close_experiment`：`xhs exp show` → `xhs exp close --rule`。
4. `deep_diagnosis`：`xhs-audience` → `xhs-viral` → 只提一个原因和一个实验。
5. 把复盘结论和「离目标还有多远」发给用户。

## 闸门：什么必须等用户

不管怎么编排，这几件事都应该停下来等人：

- **发布**。这个 pack 不发布，也不应该被编排去发布：小红书 2026-03-10 起打击 AI 托管账号，偶发代发会被警告、限流，全流程托管直接封号。
- **第一人称经历**（「我试过」「实测」）。
- **评论和私信的回复**：只出草稿，用户亲手发。
- **换方向、改人设、改目标**。

调句式、调卡片张数这类小调整，可以让编排器自己决定，但要记进实验账本并告诉用户。

## 给编排器的 prompt 骨架

```
你负责运营一个小红书账号的内容 loop。工具是 xhs-* skill 和 xhs 命令行，
所有状态在 $XHS_HOME。每次启动先跑 xhs status --json，按 next 决定做什么，不靠对话记忆。

账号的定位、读者、信息源、目标分别在 voice.md、audience.md、sources.md、goal.json。
playbook.md 里是验证过的规则，必须遵守。

[每天] 记数据 → 出稿（xhs-topic → xhs-note → xhs-cards）→ 交给我 → 我说「发了」就 xhs log add
[每 N 天] xhs review --record → 诊断 → 一次只改一个变量（xhs exp）→ 结论写进 playbook
[没起色] 停止逐篇小修：xhs-audience → xhs-viral → 一个原因、一个实验

必须等我：发布（永远我自己发）、第一人称经历、评论私信回复、换方向。
不编数字和经历；在任何平台上只读；AI 参与的笔记提醒我勾选 AI 声明；样本不够就说不够。
```

把账号自己的东西（定位、节奏、目标数字、特殊规则）写进 `$XHS_HOME` 的配置文件，而不是写进 prompt：prompt 换了平台还能用，配置换了账号还能用。
