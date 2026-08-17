//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"
)

// Su Windows si passa da PowerShell, come già fa il resto delle letture di
// sistema. WorkingSet64 è l'analogo di RSS e non comprende la memoria della
// GPU: il dato esce sempre marcato come stima.
func occupazioneProcesso(pid int) (Occupazione, error) {
	if pid <= 0 {
		return Occupazione{}, fmt.Errorf("pid non valido: %d", pid)
	}
	out, err := cmdErr(8*time.Second, "powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Get-Process -Id %d).WorkingSet64, (Get-Process -Id %d).PeakWorkingSet64", pid, pid))
	if err != nil {
		return Occupazione{}, fmt.Errorf("Get-Process per il pid %d: %w", pid, err)
	}
	righe := strings.Fields(out)
	if len(righe) == 0 {
		return Occupazione{}, fmt.Errorf("nessuna misura per il pid %d", pid)
	}
	corrente := uint64(parseFloat(righe[0]))
	picco := corrente
	if len(righe) > 1 {
		if p := uint64(parseFloat(righe[1])); p > picco {
			picco = p
		}
	}
	if corrente == 0 {
		return Occupazione{}, fmt.Errorf("misura nulla per il pid %d", pid)
	}
	return Occupazione{CorrenteByte: corrente, PiccoByte: picco, Stimato: true}, nil
}

func pidInAscoltoSuPorta(porta int) (int, error) {
	out, err := cmdErr(8*time.Second, "powershell", "-NoProfile", "-Command",
		fmt.Sprintf("(Get-NetTCPConnection -LocalPort %d -State Listen -ErrorAction SilentlyContinue | Select-Object -First 1).OwningProcess", porta))
	if err != nil {
		return 0, fmt.Errorf("Get-NetTCPConnection sulla porta %d: %w", porta, err)
	}
	if p := int(parseFloat(strings.TrimSpace(out))); p > 0 {
		return p, nil
	}
	return 0, fmt.Errorf("nessun processo in ascolto sulla porta %d", porta)
}
