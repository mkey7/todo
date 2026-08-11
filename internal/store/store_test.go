package store

import (
	"database/sql"
	"testing"

	"todo/internal/db"
	"todo/internal/models"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return d
}

func TestGroupCRUD(t *testing.T) {
	d := newTestDB(t)
	g, err := CreateGroup(d, models.Group{Name: "开发", Color: "#6366f1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g.ID == 0 || g.Name != "开发" {
		t.Errorf("unexpected group: %+v", g)
	}
	gs, _ := ListGroups(d)
	if len(gs) != 1 {
		t.Errorf("list len = %d, want 1", len(gs))
	}
	g, _ = UpdateGroup(d, g.ID, models.Group{Name: "开发组", Color: "#22c55e"})
	if g.Name != "开发组" {
		t.Errorf("update name = %s", g.Name)
	}
	if err := DeleteGroup(d, g.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	gs, _ = ListGroups(d)
	if len(gs) != 0 {
		t.Errorf("after delete len = %d", len(gs))
	}
}

func TestTodoSubtasksAndStatus(t *testing.T) {
	d := newTestDB(t)
	parent, _ := CreateTodo(d, models.Todo{Title: "父任务"})
	child, _ := CreateTodo(d, models.Todo{Title: "子任务", ParentID: &parent.ID})
	if child.ParentID == nil || *child.ParentID != parent.ID {
		t.Fatalf("child parent id = %+v", child.ParentID)
	}

	// mark child done, completed_at should be set
	updated, err := SetTodoStatus(d, child.ID, "done")
	if err != nil {
		t.Fatalf("set status: %v", err)
	}
	if updated.Status != "done" || updated.CompletedAt == nil {
		t.Errorf("done state wrong: %+v", updated)
	}

	// list with children
	top, _ := ListTodos(d, nil, "")
	top, _ = WithChildren(d, top)
	if len(top) != 1 || len(top[0].Children) != 1 {
		t.Errorf("tree wrong: %+v", top)
	}

	// deleting parent cascades children
	DeleteTodo(d, parent.ID)
	top, _ = ListTodos(d, nil, "")
	if len(top) != 0 {
		t.Errorf("cascade delete failed: %+v", top)
	}
}

func TestSubtaskInheritsAndExtendsParentTags(t *testing.T) {
	d := newTestDB(t)
	work, _ := CreateGroup(d, models.Group{Name: "工作", Color: "#6366f1"})
	urgent, _ := CreateGroup(d, models.Group{Name: "紧急", Color: "#ef4444"})
	parent, err := CreateTodo(d, models.Todo{Title: "父任务", TagIDs: []int64{work.ID}})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}
	child, err := CreateTodo(d, models.Todo{Title: "子任务", ParentID: &parent.ID, TagIDs: []int64{urgent.ID}})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if len(child.TagIDs) != 2 || child.TagIDs[0] != work.ID || child.TagIDs[1] != urgent.ID {
		t.Fatalf("child tags = %v, want inherited and additional tags", child.TagIDs)
	}
}

func TestTodoTagOrderSetsPrimaryTag(t *testing.T) {
	d := newTestDB(t)
	first, _ := CreateGroup(d, models.Group{Name: "第一", Color: "#6366f1"})
	second, _ := CreateGroup(d, models.Group{Name: "第二", Color: "#ef4444"})
	todo, err := CreateTodo(d, models.Todo{Title: "排序", TagIDs: []int64{first.ID, second.ID}})
	if err != nil {
		t.Fatalf("create todo: %v", err)
	}
	updated, err := UpdateTodo(d, todo.ID, models.Todo{Title: todo.Title, Status: todo.Status, TagIDs: []int64{second.ID, first.ID}})
	if err != nil {
		t.Fatalf("update todo: %v", err)
	}
	if len(updated.TagIDs) != 2 || updated.TagIDs[0] != second.ID {
		t.Errorf("tag order / primary group = %+v", updated)
	}
}

func TestCompletedTodoGetsAutomaticCompletedTag(t *testing.T) {
	d := newTestDB(t)
	work, _ := CreateGroup(d, models.Group{Name: "工作", Color: "#6366f1"})
	todo, err := CreateTodo(d, models.Todo{Title: "完成标签", TagIDs: []int64{work.ID}})
	if err != nil {
		t.Fatalf("create todo: %v", err)
	}
	done, err := SetTodoStatus(d, todo.ID, "done")
	if err != nil {
		t.Fatalf("complete todo: %v", err)
	}
	if len(done.Tags) != 2 || done.Tags[1].Name != completedTagName || done.TagIDs[0] != work.ID {
		t.Errorf("completed tags = %+v, want work then completed", done.Tags)
	}
	pending, err := SetTodoStatus(d, todo.ID, "pending")
	if err != nil {
		t.Fatalf("reopen todo: %v", err)
	}
	if len(pending.Tags) != 1 || pending.Tags[0].Name != "工作" {
		t.Errorf("reopened tags = %+v, want only work", pending.Tags)
	}
}

func TestTimeEntryStartStop(t *testing.T) {
	d := newTestDB(t)
	g, _ := CreateGroup(d, models.Group{Name: "开发", Color: "#6366f1"})
	todo, _ := CreateTodo(d, models.Todo{Title: "任务", TagIDs: []int64{g.ID}})

	// start with a todo -> group inherited
	e, err := StartEntry(d, &todo.ID, nil, "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if e.EndTime != nil {
		t.Errorf("new entry should be open")
	}
	if e.TagID == nil || *e.TagID != g.ID {
		t.Errorf("tag not inherited: %+v", e.TagID)
	}

	// active entry exists
	active, _ := ActiveEntry(d)
	if active == nil || active.ID != e.ID {
		t.Errorf("active entry wrong: %+v", active)
	}

	// starting again should stop the first (only one active)
	e2, _ := StartEntry(d, nil, &g.ID, "second")
	active, _ = ActiveEntry(d)
	if active.ID != e2.ID {
		t.Errorf("expected second active, got %+v", active)
	}
	// first entry should now be closed
	first, _ := GetEntry(d, e.ID)
	if first.EndTime == nil {
		t.Errorf("first entry should be stopped")
	}

	// stop the active one
	stopped, err := StopEntry(d)
	if err != nil || stopped.ID == 0 {
		t.Fatalf("stop: %v %+v", err, stopped)
	}
	if stopped.EndTime == nil {
		t.Errorf("stopped entry needs end_time")
	}
	// stopping again with nothing active -> zero value, no error
	again, err := StopEntry(d)
	if err != nil || again.ID != 0 {
		t.Errorf("double stop should be no-op: %+v %v", again, err)
	}
}

func TestListEntriesForDayOverlap(t *testing.T) {
	d := newTestDB(t)
	g, _ := CreateGroup(d, models.Group{Name: "开发", Color: "#6366f1"})
	// entry fully within the day
	CreateEntry(d, models.TimeEntry{TagID: &g.ID, StartTime: "2026-07-14 09:00:00", EndTime: strPtr("2026-07-14 10:00:00")})
	// entry that ends exactly at day start should NOT appear (end > dayStart is required, 00:00:00 is not > 00:00:00)
	CreateEntry(d, models.TimeEntry{TagID: &g.ID, StartTime: "2026-07-13 22:00:00", EndTime: strPtr("2026-07-14 00:00:00")})
	// entry starting just before midnight and ending mid-day should appear (overlap)
	CreateEntry(d, models.TimeEntry{TagID: &g.ID, StartTime: "2026-07-14 23:30:00", EndTime: strPtr("2026-07-15 01:00:00")})

	es, err := ListEntriesForDay(d, "2026-07-14")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// expect the 09:00-10:00 entry and the 23:30-01:00 entry; NOT the one ending at 00:00:00
	if len(es) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(es), es)
	}
}

func strPtr(s string) *string { return &s }
