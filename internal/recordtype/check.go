package recordtype

import (
	"fmt"
	"regexp"
	"strings"
)

// Diagnostic is one problem found in a note, at a 1-based line.
type Diagnostic struct {
	Line     int    `json:"line"`
	Message  string `json:"message"`
	Severity string `json:"severity"` // "warning" (the checker never blocks a save)
}

var (
	reNumber    = regexp.MustCompile(`^\d+(?:\.\d+)?$`)
	reInteger   = regexp.MustCompile(`^\d+$`)
	reMoneyTail = regexp.MustCompile(`^[+\-]\d+(?:\.\d+)?[A-Za-z]{0,3}$`)
	reItemLine  = regexp.MustCompile(`^-\s+(.*)$`)
)

// Check validates a note against the type and returns problems with their
// lines. It never reports errors that would prevent saving.
func (t *Type) Check(data []byte) []Diagnostic {
	lines, _ := splitLines(string(data))
	var out []Diagnostic
	var sec *Section
	inRecord := t.Unit == ""

	for i, raw := range lines {
		n := i + 1
		s := trim(raw)
		if s == "" {
			continue
		}
		if t.Unit != "" && t.unitRe().MatchString(s) {
			sec = nil
			inRecord = true
			continue
		}
		if name, ok := t.headerName(raw); ok {
			sec = t.Section(name)
			if sec == nil {
				out = append(out, Diagnostic{n, fmt.Sprintf("未知栏目「%s」", name), "warning"})
			}
			continue
		}
		m := reItemLine.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		if sec == nil {
			if t.Unit != "" && inRecord {
				out = append(out, Diagnostic{n, "这一行不在任何栏目下", "warning"})
			}
			continue
		}
		out = append(out, checkItem(*sec, n, strings.TrimSpace(m[1]))...)
	}
	return out
}

func checkItem(s Section, n int, item string) []Diagnostic {
	switch s.Kind {
	case KindFields:
		return checkField(s, n, item)
	case KindMoney:
		return checkMoney(n, item)
	default:
		return nil
	}
}

func checkField(s Section, n int, item string) []Diagnostic {
	if item == "—" || item == "-" {
		return []Diagnostic{{n, fmt.Sprintf("「%s」栏应写 键：值", s.Name), "warning"}}
	}
	key, val, ok := splitKV(item)
	if !ok {
		return []Diagnostic{{n, fmt.Sprintf("「%s」栏应写 键：值", s.Name), "warning"}}
	}
	f := fieldByKey(s, key)
	if f == nil {
		return []Diagnostic{{n, fmt.Sprintf("「%s」栏没有「%s」字段", s.Name, key), "warning"}}
	}
	if val != "" && (f.Type == "number" || f.Type == "integer") {
		if !numberOK(val, f.Type) {
			return []Diagnostic{{n, fmt.Sprintf("「%s」应为%s", key, numberWord(f.Type)), "warning"}}
		}
	}
	if hasSentencePunct(val) {
		return []Diagnostic{{n, "结构化栏目不写整句", "warning"}}
	}
	return nil
}

func checkMoney(n int, item string) []Diagnostic {
	if item == "—" || item == "-" {
		return nil
	}
	if hasSentencePunct(item) {
		return []Diagnostic{{n, "收支不写整句", "warning"}}
	}
	fields := strings.Fields(item)
	if len(fields) < 2 {
		return []Diagnostic{{n, "收支应为 分类 [说明] ±金额[货币]", "warning"}}
	}
	if !reMoneyTail.MatchString(fields[len(fields)-1]) {
		return []Diagnostic{{n, "收支应以 ±金额[货币] 结尾，如「饮食 午饭 -950」", "warning"}}
	}
	if strings.HasPrefix(fields[0], "+") || strings.HasPrefix(fields[0], "-") {
		return []Diagnostic{{n, "收支缺少分类", "warning"}}
	}
	return nil
}

func splitKV(s string) (key, val string, ok bool) {
	if i := strings.Index(s, "："); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len("："):]), true
	}
	if i := strings.Index(s, ":"); i >= 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
	}
	return "", "", false
}

func fieldByKey(s Section, key string) *Field {
	for i := range s.Fields {
		if s.Fields[i].Key == key {
			return &s.Fields[i]
		}
	}
	return nil
}

func numberOK(v, typ string) bool {
	if typ == "integer" {
		return reInteger.MatchString(v)
	}
	return reNumber.MatchString(v)
}

func numberWord(typ string) string {
	if typ == "integer" {
		return "整数"
	}
	return "数字"
}

func hasSentencePunct(s string) bool {
	return strings.ContainsAny(s, "。！？!?")
}
