package main

import (
	"fmt"
	"strings"
	"testing"

	"go.hasen.dev/shirei/drive"
)

func startThemeDrive(t *testing.T) int {
	t.Helper()
	return drive.Start(t, ".")
}

func mustCount(t *testing.T, port int, q string, want int) {
	t.Helper()
	if err := drive.WaitCount(port, q, want); err != nil {
		t.Fatal(err)
	}
}

func focusedName(t *testing.T, port int) string {
	t.Helper()
	ids, err := drive.Focused(port)
	if err != nil {
		t.Fatalf("focused: %v", err)
	}
	for _, id := range ids {
		n, err := drive.Show(port, fmt.Sprintf("#%d", id))
		if err != nil {
			continue
		}
		if n.Name != "" {
			return n.Name
		}
		toks := strings.Fields(n.Path)
		if len(toks) > 0 && toks[len(toks)-1] != "-" {
			return toks[len(toks)-1]
		}
	}
	t.Fatal("focused: no assigned name on the focus chain")
	return ""
}

func TestDriveTabCycle(t *testing.T) {
	port := startThemeDrive(t)
	drive.Comment("Tab through the toolbar color buttons")

	row := []string{
		"btn_lightsteel", "btn_slateblue", "btn_blue",
		"btn_meadow", "btn_sunshine", "btn_plastic",
	}
	for _, name := range row {
		if err := drive.TabUntil(port, name); err != nil {
			t.Fatal(err)
		}
	}

	drive.Comment("Skip past disabled buttons to Data")
	if err := drive.TabUntil(port, "btn_data"); err != nil {
		t.Fatal(err)
	}
	got := focusedName(t, port)
	if got == "btn_disabled" || got == "btn_disabled_blue" || got == "btn_disabled_meadow" {
		t.Fatalf("Tab landed on disabled %q", got)
	}
	if err := drive.Shot(port, "Toolbar after tabbing"); err != nil {
		t.Fatal("shot:", err)
	}
}

func TestDriveTabModalTrap(t *testing.T) {
	port := startThemeDrive(t)
	drive.Comment("Tab to the Open modal button")
	if err := drive.TabUntil(port, "open_modal"); err != nil {
		t.Fatal(err)
	}
	drive.Comment("Press Space to open the modal")
	if err := drive.Key(port, "space"); err != nil {
		t.Fatal(err)
	}
	mustCount(t, port, "modal_ok", 1)
	if err := drive.Shot(port, "Modal dialog open"); err != nil {
		t.Fatal("shot:", err)
	}
	got := focusedName(t, port)
	if got != "modal_name" && got != "modal_email" {
		t.Fatalf("modal first stop: focused %q, want a modal field", got)
	}

	drive.Comment("Tab around inside the modal")
	inside := map[string]bool{
		"modal_name": true, "modal_email": true,
		"modal_cancel": true, "modal_ok": true,
	}
	saw := map[string]bool{}
	for range 16 {
		got := focusedName(t, port)
		if !inside[got] {
			t.Fatalf("Tab leaked out of modal to %q", got)
		}
		saw[got] = true
		if err := drive.Key(port, "Tab"); err != nil {
			t.Fatal(err)
		}
	}
	if len(saw) < 2 {
		t.Fatalf("modal tab ring stuck on %v", saw)
	}

	drive.Comment("Press Escape to close the modal")
	if err := drive.Key(port, "escape"); err != nil {
		t.Fatal(err)
	}
	mustCount(t, port, "modal_ok", 0)
	if err := drive.Shot(port, "Modal dismissed"); err != nil {
		t.Fatal("shot:", err)
	}
}
