package recordtype

// Built-in record types, written into a notebook's .jusnote/types folder
// the first time it has none, so the notebook is self-describing and can be
// copied or shared with its rules.

type builtin struct {
	name string
	yaml string
}

var builtins = []builtin{
	{"daily-log.yaml", dailyLogYAML},
	{"reading-note.yaml", readingNoteYAML},
}

const dailyLogYAML = `# 日志：按天记录，五栏。身体/饮食/收支只放结构化内容，生活/工作自由写。
id: daily-log
name: 日志
style: list
file: "{YYYY-MM}.md"
title: "# {YYYY-MM}"
unit: "## {YYYY-MM-DD}"
sections:
  - name: 身体
    kind: fields
    fields:
      - {key: 体重, type: number}
      - {key: 步数, type: integer}
  - name: 饮食
    kind: fields
    fields:
      - {key: 早餐}
      - {key: 午餐}
      - {key: 晚餐}
  - name: 收支
    kind: money
  - name: 生活
    kind: free
  - name: 工作
    kind: free
`

const readingNoteYAML = `# 阅读笔记：四部分，用 ## 小节。内容为自由文字。
id: reading-note
name: 阅读笔记
style: heading
sections:
  - {name: 定位, kind: free}
  - {name: 方法, kind: free}
  - {name: 证据, kind: free}
  - {name: 评价, kind: free}
`
