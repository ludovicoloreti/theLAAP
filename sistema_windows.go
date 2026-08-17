//go:build windows

package main

import (
	"os/exec"
	"strings"
)

// Versione per Windows. I comandi passano da PowerShell invece che da una shell
// POSIX; il resto del programma non se ne accorge.

const SISTEMA = "Windows"

func exec_LookPath(nome string) (string, error) {
	if p, err := exec.LookPath(nome); err == nil {
		return p, nil
	}
	return exec.LookPath(nome + ".exe")
}

func memoriaSistema() (totale, libera, wired, compressa, swap float64) {
	// CIM è più affidabile del vecchio WMIC, che sulle versioni recenti non c'è più
	o := ps(`$c=Get-CimInstance Win32_OperatingSystem;
		"$($c.TotalVisibleMemorySize) $($c.FreePhysicalMemory) $($c.SizeStoredInPagingFiles) $($c.FreeSpaceInPagingFiles)"`)
	campi := strings.Fields(o)
	if len(campi) >= 4 {
		totale = parseFloat(campi[0]) / 1e6 // kB → GB
		libera = parseFloat(campi[1]) / 1e6
		swap = (parseFloat(campi[2]) - parseFloat(campi[3])) / 1e6
	}
	return
}

// tettoGraficaGB: memoria della scheda video, se c'è.
func tettoGraficaGB() float64 {
	if o := ps(`(Get-CimInstance Win32_VideoController | Sort-Object AdapterRAM -Descending |
		Select-Object -First 1).AdapterRAM`); o != "" {
		return parseFloat(o) / 1e9
	}
	if o := sh(`nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits`); o != "" {
		return parseFloat(strings.Fields(o)[0]) / 1024
	}
	return 0
}

func temaScuro() bool {
	o := ps(`(Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize' ` +
		`-Name AppsUseLightTheme -ErrorAction SilentlyContinue).AppsUseLightTheme`)
	return strings.TrimSpace(o) == "0"
}

func dimensioneCartella(path string) float64 {
	o := ps(`(Get-ChildItem -LiteralPath '` + strings.ReplaceAll(path, "'", "''") +
		`' -Recurse -File -ErrorAction SilentlyContinue | Measure-Object -Sum Length).Sum`)
	return parseFloat(o) / 1e9
}

// ps: esegue una riga di PowerShell e ne restituisce l'uscita.
func ps(comando string) string {
	return cmd("powershell", "-NoProfile", "-NonInteractive", "-Command", comando)
}

func comandiServizio(k candidato, binario string) (avvia, ferma, riavvia string) {
	if k.chiave == "lmstudio" && binario != "" {
		return binario + " server start", binario + " server stop",
			binario + " server stop; " + binario + " server start"
	}
	if k.chiave == "ollama" {
		return "", "", `Stop-Process -Name ollama -Force -ErrorAction SilentlyContinue; Start-Process ollama -ArgumentList serve -WindowStyle Hidden`
	}
	return "", "", ""
}

func comandiFermaTutto(rr []RuntimeCfg) (ferma, riaccendi string) {
	var f, r []string
	for _, x := range rr {
		if x.Ferma != "" {
			f = append(f, `Write-Host "> spengo `+x.Nome+`"; `+x.Ferma)
		}
		if x.Avvia != "" {
			r = append(r, `Write-Host "> riaccendo `+x.Nome+`"; `+x.Avvia)
		}
	}
	f = append(f, `Write-Host ""; Write-Host "memoria liberata"`)
	r = append(r, `Write-Host ""; Write-Host "fatto"`)
	return strings.Join(f, "; "), strings.Join(r, "; ")
}
