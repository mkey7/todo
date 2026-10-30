package notes

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"todo/internal/query"
)

const MaxDocument = 1024 * 1024

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path"`
}

func (e *Error) Error() string                 { return e.Path + ": " + e.Message }
func Invalid(code, path, message string) error { return &Error{code, message, path} }
func Strict(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected exactly one JSON object")
	}
	return nil
}

type Range struct {
	Mode   string `json:"mode"`
	Start  string `json:"start,omitempty"`
	End    string `json:"end_exclusive,omitempty"`
	Preset string `json:"preset,omitempty"`
}
type Tags struct {
	Mode   string  `json:"mode"`
	Values []int64 `json:"values,omitempty"`
}
type Context struct {
	Range  Range   `json:"range"`
	TagIDs []int64 `json:"tag_ids"`
}
type Params struct {
	Range          Range   `json:"range"`
	TagIDs         Tags    `json:"tag_ids"`
	DateField      string  `json:"date_field,omitempty"`
	TagRule        string  `json:"tag_rule,omitempty"`
	Status         string  `json:"status,omitempty"`
	Priority       *int    `json:"priority,omitempty"`
	TodoIDs        []int64 `json:"todo_ids,omitempty"`
	IncludeRunning bool    `json:"include_running,omitempty"`
	GroupBy        string  `json:"group_by,omitempty"`
}
type Binding struct {
	Field  string `json:"field"`
	Type   string `json:"type"`
	Source string `json:"source"`
}
type Search struct {
	Text   string   `json:"text"`
	Fields []string `json:"fields"`
}
type Definition struct {
	Kind         string              `json:"kind"`
	Search       *Search             `json:"search,omitempty"`
	OrderBy      []query.OrderItem   `json:"order_by,omitempty"`
	Name         string              `json:"name,omitempty"`
	Version      int                 `json:"version,omitempty"`
	Params       Params              `json:"params,omitempty"`
	Query        *query.QueryRequest `json:"query,omitempty"`
	Bindings     []Binding           `json:"bindings,omitempty"`
	OriginViewID *int64              `json:"origin_view_id,omitempty"`
}
type Mapping struct {
	ID    string `json:"id,omitempty"`
	Title string `json:"title,omitempty"`
	Date  string `json:"date,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}
type Options struct {
	Scale     string              `json:"scale,omitempty"`
	Columns   []string            `json:"columns"`
	Group     *query.DisplayGroup `json:"group,omitempty"`
	Summaries []query.Metric      `json:"summaries,omitempty"`
}
type View struct {
	Type    string  `json:"type"`
	Mapping Mapping `json:"mapping"`
	Options Options `json:"options"`
}
type Block struct {
	ID         string      `json:"id"`
	Type       string      `json:"type"`
	QueryMode  string      `json:"query_mode,omitempty"`
	Text       string      `json:"text,omitempty"`
	Level      int         `json:"level,omitempty"`
	Title      string      `json:"title,omitempty"`
	Definition *Definition `json:"definition,omitempty"`
	View       *View       `json:"view,omitempty"`
}
type Document struct {
	SchemaVersion int     `json:"schema_version"`
	Title         string  `json:"title"`
	Context       Context `json:"context"`
	Blocks        []Block `json:"blocks"`
}
type Note struct {
	ID        int64     `json:"id"`
	Revision  int64     `json:"revision"`
	Title     string    `json:"title"`
	Document  *Document `json:"document,omitempty"`
	CreatedAt string    `json:"created_at"`
	UpdatedAt string    `json:"updated_at"`
	DeletedAt *string   `json:"deleted_at"`
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidateView(v *View, path string) error {
	if v == nil {
		return nil
	}
	if !slices.Contains([]string{"table", "calendar", "timeline"}, v.Type) {
		return Invalid("invalid_document", path+".type", "unknown view")
	}
	if v.Options.Scale != "" && !slices.Contains([]string{"day", "week"}, v.Options.Scale) {
		return Invalid("invalid_document", path+".options.scale", "invalid scale")
	}
	if len(v.Options.Columns) > 20 {
		return Invalid("query_limit_exceeded", path+".options.columns", "maximum 20 columns")
	}
	if v.Options.Group != nil && v.Options.Group.Direction != "asc" && v.Options.Group.Direction != "desc" {
		return Invalid("invalid_document", path+".options.group.direction", "expected asc or desc")
	}
	if err := query.ValidateMetrics(v.Options.Summaries); err != nil {
		if qe, ok := err.(*query.Error); ok {
			qe.Path = path + strings.TrimPrefix(qe.Path, "view")
		}
		return err
	}
	return nil
}
func Validate(doc Document) error {
	fail := func(path, msg string) error { return Invalid("invalid_document", path, msg) }
	if doc.SchemaVersion != 1 && doc.SchemaVersion != 2 {
		return fail("document.schema_version", "unsupported document version")
	}
	if utf8.RuneCountInString(doc.Title) > 200 {
		return fail("document.title", "maximum 200 characters")
	}
	if len(doc.Blocks) > 200 {
		return fail("document.blocks", "maximum 200 blocks")
	}
	if len(doc.Context.TagIDs) > 100 {
		return fail("document.context.tag_ids", "maximum 100 tags")
	}
	if !slices.Contains([]string{"fixed", "relative", "none"}, doc.Context.Range.Mode) {
		return fail("document.context.range.mode", "invalid range mode")
	}
	if doc.Context.Range.Mode == "fixed" {
		r := doc.Context.Range
		start, e := time.Parse("2006-01-02", r.Start)
		end, ee := time.Parse("2006-01-02", r.End)
		if e != nil || ee != nil || !end.After(start) {
			return fail("document.context.range", "invalid fixed date range")
		}
	}
	if doc.Context.Range.Mode == "relative" && !slices.Contains([]string{"today", "this_week", "this_month"}, doc.Context.Range.Preset) {
		return fail("document.context.range.preset", "unknown relative range")
	}
	ids := map[int64]bool{}
	for _, id := range doc.Context.TagIDs {
		if id < 1 || ids[id] {
			return fail("document.context.tag_ids", "IDs must be positive and unique")
		}
		ids[id] = true
	}
	seen := map[string]bool{}
	queries := 0
	for i, b := range doc.Blocks {
		path := fmt.Sprintf("document.blocks[%d]", i)
		if !uuidPattern.MatchString(b.ID) || seen[strings.ToLower(b.ID)] {
			return fail(path+".id", "invalid or duplicate UUID")
		}
		seen[strings.ToLower(b.ID)] = true
		if !slices.Contains([]string{"paragraph", "heading", "bullet", "query"}, b.Type) {
			return fail(path+".type", "unknown block type")
		}
		if utf8.RuneCountInString(b.Text) > 20000 || utf8.RuneCountInString(b.Title) > 200 {
			return fail(path, "text or title too long")
		}
		if b.Type == "heading" && (b.Level < 1 || b.Level > 3) {
			return fail(path+".level", "heading level must be 1–3")
		}
		if b.Type != "query" && (b.Definition != nil || b.View != nil) {
			return fail(path, "text block cannot contain a query")
		}
		if b.Type == "query" {
			if doc.SchemaVersion == 1 && ((b.View != nil && (b.View.Options.Group != nil || len(b.View.Options.Summaries) > 0)) || (b.Definition != nil && (b.Definition.Search != nil || len(b.Definition.OrderBy) > 0))) {
				return fail(path, "new query features require schema_version 2")
			}
			queries++
			if doc.SchemaVersion == 2 && b.QueryMode != "detail" && b.QueryMode != "aggregate" {
				return fail(path+".query_mode", "select detail or aggregate")
			}
			if b.QueryMode != "" && b.QueryMode != "detail" && b.QueryMode != "aggregate" {
				return fail(path+".query_mode", "unknown query mode")
			}
			if err := ValidateView(b.View, path+".view"); err != nil {
				return err
			}
			if b.Definition != nil {
				if b.Definition.Search != nil && (utf8.RuneCountInString(b.Definition.Search.Text) > 200 || len(b.Definition.Search.Fields) > 20) {
					return fail(path+".definition.search", "search exceeds limits")
				}
				if len(b.Definition.OrderBy) > 20 {
					return fail(path+".definition.order_by", "maximum 20 sort fields")
				}
				if b.Definition.Kind != "function" && b.Definition.Kind != "custom" {
					return fail(path+".definition.kind", "unknown definition kind")
				}
				raw, _ := json.Marshal(b.Definition)
				if len(raw) > 65536 {
					return fail(path+".definition", "maximum 64 KiB")
				}
			}
		}
	}
	if queries > 30 {
		return fail("document.blocks", "maximum 30 query blocks")
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if len(raw) > MaxDocument {
		return Invalid("request_too_large", "document", "maximum 1 MiB")
	}
	return nil
}

// Mode supports old documents without reinterpreting SQL groups as display groups.
func Mode(d *Definition) string {
	if d != nil {
		if d.Kind == "function" && d.Name == "time_stats" {
			return "aggregate"
		}
		if d.Query != nil {
			if len(d.Query.GroupBy) > 0 {
				return "aggregate"
			}
			for _, s := range d.Query.Select {
				if s.Aggregate != "" {
					return "aggregate"
				}
			}
		}
	}
	return "detail"
}
func Upgrade(doc *Document) {
	for i := range doc.Blocks {
		b := &doc.Blocks[i]
		if b.Type == "query" && b.QueryMode == "" {
			b.QueryMode = Mode(b.Definition)
		}
	}
}
