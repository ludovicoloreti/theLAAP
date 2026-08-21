package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// I modelli cambiano: quelli di oggi non saranno quelli fra sei mesi. Quindi
// niente elenchi di nomi nel codice — tutto quello che sappiamo di un modello
// viene da tre fonti, in ordine di fiducia:
//
//	1. quello che abbiamo MISURATO provandolo (velocità, se ragiona)
//	2. quello che il runtime DICHIARA (contesto, dimensione su disco)
//	3. quello che si deduce dal nome, come ultima spiaggia e sempre dichiarandolo
//
// Il risultato si chiama Profile e vive su disco, così le misure non si perdono.

type Profile struct {
	ID        string    `json:"id"`
	Runtime   string    `json:"runtime"`
	TokS      float64   `json:"tokS"`      // misurato
	Reasoning bool      `json:"reasoning"` // misurato
	GB        float64   `json:"gb"`        // dal disco
	Context   int       `json:"context"`
	Provato   time.Time `json:"provato"`
	UltimoUso time.Time `json:"ultimoUso,omitempty"`
	Etichetta string    `json:"etichetta"` // scritta dall'utente, vince su tutto
	Note      string    `json:"note"`
}

var (
	profili   = map[string]*Profile{}
	profiliMu sync.RWMutex
	// Sta accanto alla configurazione, non nella cartella del progetto.
	//
	// Non è ordine: è l'unico posto da cui si può leggere all'avvio. Quando il
	// progetto è in una directory utente protetta, aprire un file al suo interno
	// fa scattare TCC. Un figlio della .app che tenta quella
	// open() si ferma dentro la syscall e non ne esce: nessun log, porta mai
	// aperta, e la voce nella barra che lo rilancia in continuazione. Provato
	// col campionatore: 100% dei campioni in `open`, sia col binario di adesso
	// sia con quello di prima. Dal Terminale non si vede se il terminale ha già
	// il permesso necessario.
	//
	// Per la stessa ragione qui NON c'è un ripiego che guarda il percorso
	// vecchio: basterebbe quello a rimettere il blocco.
	PROFILI = home(".config/thelaap/profili.json")
)

func loadProfiles() {
	b, err := os.ReadFile(PROFILI)
	if err != nil {
		return
	}
	var lista []Profile
	if json.Unmarshal(b, &lista) != nil {
		return
	}
	profiliMu.Lock()
	defer profiliMu.Unlock()
	for i := range lista {
		p := lista[i]
		profili[p.Runtime+"|"+p.ID] = &p
	}
}

func saveProfiles() {
	profiliMu.RLock()
	var lista []Profile
	for _, p := range profili {
		lista = append(lista, *p)
	}
	profiliMu.RUnlock()
	b, err := json.MarshalIndent(lista, "", "  ")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(PROFILI), 0o755)
	os.WriteFile(PROFILI, b, 0o644)
}

func readProfile(runtime, id string) *Profile {
	profiliMu.RLock()
	defer profiliMu.RUnlock()
	if p, ok := profili[runtime+"|"+id]; ok {
		c := *p
		return &c
	}
	return nil
}

func updateProfile(runtime, id string, f func(*Profile)) {
	profiliMu.Lock()
	k := runtime + "|" + id
	if profili[k] == nil {
		profili[k] = &Profile{ID: id, Runtime: runtime}
	}
	f(profili[k])
	profiliMu.Unlock()
	saveProfiles()
}

// ── deduzioni dal nome: solo indizi, mai certezze ────────────────
// Ogni indizio porta con sé il MOTIVO, così l'interfaccia può dire
// "lo deduco dal nome" invece di far finta di saperlo.

type Hint struct {
	Tratto string `json:"tratto"` // "senza-filtri", "ragiona", "misto-esperti", "programmazione"…
	Perche string `json:"perche"` // in italiano, mostrabile all'utente
}

var SIGNALS = []struct {
	tratto string
	chiavi []string
	perche string
}{
	{"senza-filtri", []string{"heretic", "abliterat", "uncensor", "unfiltered", "dolphin"},
		"nel nome c'è un termine usato per i modelli a cui sono stati tolti i rifiuti"},
	{"programmazione", []string{"coder", "code", "codestral", "starcoder", "deepseek-coder"},
		"il nome indica un modello specializzato nel codice"},
	{"misto-esperti", []string{"a3b", "a4b", "a8b", "a17b", "moe", "-a2b"},
		"la sigla indica un modello a esperti: molti parametri totali, pochi attivi, quindi veloce"},
	{"vede-immagini", []string{"vl", "vision", "ocr", "mocr", "-vlm"},
		"il nome indica che sa leggere le immagini"},
	{"trascrive", []string{"whisper", "asr", "speech"},
		"il nome indica un modello per l'audio"},
	{"ricerca-testi", []string{"embed", "bge-", "nomic-embed", "reranker"},
		"serve a cercare dentro i documenti, non a conversare"},
	{"diffusione", []string{"diffusion"},
		"genera il testo a blocchi invece che parola per parola"},
	{"minuscolo", []string{"e2b", "-1b", "-2b", "-3b", "0.5b", "1.5b"},
		"la dimensione nel nome indica un modello molto piccolo"},
}

func indizi(id string) []Hint {
	l := strings.ToLower(id)
	var out []Hint
	visti := map[string]bool{}
	for _, s := range SIGNALS {
		if visti[s.tratto] {
			continue
		}
		for _, k := range s.chiavi {
			if strings.Contains(l, k) {
				out = append(out, Hint{s.tratto, s.perche})
				visti[s.tratto] = true
				break
			}
		}
	}
	return out
}

// Card: tutto quello che l'interfaccia deve sapere per disegnare un modello,
// già messo insieme e con l'origine di ogni informazione.
type Card struct {
	Model
	TokS      float64 `json:"tokS"`
	GB        float64 `json:"gb"`
	Provato   string  `json:"provato"`
	UltimoUso string  `json:"ultimoUso,omitempty"`
	Indizi    []Hint  `json:"indizi"`
	Etichetta string  `json:"etichetta"`
	// Note: le due frasi scritte dal modellino, salvate una volta in
	// profili.json. Senza questo campo l'interfaccia non ha da dove leggerle e
	// il riquadro «descritto dal modellino» resta vuoto per sempre.
	Note     string `json:"note"`
	Misurato bool   `json:"misurato"` // false = non l'abbiamo mai provato
}

func schede() []Card {
	modelli, _ := configState()
	out := make([]Card, 0, len(modelli)) // mai nil: il client la scorre sempre
	for _, m := range modelli {
		s := Card{Model: m, Indizi: indizi(m.ID)}
		if p := readProfile(m.Runtime, m.ID); p != nil {
			s.TokS, s.GB, s.Etichetta, s.Note = p.TokS, p.GB, p.Etichetta, p.Note
			s.Misurato = !p.Provato.IsZero()
			if s.Misurato {
				s.Provato = p.Provato.Format("2/1/2006")
			}
			if !p.UltimoUso.IsZero() {
				s.UltimoUso = p.UltimoUso.Format(time.RFC3339)
			}
		}
		out = append(out, s)
	}
	return out
}

func apiCards(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, schede())
}

// apiLabel: il nome che l'utente dà a un modello. Vince su ogni deduzione.
func apiLabel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Runtime   string `json:"runtime"`
		ID        string `json:"id"`
		Etichetta string `json:"etichetta"`
		Note      string `json:"note"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non valido")
		return
	}
	updateProfile(req.Runtime, req.ID, func(p *Profile) {
		p.Etichetta = trunc(req.Etichetta, 80)
		if req.Note != "" {
			p.Note = trunc(req.Note, 300)
		}
	})
	writeJSON(w, map[string]any{"ok": true})
}
