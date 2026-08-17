//go:build linux

package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Su Linux la memoria del processo si legge da /proc, senza lanciare comandi.
//
// VmHWM è il massimo storico di VmRSS: l'equivalente più vicino al picco che
// footprint(1) dà su macOS. Non copre la memoria della GPU discreta, che su
// Linux è separata dalla RAM e va chiesta al driver — per questo il dato è
// marcato come stima quando c'è una GPU NVIDIA/AMD di mezzo.
func processFootprint(pid int) (Footprint, error) {
	if pid <= 0 {
		return Footprint{}, fmt.Errorf("pid non valido: %d", pid)
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return Footprint{}, fmt.Errorf("lettura di /proc/%d/status: %w", pid, err)
	}
	var rss, hwm uint64
	for _, riga := range strings.Split(string(b), "\n") {
		campi := strings.Fields(riga)
		if len(campi) < 2 {
			continue
		}
		switch campi[0] {
		case "VmRSS:":
			rss = uint64(parseFloat(campi[1])) * 1024
		case "VmHWM:":
			hwm = uint64(parseFloat(campi[1])) * 1024
		}
	}
	if rss == 0 {
		return Footprint{}, fmt.Errorf("nessun VmRSS per il pid %d", pid)
	}
	if hwm < rss {
		hwm = rss
	}
	// Con una GPU discreta la memoria dei pesi non sta in VmRSS: è nella VRAM
	// e la conta il driver. Dirlo, invece di far passare il numero per completo.
	stimato := hasDiscreteGPU()
	return Footprint{CorrenteByte: rss, PiccoByte: hwm, Stimato: stimato}, nil
}

func hasDiscreteGPU() bool {
	if _, err := os.Stat("/proc/driver/nvidia"); err == nil {
		return true
	}
	if _, err := os.Stat("/sys/module/amdgpu"); err == nil {
		return true
	}
	return false
}

func pidListeningOnPort(porta int) (int, error) {
	out, err := shErr(5*time.Second,
		fmt.Sprintf("ss -lptnH 'sport = :%d' 2>/dev/null | grep -o 'pid=[0-9]*' | head -1 | cut -d= -f2", porta))
	if err != nil {
		return 0, fmt.Errorf("ss sulla porta %d: %w", porta, err)
	}
	if p := int(parseFloat(strings.TrimSpace(out))); p > 0 {
		return p, nil
	}
	return 0, fmt.Errorf("nessun processo in ascolto sulla porta %d", porta)
}
