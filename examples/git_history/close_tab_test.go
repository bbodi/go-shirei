package main

import (
	"testing"
	"time"

	. "go.hasen.dev/shirei"
)

func TestCloseTabFromInsideFrame(t *testing.T) {
	skipSessionSave = true
	defer func() { skipSessionSave = false }()

	prevTabs, prevActive := appData.tabs, appData.active
	defer func() {
		appData.tabs = prevTabs
		appData.active = prevActive
	}()

	tab := newRepoTab("/tmp/close-tab-frame", "demo")
	appData.tabs = []*RepoTab{tab}
	appData.active = tab

	done := make(chan struct{})
	go func() {
		RunFrameFn(func() { closeTab(tab) })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("closeTab inside RunFrameFn deadlocked")
	}
	if len(appData.tabs) != 0 {
		t.Fatalf("tabs left: %d", len(appData.tabs))
	}
}
