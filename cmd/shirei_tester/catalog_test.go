package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindScanRootFromModule(t *testing.T) {
	// cwd for this package is …/shirei/cmd/shirei_tester; module root is two up.
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	root, err := findScanRoot(here)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		if _, err2 := os.Stat(filepath.Join(root, "go.work")); err2 != nil {
			t.Fatalf("scan root %s has neither go.mod nor go.work", root)
		}
	}
}

func TestPkgHasSnapshotMarker(t *testing.T) {
	here, _ := filepath.Abs(".")
	// …/shirei/cmd/shirei_tester → …/shirei/widgets
	shireiRoot := filepath.Dir(filepath.Dir(here))
	widgets := filepath.Join(shireiRoot, "widgets")
	if !pkgHasSnapshotMarker(widgets) {
		t.Fatalf("expected widgets to match snapshot markers")
	}

	// Negative: temp package with ordinary tests only (avoid matching this file's text).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte("package foo\nfunc TestPlain(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pkgHasSnapshotMarker(dir) {
		t.Fatalf("plain TestPlain package should not match")
	}
	if err := os.WriteFile(filepath.Join(dir, "snap_test.go"), []byte("package foo\nfunc TestSn"+"apshotDemo(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !pkgHasSnapshotMarker(dir) {
		t.Fatalf("expected marker after adding TestSnapshot* test")
	}
}

func TestPkgHasDriveMarker(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "plain_test.go"), []byte("package foo\nfunc TestPlain(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pkgHasDriveMarker(dir) {
		t.Fatal("plain package should not match drive marker")
	}
	src := "package foo\n\nfunc TestDriveFoo(t *testing.T) {\n\tdrive.Start(t, \".\")\n}\nfunc TestUnit(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(dir, "drive_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if !pkgHasDriveMarker(dir) {
		t.Fatal("expected drive.Start marker")
	}
	names := driveTestNames(dir)
	if !names["TestDriveFoo"] || !names["TestUnit"] {
		t.Fatalf("driveTestNames=%v", names)
	}
}

func TestListTestsFromSource(t *testing.T) {
	dir := t.TempDir()
	src := `package foo

import "testing"

func TestMain(m *testing.M) { m.Run() }

func TestAlpha(t *testing.T) {}
func TestBeta(t *testing.T) {}

type T struct{}
func (T) TestMethod(t *testing.T) {} // not a package-level test
`
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	names, err := listTests(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "TestAlpha" || names[1] != "TestBeta" {
		t.Fatalf("listTests = %v, want [TestAlpha TestBeta]", names)
	}
}

func TestDiscoverPackagesWalksFilesystem(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/scan\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested package with marker + tests.
	sub := filepath.Join(root, "widgets")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package widgets\n\nimport \"testing\"\n\nfunc TestSn" + "apshotX(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(sub, "w_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Nested go.mod is its own module and must still be discovered
	// (examples/ferry, examples/see_pprof).
	nested := filepath.Join(root, "other")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "go.mod"), []byte("module example.com/other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "o_test.go"),
		[]byte("package other\n\nimport \"testing\"\n\nfunc TestSn"+"apshotY(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// testdata / vendor ignored.
	for _, skip := range []string{"testdata", "vendor"} {
		d := filepath.Join(root, skip, "hidden")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "h_test.go"),
			[]byte("package hidden\n\nimport \"testing\"\n\nfunc TestSn"+"apshotZ(t *testing.T) {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pkgs, err := discoverPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("pkgs = %d, want 2 (widgets + nested other)", len(pkgs))
	}
	byRel := map[string]*PackageItem{}
	for _, p := range pkgs {
		byRel[p.Rel] = p
	}
	w := byRel["widgets"]
	if w == nil {
		t.Fatalf("missing widgets in %d pkgs", len(pkgs))
	}
	if w.ImportPath != "example.com/scan/widgets" {
		t.Fatalf("widgets ImportPath = %q", w.ImportPath)
	}
	if len(w.Tests) != 1 || w.Tests[0].Name != "TestSnapshotX" {
		t.Fatalf("widgets tests = %+v", w.Tests)
	}
	o := byRel["other"]
	if o == nil {
		t.Fatalf("missing nested module other")
	}
	if o.ImportPath != "example.com/other" {
		t.Fatalf("other ImportPath = %q (must come from nested go.mod)", o.ImportPath)
	}
	if len(o.Tests) != 1 || o.Tests[0].Name != "TestSnapshotY" {
		t.Fatalf("other tests = %+v", o.Tests)
	}
}

func TestDiscoverDriveOnlyPackage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/drv\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "drive_test.go"), []byte(
		"package drv\n\nfunc TestDriveFoo(t *testing.T) { drive.Start(t, \".\") }\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "unit_test.go"), []byte(
		"package drv\n\nfunc TestUnit(t *testing.T) {}\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	pkgs, err := discoverPackages(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("pkgs=%d want 1", len(pkgs))
	}
	p := pkgs[0]
	if !p.DriveOnly {
		t.Fatal("expected DriveOnly")
	}
	if len(p.Tests) != 1 || p.Tests[0].Name != "TestDriveFoo" || !p.Tests[0].Drive {
		t.Fatalf("tests=%+v", p.Tests)
	}
}

func TestImportPathFor(t *testing.T) {
	if got := importPathFor("m.com/x", "."); got != "m.com/x" {
		t.Fatalf("root: %q", got)
	}
	if got := importPathFor("m.com/x", "widgets"); got != "m.com/x/widgets" {
		t.Fatalf("sub: %q", got)
	}
}

func TestFindMatchLocsPackageAndTest(t *testing.T) {
	s := &AppState{
		Packages: []*PackageItem{
			{
				Dir: "/mod/examples/fontviewer", Rel: "examples/fontviewer",
				ImportPath: "mod/examples/fontviewer",
				Tests: []*TestItem{
					{Name: "TestSnapshotGallery"},
					{Name: "TestSnapshotLarge"},
				},
			},
			{
				Dir: "/mod/widgets", Rel: "widgets", ImportPath: "mod/widgets",
				Tests: []*TestItem{
					{Name: "TestButton"},
					{Name: "TestSnapshotMenu"},
				},
			},
		},
	}
	s.findQuery = "fontviewer"
	got := s.findMatchLocs()
	// Package path match → package header hit only (not every child test).
	if len(got) != 1 || !got[0].isPkg() || got[0].p != 0 {
		t.Fatalf("fontviewer package match = %v, want one package hit for pkg 0", got)
	}
	s.findQuery = "SnapshotMenu"
	got = s.findMatchLocs()
	if len(got) != 1 || got[0].isPkg() || got[0].p != 1 || got[0].t != 1 {
		t.Fatalf("test name match = %v, want widgets TestSnapshotMenu", got)
	}
	s.findQuery = "Snapshot"
	got = s.findMatchLocs()
	// Three TestSnapshot* tests by name (package headers do not match "Snapshot").
	if len(got) != 3 {
		t.Fatalf("Snapshot matches = %d, want 3", len(got))
	}
}
