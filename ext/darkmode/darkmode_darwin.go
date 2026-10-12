//go:build darwin && !ios && !x11darwin

package darkmode

import (
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// AppKit / Foundation via purego.objc — no cgo. Matches cocoabackend so
// CGO_ENABLED=0 GOOS=darwin builds of apps that import ext/darkmode work.

var (
	selEffectiveAppearance               objc.SEL
	selBestMatchFromAppearancesWithNames objc.SEL
	selIsEqualToString                   objc.SEL
	selStandardUserDefaults              objc.SEL
	selStringForKey                      objc.SEL
	selDefaultCenter                     objc.SEL
	selAddObserverForName                objc.SEL
	selMainQueue                         objc.SEL
	selRetain                            objc.SEL
	selArray                             objc.SEL
	selAddObject                         objc.SEL
	selStringWithUTF8                    objc.SEL

	nsAppPtr                 *objc.ID
	nsAppearanceNameAqua     objc.ID
	nsAppearanceNameDarkAqua objc.ID

	themeBlock    objc.Block
	themeObserver objc.ID
)

func ptr[T any](p uintptr) *T {
	return *(**T)(unsafe.Pointer(&p))
}

func loadID(lib uintptr, name string) objc.ID {
	addr, err := purego.Dlsym(lib, name)
	if err != nil || addr == 0 {
		return 0
	}
	return *ptr[objc.ID](addr)
}

func loadIDPtr(lib uintptr, name string) *objc.ID {
	addr, err := purego.Dlsym(lib, name)
	if err != nil || addr == 0 {
		return nil
	}
	return ptr[objc.ID](addr)
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8, s)
}

func nsArray(objs ...objc.ID) objc.ID {
	a := objc.ID(objc.GetClass("NSMutableArray")).Send(selArray)
	for _, o := range objs {
		a.Send(selAddObject, o)
	}
	return a
}

func currentNSApp() objc.ID {
	if nsAppPtr == nil {
		return 0
	}
	return *nsAppPtr
}

func isDark() bool {
	if app := currentNSApp(); app != 0 {
		appearance := app.Send(selEffectiveAppearance)
		if appearance != 0 && nsAppearanceNameAqua != 0 && nsAppearanceNameDarkAqua != 0 {
			names := nsArray(nsAppearanceNameAqua, nsAppearanceNameDarkAqua)
			match := appearance.Send(selBestMatchFromAppearancesWithNames, names)
			if match != 0 {
				return match.Send(selIsEqualToString, nsAppearanceNameDarkAqua) != 0
			}
		}
	}
	defaults := objc.ID(objc.GetClass("NSUserDefaults")).Send(selStandardUserDefaults)
	if defaults == 0 {
		return false
	}
	style := defaults.Send(selStringForKey, nsString("AppleInterfaceStyle"))
	if style == 0 {
		return false
	}
	return style.Send(selIsEqualToString, nsString("Dark")) != 0
}

func initPlatform() {
	if _, err := purego.Dlopen("/System/Library/Frameworks/Cocoa.framework/Cocoa",
		purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return
	}
	appkit, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit",
		purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}

	selEffectiveAppearance = objc.RegisterName("effectiveAppearance")
	selBestMatchFromAppearancesWithNames = objc.RegisterName("bestMatchFromAppearancesWithNames:")
	selIsEqualToString = objc.RegisterName("isEqualToString:")
	selStandardUserDefaults = objc.RegisterName("standardUserDefaults")
	selStringForKey = objc.RegisterName("stringForKey:")
	selDefaultCenter = objc.RegisterName("defaultCenter")
	selAddObserverForName = objc.RegisterName("addObserverForName:object:queue:usingBlock:")
	selMainQueue = objc.RegisterName("mainQueue")
	selRetain = objc.RegisterName("retain")
	selArray = objc.RegisterName("array")
	selAddObject = objc.RegisterName("addObject:")
	selStringWithUTF8 = objc.RegisterName("stringWithUTF8String:")

	nsAppPtr = loadIDPtr(appkit, "NSApp")
	nsAppearanceNameAqua = loadID(appkit, "NSAppearanceNameAqua")
	nsAppearanceNameDarkAqua = loadID(appkit, "NSAppearanceNameDarkAqua")

	setDarkMode(isDark())

	center := objc.ID(objc.GetClass("NSDistributedNotificationCenter")).Send(selDefaultCenter)
	queue := objc.ID(objc.GetClass("NSOperationQueue")).Send(selMainQueue)
	if center == 0 || queue == 0 {
		return
	}
	themeBlock = objc.NewBlock(func(_ objc.Block, _ objc.ID) {
		setDarkMode(isDark())
	})
	themeObserver = center.Send(selAddObserverForName,
		nsString("AppleInterfaceThemeChangedNotification"),
		objc.ID(0),
		queue,
		objc.ID(themeBlock),
	)
	if themeObserver != 0 {
		themeObserver.Send(selRetain)
	}
}
