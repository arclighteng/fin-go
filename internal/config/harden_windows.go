//go:build windows

package config

import (
	"os/exec"
	"os/user"
)

func hardenDir(dir string) {
	u, err := user.Current()
	if err != nil {
		return
	}
	// Best-effort ACL hardening via icacls, matching the Python implementation.
	exec.Command("icacls", dir, "/inheritance:r", "/grant:r", u.Username+":(OI)(CI)F").Run()
}
