package shirei

import (
	"os"
	"testing"
)

func smallSystemFontPath(t *testing.T) string {
	t.Helper()
	path := "/System/Library/Fonts/Helvetica.ttc"
	if _, err := os.Stat(path); err != nil {
		path = "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf"
		if _, err := os.Stat(path); err != nil {
			t.Skip("no small system UI font")
		}
	}
	return path
}

func registerFileBackedFace(t *testing.T) FontId {
	t.Helper()
	path := smallSystemFontPath(t)
	UseFontFile(path)
	for _, f := range AllFontFaces() {
		if f.Filepath == path {
			return f.FontId
		}
	}
	t.Fatal("UseFontFile did not register")
	return 0
}

func TestUnloadKeepsUsedFace(t *testing.T) {
	savedN := parsedFontIdleFrames
	parsedFontIdleFrames = 2
	defer func() { parsedFontIdleFrames = savedN }()

	fid := registerFileBackedFace(t)
	savedFrame := ui.FrameNumber
	defer func() { ui.FrameNumber = savedFrame }()

	ui.FrameNumber = 1000
	if GetParsedFont(fid) == nil {
		t.Fatal("parse failed")
	}
	if !FontParsed(fid) || !FontWarmed(fid) {
		t.Fatal("expected parsed and warmed after GetParsedFont")
	}

	unloadFileBackedParsedFonts()
	if !FontParsed(fid) {
		t.Fatal("used face must stay resident")
	}
	if !FontWarmed(fid) {
		t.Fatal("warmed must survive unload")
	}
}

func TestUnloadDropsIdleFileBackedFace(t *testing.T) {
	savedN := parsedFontIdleFrames
	parsedFontIdleFrames = 2
	defer func() { parsedFontIdleFrames = savedN }()

	fid := registerFileBackedFace(t)
	savedFrame := ui.FrameNumber
	defer func() { ui.FrameNumber = savedFrame }()

	ui.FrameNumber = 1000
	if GetParsedFont(fid) == nil {
		t.Fatal("parse failed")
	}

	ui.FrameNumber = 1003 // 1003-1000=3 > 2
	if n := unloadFileBackedParsedFonts(); n == 0 {
		t.Fatal("expected idle file-backed face to drop")
	}
	if FontParsed(fid) {
		t.Fatal("file-backed face still resident after idle unload")
	}
	if !FontWarmed(fid) {
		t.Fatal("warmed must survive unload")
	}

	if GetParsedFont(fid) == nil {
		t.Fatal("re-parse after unload failed")
	}
	if !FontParsed(fid) {
		t.Fatal("expected resident after re-parse")
	}
}

func TestUnloadKeepsUseFontBytes(t *testing.T) {
	// Microns is registered from widgets via UseFontBytes; any in-memory
	// face with no filepath must survive unload.
	var fid FontId
	for _, f := range AllFontFaces() {
		if f.Filepath == "" && f.FontId != 0 {
			fid = f.FontId
			break
		}
	}
	if fid == 0 {
		t.Skip("no UseFontBytes face registered")
	}
	if GetParsedFont(fid) == nil && !FontParsed(fid) {
		// bytes faces are published already parsed
		t.Skip("bytes face not parsed")
	}

	unloadFileBackedParsedFonts()
	if !FontParsed(fid) {
		t.Fatalf("UseFontBytes face %d dropped by unload", fid)
	}
}
