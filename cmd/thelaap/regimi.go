package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Un «regime» è una configurazione di macchina che si accende e si spegne
// tutta insieme.
//
// Nasce da un caso concreto. Laguna-S-2.1-oQ6e occupa 89,5 GiB: su questo Mac
// da 128 ci sta comodamente da solo, ma non insieme a un secondo server da
// 79 GiB di picco — insieme hanno causato un kernel panic il 27/07/2026.
// (Dal 15/08/2026 lo slot usa l'oQ5e, 73 GiB: il margine passa da ~26 a ~39 GiB
// sui 112 del wired limit. Resta comunque un modello da usare da solo — con il
// secondo server acceso non ci sta nemmeno l'oQ4e.)
// E anche da solo veniva rifiutato, non per mancanza di RAM (16,4 GB liberi,
// swap a zero) ma per i margini che il programma si impone: la cache dei
// prompt viene sottratta dal tetto, e il profilo prudente usa il 90% di quel
// tetto invece del 95%. Due manopole che in un uso mono-modello non hanno
// motivo di essere strette.
//
// Il regime le muove insieme e ferma tutto il resto, perché l'una cosa
// giustifica l'altra: allargare i margini è sicuro **solo** se sulla macchina
// non c'è nient'altro, ed è esattamente ciò che il regime garantisce.
//
// Qui dentro c'è solo il meccanismo. Cosa esattamente si tocca è nella
// configurazione, come per tutto il resto del pannello: su un'altra macchina
// cambia il file, non il programma.

type RegimeCfg struct {
	Chiave string `json:"chiave"`
	Nome   string `json:"nome"`
	Cosa   string `json:"cosa,omitempty"` // a cosa serve, in parole semplici
	// L'unico programma che resta acceso. Gli altri vengono fermati dal
	// pannello con i comandi che già conosce.
	RuntimeAttivo string `json:"runtimeAttivo"`
	// Comandi che applicano e disfano il profilo di memoria. Facoltativi:
	// senza, il regime si limita a fermare gli altri programmi.
	Attiva    string `json:"attiva,omitempty"`
	Disattiva string `json:"disattiva,omitempty"`
	// File la cui esistenza dice che il regime è attivo. Senza questo il
	// pannello non saprebbe distinguere «spento» da «non lo so».
	Segno string `json:"segno,omitempty"`
}

type StatoRegime struct {
	RegimeCfg
	Attivo bool `json:"attivo"`
	// Quali programmi verrebbero fermati entrando: mostrarlo PRIMA evita la
	// sorpresa di veder cadere un servizio che si stava usando.
	Fermera []string `json:"fermera,omitempty"`
}

func regimeAttivo(r RegimeCfg) bool {
	if strings.TrimSpace(r.Segno) == "" {
		return false
	}
	p := r.Segno
	if strings.HasPrefix(p, "~/") {
		h, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		p = filepath.Join(h, p[2:])
	}
	_, err := os.Stat(p)
	return err == nil
}

// daFermare: i programmi accesi che il regime spegnerebbe.
func daFermare(r RegimeCfg) []string {
	var out []string
	for _, rc := range cfg().Runtime {
		if rc.Chiave == r.RuntimeAttivo || rc.Ferma == "" {
			continue
		}
		if _, err := pidInAscoltoSuPorta(rc.Porta); err == nil {
			out = append(out, rc.Nome)
		}
	}
	return out
}

func apiRegimi(w http.ResponseWriter, r *http.Request) {
	out := []StatoRegime{}
	for _, rg := range cfg().Regimi {
		s := StatoRegime{RegimeCfg: rg, Attivo: regimeAttivo(rg)}
		if !s.Attivo {
			s.Fermera = daFermare(rg)
		}
		out = append(out, s)
	}
	scriviJSON(w, out)
}

// apiRegime esegue il passaggio e ne trasmette l'output riga per riga.
//
// Riusa la stessa macchina di apiEsegui invece di inventarne una: il
// passaggio ferma servizi e riavvia un programma che carica decine di GB, ci
// mette minuti, e una richiesta che resta muta per minuti sembra bloccata.
func apiRegime(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Chiave string `json:"chiave"`
		Azione string `json:"azione"` // "on" oppure "off"
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "corpo non leggibile", http.StatusBadRequest)
		return
	}
	var rg *RegimeCfg
	for _, x := range cfg().Regimi {
		if x.Chiave == req.Chiave {
			c := x
			rg = &c
			break
		}
	}
	if rg == nil {
		http.Error(w, "non conosco questo regime", http.StatusNotFound)
		return
	}
	if req.Azione != "on" && req.Azione != "off" {
		http.Error(w, "azione sconosciuta", http.StatusBadRequest)
		return
	}

	linea := componiRegime(*rg, req.Azione)
	if strings.TrimSpace(linea) == "" {
		http.Error(w, "questo regime non ha niente da eseguire", http.StatusBadRequest)
		return
	}
	streamComando(w, r, linea)
}

// componiRegime costruisce la sequenza di shell del passaggio.
//
// Entrando si fermano prima gli altri programmi e poi si allargano i margini:
// l'ordine non è estetico. Allargare i margini mentre un altro modello è
// ancora residente è precisamente la configurazione che ha fatto panicare la
// macchina. Uscendo, l'ordine si inverte per lo stesso motivo.
func componiRegime(rg RegimeCfg, azione string) string {
	var passi []string
	eco := func(s string) string { return "echo " + shQuote(s) }

	altri := func() []string {
		var c []string
		for _, rc := range cfg().Runtime {
			if rc.Chiave == rg.RuntimeAttivo || rc.Ferma == "" {
				continue
			}
			c = append(c, rc.Nome+"\x00"+rc.Ferma+"\x00"+rc.Avvia)
		}
		return c
	}()

	if azione == "on" {
		passi = append(passi, eco("▸ entro nel regime: "+rg.Nome))
		for _, x := range altri {
			p := strings.Split(x, "\x00")
			passi = append(passi, eco("  fermo "+p[0]), p[1]+" >/dev/null 2>&1 || true")
		}
		passi = append(passi, "sleep 4")
		if rg.Attiva != "" {
			passi = append(passi, eco("  allargo i margini di memoria"), rg.Attiva)
		}
		passi = append(passi, eco("✅ regime attivo — carica pure il modello grande"))
	} else {
		passi = append(passi, eco("▸ esco dal regime: "+rg.Nome))
		if rg.Disattiva != "" {
			passi = append(passi, eco("  ripristino i margini prudenti"), rg.Disattiva)
		}
		for _, x := range altri {
			p := strings.Split(x, "\x00")
			if p[2] == "" {
				continue
			}
			passi = append(passi, eco("  riaccendo "+p[0]), p[2]+" >/dev/null 2>&1 || true")
		}
		passi = append(passi, eco("✅ stato normale ripristinato"))
	}
	return strings.Join(passi, "\n")
}
