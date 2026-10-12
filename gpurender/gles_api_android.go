//go:build android

package gpurender

import "fmt"

func loadGLESLibs() error {
	libEGL, err := dlopenFirst("libEGL.so")
	if err != nil {
		return fmt.Errorf("libEGL: %w", err)
	}
	libGLES, err := dlopenFirst("libGLESv3.so", "libGLESv2.so")
	if err != nil {
		return fmt.Errorf("libGLESv2: %w", err)
	}
	if err := bindCommonGL(libEGL, libGLES); err != nil {
		return err
	}
	need := []struct {
		name string
		fn   interface{}
	}{
		{"eglCreateWindowSurface", &eglCreateWindowSurface},
		{"eglDestroySurface", &eglDestroySurface},
		{"eglSwapBuffers", &eglSwapBuffers},
		{"eglQuerySurface", &eglQuerySurface},
	}
	for _, s := range need {
		if err := bindSym(libEGL, s.name, s.fn); err != nil {
			return err
		}
	}
	return nil
}
