package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ludovicoloreti/theLAAP/internal/budget"
)

// Cosa sa fare davvero ogni programma, verificato uno per uno:
//
//	Ollama      ollama stop <modello>     scarica il singolo modello
//	LM Studio   lms unload <modello>      scarica il singolo modello
//	oMLX        POST /admin/api/models/{id}/unload   esiste, ma vuole una
//	                                      sessione admin: finché non si decide
//	                                      come custodire la credenziale, resta
//	                                      il solo stop del servizio
//	mtplx       niente                    il modello È il demone: scaricarlo
//	                                      significa fermarlo
//
// Il pannello mostra la granularità vera di ciascuno. Un pulsante unico che
// sotto fa cose diverse è la bugia che poi costa una notte.

type RuntimeCapability struct {
	Chiave                string `json:"chiave"`
	Nome                  string `json:"nome"`
	ScaricaSingoloModello bool   `json:"scaricaSingoloModello"`
	// Cosa dire all'utente quando la capacità manca. In italiano, senza gergo.
	Nota string `json:"nota,omitempty"`
	// Cosa fa il pulsante quando non si può scaricare il singolo modello.
	Alternativa string `json:"alternativa,omitempty"`
}

// capacita legge il comando di scarico dalla configurazione. Se manca, il
// runtime non sa scaricare per modello e lo si dice.
func capacita(rc RuntimeCfg) RuntimeCapability {
	c := RuntimeCapability{Chiave: rc.Chiave, Nome: rc.Nome}
	if strings.TrimSpace(rc.ScaricaModello) != "" {
		c.ScaricaSingoloModello = true
		return c
	}
	c.Nota = rc.NotaScarico
	if c.Nota == "" {
		c.Nota = "questo programma non sa togliere un solo modello dalla memoria"
	}
	if rc.Ferma != "" {
		c.Alternativa = "fermare " + rc.Nome
	}
	return c
}

func apiCapability(w http.ResponseWriter, r *http.Request) {
	out := []RuntimeCapability{}
	for _, rc := range knownRuntimes() {
		out = append(out, capacita(rc))
	}
	writeJSON(w, out)
}

// unloadModel esegue il comando dichiarato, con il nome del modello messo
// fra apici: arriva da un servizio esterno e non deve poter diventare shell.
func unloadModel(chiave, modello string) (string, error) {
	if strings.TrimSpace(modello) == "" {
		return "", fmt.Errorf("non mi hai detto quale modello")
	}
	var rc *RuntimeCfg
	for _, x := range knownRuntimes() {
		if x.Chiave == chiave {
			c := x
			rc = &c
			break
		}
	}
	if rc == nil {
		return "", fmt.Errorf("non conosco il programma: %s", chiave)
	}
	if strings.TrimSpace(rc.ScaricaModello) == "" {
		cap := capacita(*rc)
		msg := rc.Nome + ": " + cap.Nota
		if cap.Alternativa != "" {
			msg += ". Puoi " + cap.Alternativa
		}
		return "", fmt.Errorf("%s", msg)
	}
	linea := strings.ReplaceAll(rc.ScaricaModello, "{modello}", shQuote(modello))
	out, err := shErr(30*time.Second, linea+" 2>&1")
	if err != nil {
		// Il messaggio del programma vale più del codice di uscita: se c'è, è
		// quello che dice all'utente cos'è andato storto.
		if m := withoutAnsi(out); m != "" {
			return out, fmt.Errorf("%s non ha scaricato %s: %s", rc.Nome, modello, trunc(m, 300))
		}
		return out, fmt.Errorf("%s non ha scaricato %s: %w", rc.Nome, modello, err)
	}
	return out, nil
}

func apiUnloadModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Runtime string `json:"runtime"`
		Model   string `json:"modello"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile: "+err.Error())
		return
	}
	out, err := unloadModel(req.Runtime, req.Model)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	// La fotografia della memoria è vecchia fino a 4 secondi: forzarne una
	// nuova evita che l'interfaccia mostri ancora il modello appena tolto.
	refreshMemory()
	if strings.TrimSpace(out) == "" {
		out = "fatto"
	}
	writeJSON(w, map[string]any{"ok": true, "output": trunc(out, 500)})
}

// ── preflight ───────────────────────────────────────────────────────────────

// currentBudget costruisce la fotografia su cui l'arbitro decide.
//
// Il peso di ogni runtime è il picco del suo processo, non la somma delle
// cartelle su disco: misurato su questa macchina, mtplx pesa 79 GB di picco
// contro ~30 GB di pesi su disco. Decidere col disco avrebbe autorizzato
// esattamente la combinazione che ha fatto panicare il Mac.
func currentBudget() budget.Budget {
	m := currentMemory()
	return budget.Budget{
		TotalBytes:     uint64(m.TotaleGB * 1e9),
		OSReserveBytes: uint64(systemReserveGB() * 1e9),
		// Già misurate dal monitor: rifarle a ogni richiesta costerebbe un
		// lsof e un footprint per runtime, mezzo secondo buttato.
		Used: m.Processi,
	}
}

// runtimeFootprints misura quanto pesa davvero ogni programma acceso.
//
// È la sola fonte di verità sull'occupazione. I numeri che i runtime danno di
// sé descrivono i pesi del modello, non la memoria che il processo tiene:
// misurato qui, mtplx dichiara 29,3 GB e ne occupa 84,8. La differenza sono
// KV cache e buffer, che non stanno su disco e non compaiono in `ps`.
func runtimeFootprints(caricati []ModelInRAM) []budget.RuntimeUsage {
	var out []budget.RuntimeUsage
	for _, rc := range knownRuntimes() {
		pid, err := pidListeningOnPort(rc.Porta)
		if err != nil {
			continue // spento: non occupa niente
		}
		occ, err := processFootprint(pid)
		if err != nil {
			continue
		}
		var modelli []string
		for _, c := range caricati {
			if c.Runtime == rc.Nome {
				modelli = append(modelli, c.Nome)
			}
		}
		out = append(out, budget.RuntimeUsage{
			Key:          rc.Chiave,
			Name:         rc.Nome,
			PeakBytes:    occ.ExpectedWeightBytes(),
			CurrentBytes: occ.CorrenteByte,
			Estimated:    occ.Stimato,
			Freeable:     strings.TrimSpace(rc.ScaricaModello) != "",
			Models:       modelli,
		})
	}
	return out
}

func currentPolicy() budget.Policy {
	return budget.Policy{
		OneLargeModelAtATime: true,
		LargeThresholdBytes:  uint64(largeModelThresholdGB() * 1e9),
	}
}

// apiPreflight: «se carico questo, ci sta?» — la domanda che il pannello non
// sapeva porsi, e la cui assenza è costata un kernel panic.
func apiPreflight(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model    string  `json:"modello"`
		PesoGB   float64 `json:"pesoGB"`   // se noto
		Percorso string  `json:"percorso"` // altrimenti si stima dal disco
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile: "+err.Error())
		return
	}
	peso := uint64(req.PesoGB * 1e9)
	stimato := false
	if peso == 0 && req.Percorso != "" {
		// Ultima risorsa: la dimensione su disco, gonfiata del fattore
		// misurato fra pesi e occupazione reale. Resta una stima e va detto.
		peso = uint64(sizeGB(req.Percorso) * diskToMemoryInflation * 1e9)
		stimato = true
	}
	v := currentBudget().Admits(peso, currentPolicy())
	writeJSON(w, map[string]any{
		"verdetto": v,
		"stimato":  stimato,
		"modello":  req.Model,
	})
}

// Quanto un modello occupa in memoria rispetto ai suoi pesi su disco, quando
// non si può misurare il processo. Misurato su questa macchina: Laguna Q6 è
// 86 GB su disco e 92,4 residenti (1,07); il Qwen di mtplx è ~30 GB su disco
// e 79 di picco (2,6). Si tiene il caso peggiore, perché sbagliare per eccesso
// costa un rifiuto e sbagliare per difetto costa la macchina.
const diskToMemoryInflation = 2.6

// ── forzatura della fotografia ──────────────────────────────────────────────

var refreshedAt = make(chan struct{}, 1)

// refreshMemory chiede al monitor una lettura subito, senza aspettare il
// prossimo giro. Non blocca: se una richiesta è già in coda, questa cade.
func refreshMemory() {
	select {
	case refreshedAt <- struct{}{}:
	default:
	}
}
