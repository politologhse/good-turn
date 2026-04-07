// Package hybin locates the bundled or installed Hysteria2 binary.
// Single source of truth — used by both GUI runtime and Doctor.
package hybin

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Find returns the path to a hysteria binary, searching:
//  1. Same directory as the calling executable (bundled .app/Contents/MacOS, etc.)
//  2. Current working directory
//  3. PATH
func Find() (string, error) {
	name := "hysteria"
	if runtime.GOOS == "windows" {
		name = "hysteria.exe"
	}

	// 1. Next to executable
	if exePath, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 2. Current directory
	if _, err := os.Stat(name); err == nil {
		abs, _ := filepath.Abs(name)
		return abs, nil
	}

	// 3. PATH
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("hysteria binary not found (looked next to app, in cwd, and PATH)")
}
