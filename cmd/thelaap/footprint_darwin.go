//go:build darwin

package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Quanto occupa davvero un processo.
//
// Su Apple Silicon la memoria è unificata: i pesi di un modello caricato sulla
// GPU stanno nella stessa RAM di tutto il resto, ma passano da IOAccelerator e
// **non compaiono in RSS**. Misurato su questa macchina: mtplx con Qwen3.6-27B
// dava 38,7 GB di RSS contro 59 GB reali, cioè il 34% in meno. Decidere se un
// modello ci sta guardando RSS significa sbagliare di decine di GB.
//
// La metrica giusta è `phys_footprint`, la stessa che guarda jetsam quando
// decide chi uccidere.
//
// Come leggerla, fra le strade provate:
//
//	syscall proc_info diretta  — non praticabile: su darwin/arm64 Go instrada
//	                             syscall.Syscall nella syscall() variadica di
//	                             libSystem, e su arm64 gli argomenti variadici
//	                             passano dallo stack. Restituisce ENOMEM.
//	vmmap --summary            — 2,6 s: troppo per un ciclo da 4 s
//	top -l 1 -stats mem        — 354 ms, e non dà il picco
//	footprint(1)               — 109 ms, e dà corrente *e* picco. Scelta.
//
// Il picco serve: un modello che ora occupa 59 GB ma ne ha toccati 79 va
// ammesso sulla base di 79, altrimenti si carica qualcosa che sta solo finché
// nessuno lo usa davvero.

var (
	reFootprint = regexp.MustCompile(`phys_footprint:\s+([\d.]+)\s*([KMGT]?B)`)
	rePicco     = regexp.MustCompile(`phys_footprint_peak:\s+([\d.]+)\s*([KMGT]?B)`)
)

// byteDaMisura converte "71 GB", "2464 KB", "0 B" in byte.
// footprint(1) usa unità binarie pur scrivendo KB/MB/GB.
func byteDaMisura(valore, unita string) uint64 {
	v := parseFloat(valore)
	if v < 0 {
		return 0
	}
	var mult float64 = 1
	switch strings.ToUpper(unita) {
	case "KB":
		mult = 1 << 10
	case "MB":
		mult = 1 << 20
	case "GB":
		mult = 1 << 30
	case "TB":
		mult = 1 << 40
	}
	return uint64(v * mult)
}

func occupazioneProcesso(pid int) (Occupazione, error) {
	if pid <= 0 {
		return Occupazione{}, fmt.Errorf("pid non valido: %d", pid)
	}
	out, err := cmdErr(6*time.Second, "footprint", fmt.Sprint(pid))
	if err == nil {
		mc := reFootprint.FindStringSubmatch(out)
		mp := rePicco.FindStringSubmatch(out)
		if len(mc) == 3 {
			o := Occupazione{CorrenteByte: byteDaMisura(mc[1], mc[2])}
			if len(mp) == 3 {
				o.PiccoByte = byteDaMisura(mp[1], mp[2])
			}
			if o.PiccoByte < o.CorrenteByte {
				o.PiccoByte = o.CorrenteByte
			}
			return o, nil
		}
	}

	// Ripiego: footprint(1) può mancare su un Mac senza gli strumenti da
	// sviluppatore. RSS sottostima la memoria Metal, quindi il dato esce
	// marcato come stima e l'interfaccia lo deve dire. Mai far passare una
	// stima per una misura.
	rss, err2 := cmdErr(4*time.Second, "ps", "-o", "rss=", "-p", fmt.Sprint(pid))
	if err2 != nil {
		return Occupazione{}, fmt.Errorf("né footprint né ps hanno risposto per il pid %d: %v / %v", pid, err, err2)
	}
	kb := parseFloat(strings.TrimSpace(rss))
	if kb <= 0 {
		return Occupazione{}, fmt.Errorf("nessuna misura per il pid %d", pid)
	}
	b := uint64(kb * 1024)
	return Occupazione{CorrenteByte: b, PiccoByte: b, Stimato: true}, nil
}

// pidInAscoltoSuPorta: chi tiene la porta. Serve per legare un runtime della
// configurazione al processo che lo esegue davvero.
func pidInAscoltoSuPorta(porta int) (int, error) {
	out, err := cmdErr(5*time.Second, "lsof", "-nP",
		fmt.Sprintf("-iTCP:%d", porta), "-sTCP:LISTEN", "-t")
	if err != nil {
		return 0, fmt.Errorf("lsof sulla porta %d: %w", porta, err)
	}
	// Più righe se il processo ha figli in ascolto: il primo è il padre.
	for _, riga := range strings.Fields(out) {
		if p := int(parseFloat(riga)); p > 0 {
			return p, nil
		}
	}
	return 0, fmt.Errorf("nessun processo in ascolto sulla porta %d", porta)
}
