//go:build windows

package main

import "os/exec"

// Su Windows non esistono i gruppi di processi in senso POSIX: si uccide il
// processo lanciato. I comandi di manutenzione qui non sono pipeline di shell,
// quindi la differenza è meno rilevante.
func isolateProcessGroup(c *exec.Cmd) {}

func killGroup(c *exec.Cmd) {
	if c.Process != nil {
		_ = c.Process.Kill()
	}
}
