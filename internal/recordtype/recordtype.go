// Package recordtype is the layer above storage that makes Jusnote "know
// the shape of a record": a plain-text template says which file a kind of
// note lives in, what a record unit looks like, which sections it has, and
// what a valid entry in each section is. The daily log is one type; a
// reading note is another. Types are user-editable and shareable, and the
// same rules drive the in-app checker and (later) the agent interface.
package recordtype

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Kind is what a section holds.
type Kind string

const (
	KindFields Kind = "fields" // fixed keys with short structured values
	KindMoney  Kind = "money"  // "分类 [说明] ±金额[货币单位]" lines
	KindFree   Kind = "free"   // free lines
)

// Style is how sections are written.
type Style string

const (
	StyleList    Style = "list"    // "- 【name】" (log style)
	StyleHeading Style = "heading" // "## name" (document style)
)

// Field is one key inside a "fields" section.
type Field struct {
	Key  string `yaml:"key" json:"key"`
	Type string `yaml:"type" json:"type"` // "number", "integer" or "" (text)
}

// Section is one part of a record.
type Section struct {
	Name   string  `yaml:"name" json:"name"`
	Kind   Kind    `yaml:"kind" json:"kind"`
	Fields []Field `yaml:"fields" json:"fields,omitempty"`
}

// Type is one kind of record.
type Type struct {
	ID       string    `yaml:"id" json:"id"`
	Name     string    `yaml:"name" json:"name"`
	Style    Style     `yaml:"style" json:"style"`
	File     string    `yaml:"file" json:"file,omitempty"`   // e.g. "{YYYY-MM}.md"; empty for user-named files
	Title    string    `yaml:"title" json:"title,omitempty"` // e.g. "# {YYYY-MM}"
	Unit     string    `yaml:"unit" json:"unit,omitempty"`   // e.g. "## {YYYY-MM-DD}"; empty for document types
	Sections []Section `yaml:"sections" json:"sections"`
}

// TypesDir is where a notebook keeps its record types.
func TypesDir(vault string) string { return filepath.Join(vault, "types") }

// Load reads every *.yaml/*.yml in the notebook's types folder.
func Load(vault string) ([]*Type, error) {
	dir := TypesDir(vault)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Type
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".yaml", ".yml":
		default:
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		var t Type
		if err := yaml.Unmarshal(b, &t); err != nil {
			return nil, fmt.Errorf("recordtype: %s: %w", e.Name(), err)
		}
		if t.ID == "" {
			t.ID = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		}
		t.normalize()
		if err := t.validate(); err != nil {
			return nil, fmt.Errorf("recordtype: %s: %w", e.Name(), err)
		}
		out = append(out, &t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// EnsureDefaults writes each built-in type that the notebook does not
// already have, so a customized type is never overwritten and a notebook
// still gets the rest.
func EnsureDefaults(vault string) error {
	dir := TypesDir(vault)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, b := range builtins {
		p := filepath.Join(dir, b.name)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, []byte(b.yaml), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (t *Type) normalize() {
	if t.Style == "" {
		t.Style = StyleList
	}
	for i := range t.Sections {
		if t.Sections[i].Kind == "" {
			t.Sections[i].Kind = KindFree
		}
	}
}

func (t *Type) validate() error {
	if t.Name == "" {
		return fmt.Errorf("missing name")
	}
	if len(t.Sections) == 0 {
		return fmt.Errorf("no sections")
	}
	for _, s := range t.Sections {
		if s.Name == "" {
			return fmt.Errorf("a section has no name")
		}
		if s.Kind == KindFields && len(s.Fields) == 0 {
			return fmt.Errorf("section %q is fields but has no fields", s.Name)
		}
	}
	return nil
}

// Section returns the named section, or nil.
func (t *Type) Section(name string) *Section {
	for i := range t.Sections {
		if t.Sections[i].Name == name {
			return &t.Sections[i]
		}
	}
	return nil
}

// Match returns the first type whose file pattern matches rel's base name.
func Match(types []*Type, rel string) *Type {
	base := filepath.Base(filepath.FromSlash(rel))
	for _, t := range types {
		if t.File == "" {
			continue
		}
		if patternRegex(t.File).MatchString(base) {
			return t
		}
	}
	return nil
}

// FileName is the file this type uses for the date (empty when the type
// does not name its files).
func (t *Type) FileName(date time.Time) string { return t.expand(t.File, date) }

// TitleLine and UnitLine are the expanded title / record heading.
func (t *Type) TitleLine(date time.Time) string { return t.expand(t.Title, date) }
func (t *Type) UnitLine(date time.Time) string  { return t.expand(t.Unit, date) }

func (t *Type) expand(p string, d time.Time) string {
	return strings.NewReplacer(
		"{YYYY-MM-DD}", d.Format("2006-01-02"),
		"{YYYY-MM}", d.Format("2006-01"),
		"{YYYY}", d.Format("2006"),
		"{MM}", d.Format("01"),
		"{DD}", d.Format("02"),
	).Replace(p)
}

func patternRegex(p string) *regexp.Regexp {
	esc := regexp.QuoteMeta(p)
	esc = strings.NewReplacer(
		`\{YYYY-MM-DD\}`, `\d{4}-\d{2}-\d{2}`,
		`\{YYYY-MM\}`, `\d{4}-\d{2}`,
		`\{YYYY\}`, `\d{4}`,
		`\{MM\}`, `\d{2}`,
		`\{DD\}`, `\d{2}`,
	).Replace(esc)
	return regexp.MustCompile("^" + esc + "$")
}

func (t *Type) unitRe() *regexp.Regexp { return patternRegex(t.Unit) }
func (t *Type) titleRe() *regexp.Regexp {
	if t.Title == "" {
		return nil
	}
	return patternRegex(t.Title)
}

// sectionHeaderLine renders a section heading in the type's style.
func (t *Type) sectionHeaderLine(name string) string {
	if t.Style == StyleHeading {
		return "## " + name
	}
	return "- 【" + name + "】"
}

// sectionSkeleton is a fresh, empty section.
func (t *Type) sectionSkeleton(s Section) []string {
	out := []string{t.sectionHeaderLine(s.Name)}
	switch s.Kind {
	case KindFields:
		for _, f := range s.Fields {
			out = append(out, "  - "+f.Key+"：")
		}
	default:
		out = append(out, "  - —")
	}
	return out
}

// unitSkeleton is a fresh record unit with every section.
func (t *Type) unitSkeleton() []string {
	var out []string
	for _, s := range t.Sections {
		out = append(out, t.sectionSkeleton(s)...)
	}
	return out
}

// Append adds one entry under section in the record for date and returns
// the new file bytes. Everything else is preserved byte for byte. It needs
// a unit and list style (capture is for record units).
func (t *Type) Append(data []byte, date time.Time, section, text string) ([]byte, error) {
	if t.Unit == "" || t.Style == StyleHeading {
		return nil, fmt.Errorf("recordtype %s: this type does not take quick entries", t.ID)
	}
	sec := t.Section(section)
	if sec == nil {
		return nil, fmt.Errorf("recordtype %s: unknown section %q", t.ID, section)
	}
	lines, _ := splitLines(string(data))

	if t.Title != "" {
		tl := t.TitleLine(date)
		if len(lines) == 0 || trim(lines[0]) != trim(tl) {
			lines = append([]string{tl, ""}, lines...)
		}
	}

	ul := t.UnitLine(date)
	if indexOf(lines, ul) < 0 {
		lines = trimTrailingBlank(lines)
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, ul, "")
		lines = append(lines, t.unitSkeleton()...)
	}
	ui := indexOf(lines, ul)
	unitEnd := t.nextUnit(lines, ui+1)

	si := t.findSection(lines, ui+1, unitEnd, section)
	if si < 0 {
		block := t.sectionSkeleton(*sec)
		at := lastNonBlankEnd(lines, ui+1, unitEnd)
		lines = insertAt(lines, at, block)
		unitEnd += len(block)
		si = t.findSection(lines, ui+1, unitEnd, section)
	}
	if si < 0 {
		return nil, fmt.Errorf("recordtype %s: section %q not found after insert", t.ID, section)
	}
	se := unitEnd
	for i := si + 1; i < unitEnd; i++ {
		if _, ok := t.headerName(lines[i]); ok {
			se = i
			break
		}
	}
	at := lastNonBlankEnd(lines, si+1, se)
	lines = insertAt(lines, at, []string{"  - " + text})
	return []byte(joinLines(lines)), nil
}

func (t *Type) nextUnit(lines []string, from int) int {
	re := t.unitRe()
	for i := from; i < len(lines); i++ {
		if re.MatchString(trim(lines[i])) {
			return i
		}
	}
	return len(lines)
}

func (t *Type) findSection(lines []string, from, to int, name string) int {
	for i := from; i < to; i++ {
		if n, ok := t.headerName(lines[i]); ok && n == name {
			return i
		}
	}
	return -1
}

// headerName reports whether the line is a section heading of this type's
// style, and returns the section name.
func (t *Type) headerName(line string) (string, bool) {
	s := trim(line)
	if t.Style == StyleHeading {
		if m := reHeading.FindStringSubmatch(s); m != nil {
			return m[1], true
		}
		return "", false
	}
	if m := reListHeader.FindStringSubmatch(s); m != nil {
		return m[1], true
	}
	return "", false
}

var (
	reListHeader = regexp.MustCompile(`^- 【(.+?)】\s*$`)
	reHeading    = regexp.MustCompile(`^##\s+(.+?)\s*$`)
	reItem       = regexp.MustCompile(`^-\s+(.*)$`)
)

func splitLines(s string) ([]string, bool) {
	if s == "" {
		return nil, false
	}
	trailing := strings.HasSuffix(s, "\n")
	if trailing {
		s = s[:len(s)-1]
	}
	return strings.Split(s, "\n"), trailing
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

func trim(s string) string { return strings.TrimSpace(strings.TrimSuffix(s, "\r")) }

func indexOf(lines []string, target string) int {
	want := trim(target)
	for i, l := range lines {
		if trim(l) == want {
			return i
		}
	}
	return -1
}

func trimTrailingBlank(lines []string) []string {
	for len(lines) > 0 && trim(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// lastNonBlankEnd is the index after the last non-blank line in [from,to).
func lastNonBlankEnd(lines []string, from, to int) int {
	end := from
	for i := from; i < to; i++ {
		if trim(lines[i]) != "" {
			end = i + 1
		}
	}
	return end
}

func insertAt(lines []string, at int, block []string) []string {
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	out = append(out, lines[at:]...)
	return out
}
