# 测试笔记本（合成数据）

这个文件夹是 Jusnote 的测试夹具：里面的笔记全部是**编造的示例**，不对应任何真实人物、事件或金额，只用来做单元测试和手动运行。

它覆盖三类内容：

- `logs/2026-01.md` —— "日志"记录类型（`# 年月` + `## 日期` + 五栏）。
- `notes/reading-example.md` —— "阅读笔记"记录类型（定位/方法/证据/评价）。
- `notes/ideas.md` —— 普通自由 Markdown。

打开方式：

```text
bin\jusnote.exe -notebook testdata\notebook
```

程序会在首次打开时为它建立一个 git 仓库（自动提交也发生在这个仓库里）。
