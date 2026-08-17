//go:build darwin || linux

package main

import (
	"os/exec"
	"syscall"
)

// I comandi di manutenzione sono pipeline di shell: `sh -c "... | ..."`.
// Uccidere solo `sh` lascia in giro i figli, che continuano a girare e a
// tenere la memoria — proprio quello che il pannello serve a evitare.
// Metterli in un gruppo di processi permette di ucciderli tutti insieme.
func isolateProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(c *exec.Cmd) {
	if c.Process == nil {
		return
	}
	// Il negativo indica il gruppo, non il singolo processo.
	if err := syscall.Kill(-c.Process.Pid, syscall.SIGKILL); err != nil {
		_ = c.Process.Kill() // ripiego: almeno il capofila
	}
}
