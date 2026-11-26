---
name: xhs-cards
description: >-
  把小红书笔记的封面和卡片排成 1080x1440（3:4）的白底图，用本机 Chrome 无头截图出 PNG，自动检查字有没有溢出，拼一张预览图。
  用户说「出图」「做卡片」「生成封面」「渲染小红书图片」「卡片排一下」，或者 xhs-note 写完 post.json 之后，都用这个。
---

# xhs-cards

```bash
xhs cards post.json                  # 只生成 HTML（card-01.html …）
xhs cards post.json --render         # 调本机 Chrome 出 PNG，检查溢出，拼 preview.png
xhs cards post.json --render --chrome "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
```

## 读 / 写

| | 文件 | 用途 |
| --- | --- | --- |
| 读 | `post.json` | 封面和卡片文字 |
| 读 | `house.json` | `card_style`（颜色、字体）、`handle_on_cards`、`max_cards` |
| 写 | post.json 同目录 `cards/` | card-01.html… / .png、preview.png |

输出在 post.json 同目录的 `cards/` 下。账号名（`handle_on_cards`）、颜色、字体都来自 `house.json`。溢出的卡片图上会盖一条红色横幅，命令行退出码非 0。

## 风格，是刻意收着的

小红书满屏的 AI 图卡有几样共同点：渐变底、玻璃拟态圆角卡、emoji 图标一排、机器人插画、条形图、四五种颜色。读者一眼认得出来。这里反着来：

- 白底，粗黑体，一个强调色（`house.json` 的 `card_style.accent`），只用在封面短横线、代码块左边线和 `==重点==` 的荧光笔划线上。
- 不放 logo、不放「第 N 期」、不放日期。账号名小字放左下角，截图传出去也带着你。
- 页码右上角（2 / 5），读者知道还有几张。
- 卡片上只放做法。来源、统计口径、免责声明挪到置顶评论（`xhs check` 会查）。

账号有自己的设计规范就改 `house.json` 的 `card_style`，不要在模板里另起一套颜色。

## 卡片的结构

```
1      封面    post.json 的 cover.big + cover.sub。字号自动按字数选，最多两行。
2      兑现    封面承诺的东西就在这一张。读者右滑一次没看到答案，就走了。
3..N   一张一个做法。大标题 ≤16 字，正文 ≤70 字；代码 ≤12 行。
最后   能截图的那张：要复制的配置 / 提示词放这里，或者放第 2 张。
```

最多张数看 `house.json` 的 `max_cards`。平台上限 18 张，但一张卡讲一件事；张数远超账号习惯，通常说明是两篇。

文字里用 `==这样==` 标重点，一张卡最多一处。

## 流程

1. `xhs cards post.json --render`
2. 有溢出：**删字或拆卡，别缩字号。** 读者在手机上看的是三分之一大小，字号已经是下限。
3. 打开 `preview.png` 看整篇的节奏，再逐张打开看：
   - 封面在缩略图大小（预览图里那么大）能不能一眼读完？
   - 第 2 张有没有兑现封面？
   - 有没有一行只剩一两个字的孤字？有就改一下措辞。
   - 代码块里有没有手机上看不清的长行？
4. 没问题再交付。PNG 按 card-01、card-02 的顺序就是上传顺序。

## 需要一张图解的时候

模板只排文字和代码。真需要示意图（比如「一张图看懂 MCP」），拿生成的 `card-0N.html` 当底，在 `.fit` 里手写 SVG：框不超过 5 个、箭头只走一个方向、字和正文一样大。画完用 Chrome 打开这个 HTML 截图（或者临时把它放回 cards 目录重跑 `--render`），按上面第 3 步逐项看一遍。标「示意」。

## 底线

- 不用 AI 生图做封面人物或产品图。真要截图，用用户自己的截图。
- 不把别人笔记的卡片拿来改字。学结构，不抄设计。
- 卡片上有 AI 参与，发布时一样勾选 AI 声明。
