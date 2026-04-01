//go:build !windows

package config

import "os"

func hardenDir(dir string) {
	os.Chmod(dir, 0700) // best-effort
}
