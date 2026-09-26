package recordtype

import (
	"os"
	"strings"
	"testing"
	"time"
)

func daily(t *testing.T) *Type {
	t.Helper()
	vault := t.TempDir()
	if err := EnsureDefaults(vault); err != nil {
		t.Fatal(err)
	}
	types, err := Load(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 2 {
		t.Fatalf("want 2 built-in types, got %d", len(types))
	}
	tp := Match(types, "logs/2026-01.md")
	if tp == nil || tp.ID != "daily-log" {
		t.Fatalf("match failed: %+v", tp)
	}
	return tp
}

func TestEnsureDefaultsIdempotent(t *testing.T) {
	vault := t.TempDir()
	if err := EnsureDefaults(vault); err != nil {
		t.Fatal(err)
	}
	// A user-edited file must not be overwritten.
	p := TypesDir(vault) + "/daily-log.yaml"
	if err := os.WriteFile(p, []byte("# mine\nid: daily-log\nname: 日志\nunit: '## {YYYY-MM-DD}'\nsections:\n  - {name: 身体, kind: free}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefaults(vault); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "# mine") {
		t.Fatalf("EnsureDefaults overwrote a custom file: %q", b)
	}
	// The other built-in is still added.
	if types, err := Load(vault); err != nil || len(types) != 2 {
		t.Fatalf("want 2 types, got %v (%v)", types, err)
	}
}

func TestAppendNewUnit(t *testing.T) {
	tp := daily(t)
	date := time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC)
	out, err := tp.Append(nil, date, "身体", "体重：69.5")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{"# 2026-01", "## 2026-01-04", "- 【身体】", "  - 体重：", "  - 体重：69.5", "- 【工作】"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestAppendPreservesOtherLines(t *testing.T) {
	tp := daily(t)
	date := time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC)
	before := "# 2026-01\n\n## 2026-01-03\n\n- 【身体】\n  - 体重：69.9\n  - 步数：9100\n- 【工作】\n  - 写测试\n"
	out, err := tp.Append([]byte(before), date, "工作", "继续写测试")
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "  - 写测试\n  - 继续写测试\n") {
		t.Fatalf("entry not appended after the last work line:\n%s", got)
	}
	// Every original line survives, in order.
	for _, l := range strings.Split(strings.TrimSuffix(before, "\n"), "\n") {
		if !strings.Contains(got, l) {
			t.Errorf("lost line %q", l)
		}
	}
}

func TestCheck(t *testing.T) {
	tp := daily(t)
	data := "# 2026-01\n\n## 2026-01-01\n\n" +
		"- 【身体】\n  - 体重：七十\n  - 血压：120\n" + // bad number, unknown field
		"- 【收支】\n  - 午饭 花了25块。\n" // sentence, no sign
	out := tp.Check([]byte(data))
	if len(out) != 3 {
		t.Fatalf("want 3 diagnostics, got %d: %+v", len(out), out)
	}
	if out[0].Line != 6 || out[1].Line != 7 || out[2].Line != 9 {
		t.Fatalf("wrong lines: %+v", out)
	}
}

func TestCheckClean(t *testing.T) {
	tp := daily(t)
	data := "# 2026-01\n\n## 2026-01-01\n\n- 【身体】\n  - 体重：70.0\n  - 步数：8000\n- 【收支】\n  - 饮食 午饭 -950\n  - —\n- 【生活】\n  - 随意的一句话。\n"
	if out := tp.Check([]byte(data)); len(out) != 0 {
		t.Fatalf("clean file reported: %+v", out)
	}
}
