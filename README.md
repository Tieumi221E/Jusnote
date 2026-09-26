# Jusnote

**一个本地的 Markdown 笔记编辑器：纯文本，用 git 保存历史，对 AI 友好，也很轻。**
A local Markdown notebook for Windows: plain text, history in git, friendly to AI agents, small.
ローカルの Markdown ノート。プレーンテキスト、履歴は git、AI エージェントにも扱いやすく、軽量です。

- **打开就用**：一个约 17 MB 的 exe，解压即可运行，不需要安装，也不需要另装 git（Windows 11 自带所需的 WebView2 运行时）。
- **笔记就是文件**：选一个文件夹作笔记本，笔记就是里面的 `.md` 文件，任何编辑器、任何 agent 都能直接读写。
- **git 是存储层**：边写边自动存盘；按 `Ctrl+S`、切换笔记或关闭窗口时记入 git 历史。行号旁的细条标出这篇笔记相对上次提交新增、修改、删除了哪些行。
- **不覆盖别人的改动**：别的程序或 agent 改了你正在看的笔记，会当场载入；你也在改时，会问你用哪一份。Jusnote 只提交它自己写过的文件，别人的改动留在“未提交”里，由你看过后再提交。
- **编辑器的手感**：Markdown 高亮（标记淡化、正文优先），标题折叠，查找替换，多光标，命令面板，文件名与全文搜索，预览，字号与字体可调，自动换行，行号，彩虹缩进线。
- **记录类型**：内置“日志”“阅读笔记”两种结构，规则是笔记本里的纯文本文件（`.jusnote/types/`）；写的时候当场检查格式，也可以一句话记到今天的某一栏。
- **中文 / 日本語** 界面，日间 / 夜间两种外观。
- **隐私**：完全离线，不联网、不上传任何东西。笔记本自己的记录放在它隐藏的 `.jusnote` 文件夹里；程序的设置放在 exe 旁边；日志只在内存里。

版本：**0.1.0**（第一个公开版本）。

## 下载与运行

1. 从 Releases 下载 `Jusnote-0.1.0-windows-x64.zip`，解压到任意文件夹。
2. 双击 `jusnote.exe`，选择一个文件夹作笔记本（可以是空文件夹，也可以是已有的 Markdown 文件夹）。之后从左上角的笔记本名字切换。

需要：

- Windows 10 / 11（64 位）。
- Microsoft Edge WebView2 运行时：Windows 11 已自带；Windows 10 若缺少，启动时会提示。

exe 没有代码签名，第一次运行时 Windows SmartScreen 可能提示“已保护你的电脑”，点“更多信息 → 仍要运行”即可。

## 保存与历史

| 什么时候 | 发生什么 |
|---|---|
| 停下打字不到一秒 | 存盘（原子写入，上一版留在 `.jusnote\backup`） |
| `Ctrl+S` | 存盘并提交这篇笔记 |
| 切换笔记、切换笔记本、关闭窗口 | 提交 Jusnote 写过、之后没被别处改过的笔记 |
| 重命名、删除 | 立即提交 |
| 别的程序改了文件 | 不自动提交；在“大纲”栏的“未提交”里列出，点“全部提交”接受 |

打开一个还没有 git 仓库的文件夹时，Jusnote 会在里面建一个（分支 `main`）。已有的仓库照常使用，作者身份沿用仓库里的设置；没有设置时用 `Jusnote <jusnote@localhost>`，不改动你的 git 配置。

## 快捷键

| 键 | 作用 |
|---|---|
| Ctrl+N | 新建笔记 |
| Ctrl+P / Ctrl+Shift+F | 搜索笔记名与内容 |
| Ctrl+F / Ctrl+H | 在当前笔记中查找 / 替换 |
| Ctrl+S | 保存并提交 |
| Ctrl+K | 命令面板 |
| Ctrl+O | 文件 |
| Ctrl+J | 大纲、改动与历史 |
| Ctrl+Shift+V | 预览 / 源码 |
| Ctrl+B / Ctrl+I / Ctrl+E | 粗体 / 斜体 / 行内代码 |
| Ctrl+单击 | 打开链接（`jus://note/…` 在 Jusnote 里打开，网页链接交给浏览器） |
| Ctrl + / Ctrl - / Ctrl 0 | 字号 |
| F1 | 全部快捷键 |
| Esc | 关闭面板 |

在文件列表里右键一篇笔记可以重命名、删除、复制 `jus://` 链接或在资源管理器中显示。

## 命令行

同一个 exe 也是命令行工具，人和 agent 用同一套操作；加 `-json` 输出机器可读的结果。

```text
jusnote                               打开编辑器（默认打开上次的笔记本）
jusnote gui -notebook D:\notes        打开指定的笔记本
jusnote list   -notebook D:\notes     列出笔记
jusnote read   <路径> -notebook …      输出一篇笔记
jusnote write  <路径> -text "…"        原子写入并提交这一篇（也可从管道输入；-no-commit 只写不提交）
jusnote history log | status          最近的提交 / 有改动的文件
jusnote info | init | version
```

## 数据放在哪里

| 什么 | 在哪里 |
|---|---|
| 笔记 | 你的笔记本文件夹里的 `.md` / `.markdown` 文件 |
| 历史 | 笔记本自己的 git 仓库（`.git`） |
| 记录类型的规则 | 笔记本里隐藏的 `.jusnote\types\`（随 git 同步，可以编辑、分享） |
| 上一版备份、上次打开的笔记与光标位置 | `.jusnote\backup\`、`.jusnote\cache\`（不进 git） |
| 最近的笔记本、界面偏好 | exe 旁边的 `jusnote-data\`（不可写时用 `%AppData%\jusnote`） |
| 日志 | 只在内存里；需要时在“设置 → 复制诊断日志” |

删除 `jusnote-data` 和笔记本里的 `.jusnote` 就会清除 Jusnote 留下的痕迹；笔记和 git 历史是你的，Jusnote 不会删除。

## 从源码构建

需要 Go 1.27+ 和 Node 22+。

```text
npm --prefix web ci
./build.ps1            # 生成 bin\jusnote.exe
go test ./...          # 测试不依赖任何外部程序
npm --prefix web test
./release.ps1          # 测试、构建并打包 dist\Jusnote-<版本>-windows-x64.zip
```

外观遵循 Jus 系列的统一设计语言（与 Jusplay 相同）。

## 许可证

Jusnote 以 MIT 许可证发布（[LICENSE](LICENSE)）。exe 内包含的第三方软件与字体（Go 运行时、go-git、go-webview2、CodeMirror、marked、DOMPurify 等，以及思源黑体的 SIL OFL 1.1 子集）的许可证见 [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt)。
