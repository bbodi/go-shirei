package main

import (
	"testing"
	"time"
)

func TestStoreKeepsExitedThenPrunes(t *testing.T) {
	s := NewProcessStore()
	t0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	started := t0.Add(-time.Minute)

	s.Update(&ProcSnapshot{
		Time:      t0,
		Processes: []ProcInfo{{PID: 10, Name: "foo", StartTime: started}},
	}, nil)
	if n := s.ActiveCount(); n != 1 {
		t.Fatalf("active after first sample: %d", n)
	}

	s.Update(&ProcSnapshot{Time: t0.Add(time.Second)}, nil)
	procs := s.Processes()
	if len(procs) != 1 {
		t.Fatalf("just-exited should stay, got %d", len(procs))
	}
	if procs[0].Running() {
		t.Fatal("missing process should be marked exited")
	}
	if procs[0].StoppedAt != t0.Add(time.Second) {
		t.Fatalf("StoppedAt = %v", procs[0].StoppedAt)
	}

	s.Update(&ProcSnapshot{Time: t0.Add(time.Second + keepStoppedFor)}, nil)
	if len(s.Processes()) != 1 {
		t.Fatal("should still be kept at exactly the keep window")
	}

	s.Update(&ProcSnapshot{Time: t0.Add(time.Second + keepStoppedFor + time.Nanosecond)}, nil)
	if len(s.Processes()) != 0 {
		t.Fatalf("should prune after %s, still have %d", keepStoppedFor, len(s.Processes()))
	}
}

func TestStoreKeepsSelectedAfterExit(t *testing.T) {
	s := NewProcessStore()
	t0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	s.Update(&ProcSnapshot{
		Time:      t0,
		Processes: []ProcInfo{{PID: 10, Name: "foo", StartTime: t0}},
	}, nil)
	selected := s.Processes()[0]

	s.Update(&ProcSnapshot{Time: t0.Add(time.Second)}, selected)
	s.Update(&ProcSnapshot{Time: t0.Add(time.Hour)}, selected)
	procs := s.Processes()
	if len(procs) != 1 {
		t.Fatalf("selected exited process should stay, got %d", len(procs))
	}
	if procs[0].Running() {
		t.Fatal("selected process should still be exited")
	}
}

func TestStoreKeepsPinnedAfterExit(t *testing.T) {
	s := NewProcessStore()
	t0 := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	s.Update(&ProcSnapshot{
		Time:      t0,
		Processes: []ProcInfo{{PID: 10, Name: "foo", StartTime: t0}},
	}, nil)
	s.Processes()[0].Pinned = true

	s.Update(&ProcSnapshot{Time: t0.Add(time.Second)}, nil)
	s.Update(&ProcSnapshot{Time: t0.Add(time.Hour)}, nil)
	procs := s.Processes()
	if len(procs) != 1 {
		t.Fatalf("pinned exited process should stay, got %d", len(procs))
	}
	if procs[0].Running() || !procs[0].Pinned {
		t.Fatal("pinned process should stay exited and pinned")
	}
}

func TestSteppedScale(t *testing.T) {
	cases := []struct {
		peak, min, step, want float64
	}{
		{0, 100, 50, 100},
		{100, 100, 50, 100},
		{120, 100, 50, 150},
		{150, 100, 50, 150},
		{151, 100, 50, 200},
		{400 << 20, 500 << 20, 500 << 20, 500 << 20},
		{501 << 20, 500 << 20, 500 << 20, 1000 << 20},
		{12, 10, 5, 15},
	}
	for _, c := range cases {
		if got := steppedScale(c.peak, c.min, c.step); got != c.want {
			t.Errorf("steppedScale(%v,%v,%v)=%v want %v", c.peak, c.min, c.step, got, c.want)
		}
	}
}

func TestOrderProcessesPinsFirst(t *testing.T) {
	a := &Process{ProcInfo: ProcInfo{PID: 1, CPUPercent: 90}}
	b := &Process{ProcInfo: ProcInfo{PID: 2, CPUPercent: 10}, Pinned: true}
	c := &Process{ProcInfo: ProcInfo{PID: 3, CPUPercent: 50}, Pinned: true}
	rows := []*Process{a, b, c}
	less := func(x, y *Process) bool { return x.CPUPercent < y.CPUPercent }
	orderProcesses(rows, less, true) // desc CPU
	// pinned first, then by CPU desc within each set: c (50) then b (10), then a
	if rows[0] != c || rows[1] != b || rows[2] != a {
		t.Fatalf("got pids %d,%d,%d", rows[0].PID, rows[1].PID, rows[2].PID)
	}
}
