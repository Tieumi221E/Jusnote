# Jusnote

**一个本地的 Markdown 笔记编辑器：纯文本，用 git 保存历史，对 AI 友好，也很轻。**
A local Markdown notebook for Windows: plain text, history in git, friendly to AI agents, small.
ローカルの Markdown ノート。プレーンテキスト、履歴は git、AI エージェントにも扱いやすく、軽量です。

- **打开就用**：一个约 18 MB 的 exe，解压即可运行，不需要安装，也不需要另装 git（Windows 11 自带所需的 WebView2 运行时）。
- **笔记就是文件**：选一个文件夹作笔记本，笔记就是里面的 `.md` 文件，任何编辑器、任何 agent 都能直接读写。
- **git 是存储层**：边写边自动存盘；按 `Ctrl+S`、切换笔记或关闭窗口时记入 git 历史。行号旁的细条标出相对上次提交改了哪些行；每篇笔记都能看历史、比较、恢复到任意一版。
- **审阅别人的改动**：别的程序或 AI agent 改了笔记，Jusnote 不会替你提交，而是列在“审阅”里：逐个文件看差异，接受或丢弃。agent 声明了身份的，历史里会记下是哪个 agent、哪个模型、哪次运行写的。你正在改的笔记被别处改了，会问你用哪一份，谁也不会被悄悄覆盖。
- **笔记之间的链接**：`[[笔记名]]` 输入时自动补全，Ctrl+单击跳转，右栏列出“链接到这篇”的笔记；重命名时一并更新指向它的链接。
- **编辑器的手感**：Markdown 高亮（标记淡化、正文优先），标题折叠，查找替换，多光标，命令面板，文件名与全文搜索，预览（公式、可勾选的任务），粘贴图片，字号与字体可调，自动换行，行号，彩虹缩进线。
- **记录类型**：内置“日志”“阅读笔记”两种结构，规则是笔记本里的纯文本文件（`.jusnote/types/`）；写的时候当场检查格式，也可以一句话记到今天的某一栏。
- **中文 / 日本語** 界面，日间 / 夜间两种外观。
- **隐私**：完全离线，不联网、不上传任何东西。笔记本自己的记录放在它隐藏的 `.jusnote` 文件夹里；程序的设置放在 exe 旁边；日志只在内存里。

版本：**0.2.0**（审阅与历史、笔记链接、给 AI agent 的完整命令行）。

## 下载与运行

1. 从 Releases 下载 `Jusnote-0.2.0-windows-x64.zip`，解压到任意文件夹。
2. 双击 `jusnote.exe`，选择一个文件夹作笔记本（可以是空文件夹，也可以是已有的 Markdown 文件夹）。之后从左上角的笔记本名字切换。

需要：

- Windows 10 / 11（64 位）。
- Microsoft Edge WebView2 运行时：Windows 11 已自带；Windows 10 若缺少，启动时会提示。

exe 没有代码签名，第一次运行时 Windows SmartScreen 可能提示“已保护你的电脑”，点“更多信息 → 仍要运行”即可。

## 保存、历史与审阅

| 什么时候 | 发生什么 |
|---|---|
| 停下打字不到一秒 | 存盘（原子写入，上一版留在 `.jusnote\backup`） |
| `Ctrl+S` | 存盘并提交这篇笔记 |
| 切换笔记、切换笔记本、关闭窗口 | 提交 Jusnote 写过、之后没被别处改过的笔记 |
| 重命名、删除、恢复旧版 | 立即提交（重命名连同被更新了链接的笔记一起） |
| 别的程序或 agent 改了文件 | 不自动提交；状态栏显示“N 处待审阅”，文件列表里标出改过的笔记（agent 改的是紫色），在“审阅”（`Ctrl+Shift+G`）里看差异，接受或丢弃 |

- **历史**（`Ctrl+Shift+H`）：这篇笔记的每一次提交，选一版就能看到它和现在的差别，“恢复这一版”会作为一次新提交记下，历史不会被改写。
- 打开一个还没有 git 仓库的文件夹时，Jusnote 会在里面建一个（分支 `main`）。已有的仓库照常使用，作者身份沿用仓库里的设置；没有设置时用 `Jusnote <jusnote@localhost>`，不改动你的 git 配置。

## 给 AI agent 用

Codex、Claude Code、Cursor、Gemini CLI 等编码 agent 可以直接在笔记本里工作：

1. 在 Jusnote 里运行命令“让 AI agent 能在这里工作…”（或 `jusnote agents -notebook <笔记本>`）。它在笔记本根目录写入 `AGENTS.md`，以及指向它的 `CLAUDE.md`、`GEMINI.md`；里面写明 exe 的完整路径、可用命令、这本笔记本的记录类型，以及规则：**用 jusnote 命令、不自己运行 git、写入加 `-no-commit` 并声明身份**。已有的同名文件不会被覆盖。
2. agent 写入时带上身份：

   ```text
   jusnote write notes/ideas.md -text "…" -no-commit -author agent -model anthropic/claude -run <会话 id>
   ```

   （也可以设环境变量 `JUS_AUTHOR=agent`、`JUS_MODEL`、`JUS_RUN`。）
3. 你在 Jusnote 的“审阅”里看它改了什么、接受或丢弃。接受后的提交带有 `Jus-Author: agent`、`Jus-Model`、`Jus-Run`，任何 git 工具都看得到。

实测：让从未见过这个笔记本的 Claude Haiku 与 Sonnet 只凭 `AGENTS.md` 完成加想法、记日志、新建阅读笔记、查反向链接四件事，逐字节核对：原文不动、没有提交、每处改动都带着它们的身份。其他 agent（Codex、Cursor、Gemini CLI、Copilot 等）读取这些文件的方式来自它们的官方文档，尚未逐一实测。

命令行对 agent 友好的地方：`jusnote help -json` 列出全部命令与参数；每个命令都有 `-json`；失败时 `-json` 下在 stderr 输出 `{"error": …, "code": …}`；退出码固定（0 成功、1 失败、2 用法错误、3 冲突：文件在你读取之后被改过）；`read -json` 给出版本，`write -base <版本>` 保证不覆盖别人的改动；空的标准输入不会被当成内容；`append` 只在末尾追加、其余逐字节不动；`-file` 从文件读内容，避开 shell 对引号和反引号的转义；agent 用自己的编辑工具改完后，`record` 声明是它改的。

## 命令行

同一个 exe 也是命令行工具，和编辑器用的是同一套代码。所有命令都接受 `-notebook <文件夹>`（默认当前文件夹）和 `-json`。

```text
jusnote                              打开编辑器（默认打开上次的笔记本）
jusnote list | read <路径> | search <文字> | links <路径> | types | info
jusnote write <路径> -text "…" | -file <文件>   原子写入并提交（-no-commit 只写不提交；-base 版本冲突检查）
jusnote append <路径> -file <文件>     在末尾追加（其余不动）
jusnote attach <笔记> <文件>          把图片等文件放到笔记旁的 attachments/，输出要插入的 Markdown
jusnote record <路径…>                声明这些改动是谁做的（agent 用自己的工具改完文件后）
jusnote capture -section 工作 -text "…"   记到今天的日志（-type、-date）
jusnote check <路径>                   按记录类型检查格式
jusnote rename <旧> <新> | delete <路径>   重命名（同时更新链接）/ 删除
jusnote status | diff <路径> | discard <路径> | commit <路径…> [-all]
jusnote log [路径] | show <提交> <路径> | restore <提交> <路径>
jusnote agents | init | help [-json] | version
```

## 快捷键

| 键 | 作用 |
|---|---|
| Ctrl+N | 新建笔记 |
| Ctrl+P / Ctrl+Shift+F | 搜索笔记名与内容 |
| Ctrl+F / Ctrl+H | 在当前笔记中查找 / 替换 |
| Ctrl+S | 保存并提交 |
| Ctrl+Shift+G | 审阅别处的改动（再按一次收起；处理完会自动收起） |
| Ctrl+Shift+H | 这篇笔记的历史 |
| Alt+← / Alt+→ | 后退 / 前进（在打开过的笔记之间；鼠标侧键也可以） |
| `[[` | 插入指向笔记的链接（自动补全） |
| Ctrl+V / 拖入 | 粘贴图片或文件（存到笔记旁的 `attachments/`） |
| Ctrl+K | 命令面板 |
| Ctrl+O | 文件 |
| Ctrl+J | 大纲、链接、改动与历史 |
| Ctrl+Shift+V | 预览 / 源码 |
| Ctrl+B / Ctrl+I / Ctrl+E | 粗体 / 斜体 / 行内代码 |
| Ctrl+单击 | 打开链接（`[[笔记]]`、`jus://note/…` 在 Jusnote 里打开，网页链接交给浏览器） |
| Ctrl + / Ctrl - / Ctrl 0 | 字号 |
| F1 | 全部快捷键 |
| Esc | 关闭面板 |

预览里：点任务框就勾选（写回原文）；`$…$`、`$$…$$` 显示为公式。文件列表里右键一篇笔记可以重命名、删除、复制 `jus://` 链接或在资源管理器中显示。

## 数据放在哪里

| 什么 | 在哪里 |
|---|---|
| 笔记 | 你的笔记本文件夹里的 `.md` / `.markdown` 文件 |
| 粘贴的图片与文件 | 笔记旁边的 `attachments\` |
| 历史 | 笔记本自己的 git 仓库（`.git`） |
| 记录类型的规则 | 笔记本里隐藏的 `.jusnote\types\`（随 git 同步，可以编辑、分享） |
| 上一版备份；上次打开的笔记、光标位置、agent 声明的来源 | `.jusnote\backup\`、`.jusnote\cache\`（不进 git） |
| 给 agent 的说明（只在你要求时写入） | 笔记本根目录的 `AGENTS.md`、`CLAUDE.md`、`GEMINI.md` |
| 最近的笔记本、界面偏好 | exe 旁边的 `jusnote-data\`（不可写时用 `%AppData%\jusnote`） |
| 日志 | 只在内存里；需要时在“设置 → 复制诊断日志” |

删除 `jusnote-data` 和笔记本里的 `.jusnote` 就会清除 Jusnote 留下的痕迹；笔记和 git 历史是你的，Jusnote 不会删除。

## 从源码构建

需要 Go 1.27+ 和 Node 22+。

```text
npm --prefix web ci
./build.ps1            # 生成 bin\jusnote.exe
go test ./...          # 测试不依赖任何外部程序（命令行的端到端测试会用 go build 编一个临时的 exe）
npm --prefix web test
./release.ps1          # 测试、构建并打包 dist\Jusnote-<版本>-windows-x64.zip
./tools/bench.ps1      # 在本机测量：后端基准、命令行冷启动、真实窗口里的自检；结果写到 bench\（不进 git）
```

`jusnote gui -selftest -notebook <一份副本>` 在真实窗口里跑一遍页面自检：存盘、冲突、撤销、审阅（接受 / 丢弃）、恢复旧版、粘贴图片、改名更新链接、预览、搜索、切换语言与外观，每项延迟都有上限（例如打开笔记 p95 ≤ 100 ms、按键到绘制 ≤ 两帧、切换语言 ≤ 50 ms），正确性与延迟在同一份 JSON 报告里。

外观遵循 Jus 系列的统一设计语言（与 Jusplay 相同）。

## 许可证

Jusnote 以 MIT 许可证发布（[LICENSE](LICENSE)）。exe 内包含的第三方软件与字体（Go 运行时、go-git、go-webview2、CodeMirror、marked、DOMPurify、KaTeX 等，以及思源黑体的 SIL OFL 1.1 子集）的许可证见 [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt)。
