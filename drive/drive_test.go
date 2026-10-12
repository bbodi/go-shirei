package drive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.hasen.dev/shirei"
)

func TestParseRecord(t *testing.T) {
	n, err := parseRecord(`id: #7
path: top_bar files_browser
rect: 10 20 80 30
role: button
focused: true
checked: true
value: hi
scroll: 0 12 max: 0 400
`)
	if err != nil {
		t.Fatal(err)
	}
	if n.ID != 7 {
		t.Fatalf("id=%d", n.ID)
	}
	if n.Path != "top_bar files_browser" || n.Name != "files_browser" {
		t.Fatalf("path=%q name=%q", n.Path, n.Name)
	}
	if n.Rect != (shirei.Rect{Origin: shirei.Vec2{10, 20}, Size: shirei.Vec2{80, 30}}) {
		t.Fatalf("rect=%+v", n.Rect)
	}
	if n.Role != "button" || !n.Focused || !n.Checked || n.Value != "hi" {
		t.Fatalf("fields=%+v", n)
	}
	if !n.ScrollPort || n.Scroll != (shirei.Vec2{0, 12}) || n.ScrollMax != (shirei.Vec2{0, 400}) {
		t.Fatalf("scroll=%+v", n)
	}

	dash, err := parseRecord("path: top_bar -\nrect: 1 2 3 4\nrole: button\n")
	if err != nil {
		t.Fatal(err)
	}
	if dash.Path != "top_bar -" || dash.Name != "" {
		t.Fatalf("unnamed: path=%q name=%q", dash.Path, dash.Name)
	}
}

func TestParseQueryReply(t *testing.T) {
	r, err := parseQueryReply("count: 40\n\npath: proc\nrect: 1 2 3 4\nvalue: 1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Count != 40 {
		t.Fatalf("Count=%d want 40 (full match total, not len(Nodes))", r.Count)
	}
	if len(r.Nodes) != 1 || r.Nodes[0].Value != "1" {
		t.Fatalf("Nodes=%+v", r.Nodes)
	}
	z, err := parseQueryReply("count: 0")
	if err != nil || z.Count != 0 || len(z.Nodes) != 0 {
		t.Fatalf("zero: %+v %v", z, err)
	}
}

func TestShotNoopWithoutEnv(t *testing.T) {
	if err := Shot(1, "x"); err != nil {
		t.Fatal(err)
	}
	Comment("ignored")
}

func TestCommentRecordsInTrace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvTrace, dir)
	beginTrace("TestCommentRecordsInTrace")
	defer endTrace()
	Comment("Open the filter text field")
	events, err := ReadTrace(filepath.Join(dir, "TestCommentRecordsInTrace", "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "comment" || events[0].Name != "Open the filter text field" {
		t.Fatalf("events=%+v", events)
	}
}

func TestTraceRecordsCmdAndShot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvTrace, dir)
	beginTrace("TestTraceRecordsCmdAndShot")
	defer endTrace()

	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	shirei.AcceptInputCommands(port)
	shirei.GetHost().WindowSize = shirei.Vec2{80, 40}
	shirei.GetHost().WindowScale = 1
	shirei.RunFrameFn(func() {
		shirei.Container(shirei.Attrs(shirei.FixSize(80, 40), shirei.Background(200, 80, 40, 1)), func() {})
	})

	if err := Ping(port); err != nil {
		t.Fatal(err)
	}
	if _, err := Count(port, ""); err != nil {
		t.Fatal(err)
	}
	if err := Shot(port, "hello world"); err != nil {
		t.Fatal(err)
	}

	events, err := ReadTrace(filepath.Join(dir, "TestTraceRecordsCmdAndShot", "trace.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == "cmd" && strings.HasPrefix(strings.ToLower(ev.Sent), "ping") {
			t.Fatalf("ping logged: %+v", ev)
		}
	}
	if len(events) != 3 || kinds[0] != "cmd" || kinds[1] != "cmd" || kinds[2] != "shot" {
		t.Fatalf("kinds=%v want [cmd cmd shot] (count, screenshot, beat)", kinds)
	}
	if events[0].Sent != "count" {
		t.Fatalf("cmd sent=%q", events[0].Sent)
	}
	if !strings.HasPrefix(events[1].Sent, "screenshot ") {
		t.Fatalf("screenshot cmd sent=%q", events[1].Sent)
	}
	if events[2].Name != "hello world" {
		t.Fatalf("shot name=%q", events[2].Name)
	}
	if _, err := os.Stat(events[2].Path); err != nil {
		t.Fatal(err)
	}
}

func TestPing(t *testing.T) {
	port, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	shirei.AcceptInputCommands(port)
	if err := Ping(port); err != nil {
		t.Fatal(err)
	}
}
