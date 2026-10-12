package widgets

import (
	"sync"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"

	. "go.hasen.dev/shirei"
)

func TestDriveClickCheckbox(t *testing.T) {
	initFontsOnce.Do(shirei.InitFontSubsystem)
	ResetInputSession()
	GetHost().WindowSize = Vec2{600, 400}

	port, err := drive.FreePort()
	if err != nil {
		t.Fatal(err)
	}
	AcceptInputCommands(port)

	on := false
	scope := new(int)
	view := func() {
		ContainerWithKey(scope, Attrs(Pad(8)), func() {
			NextAccessName("ts")
			CheckBox(&on, "Show")
		})
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				RunFrameFn(view)
				time.Sleep(8 * time.Millisecond)
			}
		}
	}()

	time.Sleep(40 * time.Millisecond) // a few produces so the tree exists
	_, clickErr := drive.ClickOne(port, "ts")
	close(stop)
	wg.Wait()
	if clickErr != nil {
		t.Fatal(clickErr)
	}
	if !on {
		t.Fatal("checkbox did not toggle")
	}
}
