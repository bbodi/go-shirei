package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"go.hasen.dev/shirei/drive"
)

func gitDriveRepo(t *testing.T, name string) string {
	t.Helper()
	repo, run := gitTestRepo(t)
	writeFile(t, repo, name+".txt", name+"\n")
	run("add", name+".txt")
	run("commit", "-m", "init")
	return repo
}

func startDriveGitHistory(t *testing.T, sess sessionData) int {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.json")
	b, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return drive.Start(t, ".", "--session", path)
}

func mustCount(t *testing.T, port int, q string, want int) {
	t.Helper()
	if err := drive.WaitCount(port, q, want); err != nil {
		t.Fatal(err)
	}
}

func TestDriveCloseTab(t *testing.T) {
	repoA := gitDriveRepo(t, "alpha")
	repoB := gitDriveRepo(t, "beta")
	port := startDriveGitHistory(t, sessionData{
		Tabs:    []string{repoA, repoB},
		Active:  0,
		Recents: []string{repoA, repoB},
	})
	mustCount(t, port, "close", 2)
	drive.Comment("Close the first tab")
	if _, err := drive.Click(port, "close"); err != nil {
		t.Fatal("click close:", err)
	}
	if err := drive.Ping(port); err != nil {
		t.Fatal("app unresponsive after close:", err)
	}
	mustCount(t, port, "close", 1)
	if err := drive.Shot(port, "One tab remains"); err != nil {
		t.Fatal("shot:", err)
	}
}

func TestDriveOpenBrowser(t *testing.T) {
	repo := gitDriveRepo(t, "alpha")
	port := startDriveGitHistory(t, sessionData{
		Tabs:    []string{repo},
		Recents: []string{repo},
	})
	mustCount(t, port, "top_bar files_browser", 1)
	mustCount(t, port, "file_browser", 0)
	drive.Comment("Open the file browser")
	if _, err := drive.Click(port, "top_bar files_browser"); err != nil {
		t.Fatal("click Open:", err)
	}
	mustCount(t, port, "file_browser", 1)
	if err := drive.Shot(port, "File browser open"); err != nil {
		t.Fatal("shot:", err)
	}
}

func TestDriveRecentMenu(t *testing.T) {
	repo := gitDriveRepo(t, "alpha")
	port := startDriveGitHistory(t, sessionData{
		Tabs:    []string{repo},
		Recents: []string{repo},
	})
	mustCount(t, port, "top_bar recent", 1)
	mustCount(t, port, "recent_menu", 0)
	drive.Comment("Open the recent repositories menu")
	if _, err := drive.Click(port, "top_bar recent"); err != nil {
		t.Fatal("click Recent:", err)
	}
	mustCount(t, port, "recent_menu", 1)
	if err := drive.Shot(port, "Recent repositories menu"); err != nil {
		t.Fatal("shot:", err)
	}
}
