//go:build linux

package main

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// Versione per Linux. Su una macchina con GPU dedicata la memoria di sistema e
// quella della scheda video sono separate: leggiamo entrambe.

const SYSTEM = "Linux"

func exec_LookPath(nome string) (string, error) { return exec.LookPath(nome) }

func systemMemory() (totale, libera, wired, compressa, swap float64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return
	}
	val := func(chiave string) float64 {
		re := regexp.MustCompile(`(?m)^` + chiave + `:\s+(\d+) kB`)
		if m := re.FindStringSubmatch(string(b)); len(m) > 1 {
			return parseFloat(m[1]) / 1e6 // kB → GB
		}
		return 0
	}
	totale = val("MemTotal")
	libera = val("MemAvailable")
	wired = val("Shmem") // il concetto più vicino: memoria non paginabile
	compressa = val("Zswap")
	swap = val("SwapTotal") - val("SwapFree")
	return
}

// graphicsCeilingGB: la memoria della scheda video, se c'è una NVIDIA o una AMD.
func graphicsCeilingGB() float64 {
	if o := sh("nvidia-smi --query-gpu=memory.total --format=csv,noheader,nounits 2>/dev/null | head -1"); o != "" {
		return parseFloat(o) / 1024 // MiB → GB
	}
	if o := sh("rocm-smi --showmeminfo vram --csv 2>/dev/null | tail -1 | cut -d, -f2"); o != "" {
		return parseFloat(o) / 1e9
	}
	return 0
}

func darkTheme() bool {
	// GNOME e derivati
	o := sh("gsettings get org.gnome.desktop.interface color-scheme 2>/dev/null")
	if strings.Contains(o, "dark") {
		return true
	}
	o = sh("gsettings get org.gnome.desktop.interface gtk-theme 2>/dev/null")
	if strings.Contains(strings.ToLower(o), "dark") {
		return true
	}
	// KDE
	if o := sh("grep -h 'ColorScheme=' ~/.config/kdeglobals 2>/dev/null | head -1"); o != "" {
		return strings.Contains(strings.ToLower(o), "dark")
	}
	return true // in dubbio, scuro: è il caso più comune sui sistemi da sviluppo
}

func folderSize(path string) float64 {
	if o := sh("du -sk " + shQuote(path) + " 2>/dev/null | cut -f1"); o != "" {
		return parseFloat(o) / 1e6
	}
	return 0
}

// serviceCommands: su Linux si usa systemd per utente, quando l'unità esiste.
func serviceCommands(k candidato, binario string) (avvia, ferma, riavvia string) {
	if k.unitaLinux != "" {
		u := k.unitaLinux
		if sh("systemctl --user list-unit-files "+u+".service 2>/dev/null | grep -c "+u) != "0" {
			return "systemctl --user start " + u,
				"systemctl --user stop " + u,
				"systemctl --user restart " + u
		}
	}
	if k.chiave == "lmstudio" && binario != "" {
		return binario + " server start", binario + " server stop",
			binario + " server stop; " + binario + " server start"
	}
	return "", "", ""
}

func stopAllCommands(rr []RuntimeCfg) (ferma, riaccendi string) {
	var f, r []string
	for _, x := range rr {
		if x.Ferma != "" {
			f = append(f, "echo '▸ spengo "+x.Nome+"'; "+x.Ferma+" 2>&1 | tail -1")
		}
		if x.Avvia != "" {
			r = append(r, "echo '▸ riaccendo "+x.Nome+"'; "+x.Avvia+" 2>&1 | tail -1")
		}
	}
	f = append(f, `for m in $(ollama ps 2>/dev/null | tail -n +2 | awk '{print $1}'); do ollama stop "$m"; done 2>/dev/null`,
		"sleep 2; echo; echo '✅ memoria liberata'")
	r = append(r, "echo; echo '✅ fatto. I modelli si ricaricano alla prima domanda.'")
	return strings.Join(f, ";\n"), strings.Join(r, ";\n")
}
