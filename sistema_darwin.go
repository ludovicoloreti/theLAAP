//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Tutto ciò che è specifico di macOS sta qui. Gli altri sistemi hanno un file
// gemello: il resto del programma non sa su cosa sta girando.

const SISTEMA = "macOS"

func exec_LookPath(nome string) (string, error) { return exec.LookPath(nome) }

// memoriaSistema: su Apple Silicon la memoria è unificata, quindi RAM e VRAM
// sono la stessa cosa e una lettura sola basta.
func memoriaSistema() (totale, libera, wired, compressa, swap float64) {
	totale = parseFloat(cmd("sysctl", "-n", "hw.memsize")) / 1e9

	vm := cmd("vm_stat")
	pag := func(etichetta string) float64 {
		re := regexp.MustCompile(etichetta + `:\s+(\d+)`)
		if m := re.FindStringSubmatch(vm); len(m) > 1 {
			return parseFloat(m[1]) * 16384 / 1e9 // pagine da 16 KB
		}
		return 0
	}
	libera = pag("Pages free") + pag("Pages inactive")
	wired = pag("Pages wired down")
	compressa = pag("Pages occupied by compressor")

	if sw := cmd("sysctl", "-n", "vm.swapusage"); sw != "" {
		re := regexp.MustCompile(`used = ([\d.]+)([MG])`)
		if m := re.FindStringSubmatch(sw); len(m) > 2 {
			swap = parseFloat(m[1])
			if m[2] == "M" {
				swap /= 1024
			}
		}
	}
	return
}

// tettoGrafica: quanta memoria può bloccare la GPU. Solo Apple Silicon.
//
// Il sysctl è in MiB (unità binarie), ma tutto il resto del pannello lavora in
// GB decimali — come Monitoraggio Attività. Dividere per 1024 dava GiB, e quel
// numero finiva nella stessa barra dei GB decimali: il tetto veniva disegnato
// circa 8 GB più corto del vero.
func tettoGraficaGB() float64 {
	return parseFloat(cmd("sysctl", "-n", "iogpu.wired_limit_mb")) * 1048576 / 1e9
}

func temaScuro() bool {
	return strings.TrimSpace(sh("defaults read -g AppleInterfaceStyle 2>/dev/null")) == "Dark"
}

// dimensioneCartellaGB
func dimensioneCartella(path string) float64 {
	if o := sh("du -sk " + shQuote(path) + " 2>/dev/null | cut -f1"); o != "" {
		return parseFloat(o) / 1e6
	}
	return 0
}

// etichettaLaunchd: qual è il servizio launchd che esegue questo programma.
//
// NON si può indovinare dal nome del programma. Ognuno chiama i propri servizi
// come vuole: `homebrew.mxcl.omlx` se installato con Homebrew, `com.tizio.omlx`
// se l'utente si è scritto un LaunchAgent a mano. Scriverne uno nel codice
// significa che su un altro computer i pulsanti accendi/spegni puntano a un
// servizio inesistente e falliscono in silenzio — che è peggio di non averli.
//
// Quindi si cerca: fra i LaunchAgent dell'utente e i servizi Homebrew, quale
// esegue davvero il binario o nomina il programma.
func etichettaLaunchd(chiave, binario string) string {
	// 1) I LaunchAgent dell'utente: si guarda dentro chi esegue quel binario.
	if h, err := os.UserHomeDir(); err == nil {
		dir := filepath.Join(h, "Library", "LaunchAgents")
		if voci, err := os.ReadDir(dir); err == nil {
			for _, v := range voci {
				if v.IsDir() || !strings.HasSuffix(v.Name(), ".plist") {
					continue
				}
				b, err := os.ReadFile(filepath.Join(dir, v.Name()))
				if err != nil {
					continue
				}
				t := string(b)
				if (binario != "" && strings.Contains(t, binario)) ||
					strings.Contains(strings.ToLower(t), "/"+chiave) {
					return strings.TrimSuffix(v.Name(), ".plist")
				}
			}
		}
	}
	// 2) Il servizio Homebrew, se c'è: `brew services` lo chiama così.
	brew := "homebrew.mxcl." + chiave
	if out, err := cmdErr(4*time.Second, "launchctl", "list"); err == nil &&
		strings.Contains(out, brew) {
		return brew
	}
	return ""
}

// comandiServizio: su macOS i servizi si governano con launchd; se un programma
// non ha un'unità, si ripiega sui comandi del programma stesso.
//
// bootout/bootstrap, non load/unload: i primi sono deprecati, e `kickstart`
// NON rilegge il plist — se la configurazione del servizio è cambiata,
// riavvierebbe con quella vecchia dicendo che è andato tutto bene.
func comandiServizio(k candidato, binario string) (avvia, ferma, riavvia string) {
	if u := etichettaLaunchd(k.chiave, binario); u != "" {
		p := "~/Library/LaunchAgents/" + u + ".plist"
		g := "gui/$(id -u)/" + u
		if strings.HasPrefix(u, "homebrew.") {
			// I servizi Homebrew si governano col loro comando: il plist sta
			// altrove e cambia fra installazioni.
			return "brew services start " + k.chiave,
				"brew services stop " + k.chiave,
				"brew services restart " + k.chiave
		}
		return "launchctl bootstrap gui/$(id -u) " + p,
			"launchctl bootout " + g,
			"launchctl bootout " + g + " 2>/dev/null; sleep 3; launchctl bootstrap gui/$(id -u) " + p
	}
	if k.chiave == "lmstudio" && binario != "" {
		return binario + " server start", binario + " server stop",
			binario + " server stop; " + binario + " server start"
	}
	return "", "", ""
}

// I comandi di launchd si lamentano quando il servizio è già nello stato che
// chiedi ("Try running launchctl bootout as root for richer errors"), e quel
// messaggio fa pensare a un guasto quando invece non c'era niente da fare.
// Quindi: errori silenziati, e una riga in italiano che dice cos'è successo
// davvero, verificando prima e dopo se il programma risponde.
func comandiFermaTutto(rr []RuntimeCfg) (ferma, riaccendi string) {
	risponde := func(x RuntimeCfg) string {
		return fmt.Sprintf("curl -s -m 2 http://127.0.0.1:%d%s >/dev/null 2>&1", x.Porta, x.Elenco)
	}
	var f, r []string
	for _, x := range rr {
		if x.Ferma != "" {
			f = append(f, fmt.Sprintf(`if %s; then
  %s >/dev/null 2>&1; sleep 1
  if %s; then echo "· %s — non sono riuscito a spegnerlo"; else echo "· %s — spento"; fi
else echo "· %s — era già spento"; fi`,
				risponde(x), x.Ferma, risponde(x), x.Nome, x.Nome, x.Nome))
		}
		if x.Avvia != "" {
			r = append(r, fmt.Sprintf(`if %s; then echo "· %s — era già acceso"
else %s >/dev/null 2>&1; echo "· %s — acceso"; fi`,
				risponde(x), x.Nome, x.Avvia, x.Nome))
		}
	}
	f = append(f,
		`n=0; for m in $(ollama ps 2>/dev/null | tail -n +2 | awk '{print $1}'); do ollama stop "$m" >/dev/null 2>&1; n=$((n+1)); done`,
		`[ "${n:-0}" -gt 0 ] && echo "· scaricati $n modelli da Ollama"`,
		`echo; echo "✅ fatto — la memoria è libera. Per rimettere tutto: «Riaccendi tutto»."`)
	r = append(r,
		`echo; echo "✅ fatto — i programmi caricano i modelli alla prima domanda, ci vuole qualche secondo."`)
	return strings.Join(f, "\n"), strings.Join(r, "\n")
}
