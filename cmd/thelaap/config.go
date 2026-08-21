package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// I percorsi dei client vengono dalla configurazione; questi valgono solo se
// la configurazione non li dichiara.
func filePi() string {
	for _, c := range cfg().Clienti {
		if c.Formato == "pi" {
			return espandi(c.File)
		}
	}
	return PI_CFG
}

func fileOC() string {
	for _, c := range cfg().Clienti {
		if c.Formato == "opencode" {
			return espandi(c.File)
		}
	}
	return OC_CFG
}

var (
	PI_CFG = home(".pi/agent/models.json")
	OC_CFG = home(".config/opencode/opencode.json")
	// Accanto alla configurazione, per la stessa ragione di PROFILI in
	// profiles.go: una cartella utente protetta da TCC può bloccare una .app in
	// modo silenzioso. Qui il danno sarebbe più insidioso che all'avvio: il
	// backup si scrive appena prima di sovrascrivere una configurazione.
	BACKUP = home(".config/thelaap/backup-config")
)

func home(p string) string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, p)
}

// Model — vista unificata di una voce presente nelle config dei client.
type Model struct {
	Runtime   string `json:"runtime"`   // chiave Pi: mtplx / omlx / lmstudio / ...
	ID        string `json:"id"`        // id esposto dal server
	Nome      string `json:"nome"`      // etichetta leggibile
	Reasoning bool   `json:"reasoning"` // il modello produce reasoning_content?
	// ThinkBloccato: il thinking non si può spegnere. In Pi si esprime con "off": null
	// dentro thinkingLevelMap. Va PRESERVATO: rigenerare la mappa senza questo campo
	// riattiverebbe di nascosto la possibilità di spegnerlo.
	ThinkBloccato bool `json:"thinkBloccato"`
	// MappaEffort: la thinkingLevelMap già scritta nella config, conservata così com'è.
	// Va PRESERVATA per lo stesso motivo di ThinkBloccato, ma il danno è peggiore: i
	// livelli ammessi dipendono dal modello. Qwen3.8 accetta solo xhigh/medium/low e
	// risponde 400 «Unexpected reasoning effort high» — la mappa generica manda "high"
	// e rompe ogni richiesta. Rigenerarla da zero ha già cancellato una mappa corretta
	// il 15/08/2026. Se è nil si usa il default generico.
	MappaEffort map[string]any `json:"-"`
	Context     int            `json:"context"`
	MaxTokens   int            `json:"maxTokens"`
	InPi        bool           `json:"inPi"`
	InOC        bool           `json:"inOC"`
	Servito     bool           `json:"servito"` // esiste davvero sul server?
}

func readJSON(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return m, nil
}

func num(v any, def int) int {
	if f, ok := v.(float64); ok {
		return int(f)
	}
	return def
}

// configState costruisce la vista unificata dalle due config + da cosa è servito.
func configState() ([]Model, []string) {
	var errori []string
	indice := map[string]*Model{} // chiave: runtime|id

	pi, err := readJSON(filePi())
	if err != nil {
		errori = append(errori, "Pi: "+err.Error())
	} else if provs, ok := pi["providers"].(map[string]any); ok {
		for chiave, pv := range provs {
			p, _ := pv.(map[string]any)
			ms, _ := p["models"].([]any)
			for _, mv := range ms {
				m, _ := mv.(map[string]any)
				id, _ := m["id"].(string)
				if id == "" {
					continue
				}
				k := chiave + "|" + id
				if _, dup := indice[k]; dup {
					errori = append(errori, "duplicato in Pi: "+k)
					continue
				}
				nome, _ := m["name"].(string)
				reas, _ := m["reasoning"].(bool)
				// "off" presente e a null = thinking non disattivabile
				bloccato := false
				var mappa map[string]any
				if tlm, ok := m["thinkingLevelMap"].(map[string]any); ok {
					if v, presente := tlm["off"]; presente && v == nil {
						bloccato = true
					}
					mappa = tlm
				}
				indice[k] = &Model{
					Runtime: chiave, ID: id, Nome: nome, Reasoning: reas, ThinkBloccato: bloccato,
					MappaEffort: mappa,
					Context:     num(m["contextWindow"], 131072), MaxTokens: num(m["maxTokens"], 32768),
					InPi: true,
				}
			}
		}
	}

	oc, err := readJSON(fileOC())
	if err != nil {
		errori = append(errori, "OpenCode: "+err.Error())
	} else if provs, ok := oc["provider"].(map[string]any); ok {
		for chiaveOC, pv := range provs {
			chiave := chiaveOC
			for _, rt := range configuredRuntimes() { // OpenCode chiama "mlx" ciò che Pi chiama "lmstudio"
				if rt.ChiaveOC == chiaveOC {
					chiave = rt.Chiave
					break
				}
			}
			p, _ := pv.(map[string]any)
			ms, _ := p["models"].(map[string]any)
			for id, mv := range ms {
				k := chiave + "|" + id
				m, _ := mv.(map[string]any)
				nome, _ := m["name"].(string)
				ctx, out := 131072, 32768
				if lim, ok := m["limit"].(map[string]any); ok {
					ctx, out = num(lim["context"], ctx), num(lim["output"], out)
				}
				if ex, ok := indice[k]; ok {
					ex.InOC = true
				} else {
					indice[k] = &Model{Runtime: chiave, ID: id, Nome: nome,
						Context: ctx, MaxTokens: out, InOC: true}
				}
			}
		}
	}

	// Incrocia con ciò che i server dichiarano davvero e aggiunge anche i
	// modelli che non sono ancora nei menu dei client.
	//
	// Prima l'elenco nasceva esclusivamente da Pi e OpenCode. Un modello poteva
	// quindi essere installato e perfettamente visibile in LM Studio, ma sparire
	// dal pannello finché non si modificavano a mano entrambi i JSON. Oltre a
	// essere scomodo, questo rendeva impossibile capire il guasto del modellino
	// locale: il file c'era, il runtime lo vedeva, theLAAP no.
	serviti := map[string]bool{}
	for _, rt := range discoverRuntimes() {
		for _, id := range rt.Modelli {
			k := rt.Chiave + "|" + id
			serviti[k] = true
			if _, presente := indice[k]; !presente {
				indice[k] = &Model{
					Runtime:   rt.Chiave,
					ID:        id,
					Nome:      id,
					Context:   131072,
					MaxTokens: 32768,
					Servito:   true,
				}
			}
		}
	}
	out := []Model{}
	for k, m := range indice {
		m.Servito = serviti[k] || provRemote(m.Runtime)
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runtime != out[j].Runtime {
			return out[i].Runtime < out[j].Runtime
		}
		return out[i].ID < out[j].ID
	})
	return out, errori
}

func backup(path string) error {
	if err := os.MkdirAll(BACKUP, 0o755); err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dst := filepath.Join(BACKUP, fmt.Sprintf("%s.%s.bak",
		filepath.Base(path), time.Now().Format("20060102-150405.000000000")))
	return os.WriteFile(dst, b, 0o644)
}

// writeConfig rigenera le sezioni "models" di entrambe le config dalla lista data,
// lasciando intatto tutto il resto (agent, compat, provider non gestiti).
func writeConfig(modelli []Model) error {
	pi, err := readJSON(filePi())
	if err != nil {
		return err
	}
	oc, err := readJSON(fileOC())
	if err != nil {
		return err
	}

	perRuntime := map[string][]Model{}
	for _, m := range modelli {
		perRuntime[m.Runtime] = append(perRuntime[m.Runtime], m)
	}

	// Le thinkingLevelMap che stanno GIÀ sul disco, indicizzate per provider|id.
	//
	// Serve perché MappaEffort è `json:"-"`: non esce verso il pannello e quindi
	// non rientra. Chi salva dal pannello manda sempre nil, e senza questo la
	// mappa su misura verrebbe sostituita da quella generica — che manda "high"
	// a un modello che accetta solo xhigh/medium/low e fa fallire ogni richiesta
	// con 400. È il danno del 15/08/2026, per una strada diversa da quella che il
	// commento su MappaEffort descrive.
	//
	// La regola: la mappa la conosce il modello, non questo codice. Se una c'era,
	// vince su qualunque cosa il chiamante non sappia.
	mappeSulDisco := map[string]map[string]any{}
	if provs, ok := pi["providers"].(map[string]any); ok {
		for chiave, pv := range provs {
			p, _ := pv.(map[string]any)
			ms, _ := p["models"].([]any)
			for _, mv := range ms {
				m, _ := mv.(map[string]any)
				id, _ := m["id"].(string)
				if tlm, ok := m["thinkingLevelMap"].(map[string]any); ok && id != "" {
					mappeSulDisco[chiave+"|"+id] = tlm
				}
			}
		}
	}

	// ---- Pi ----
	provs, _ := pi["providers"].(map[string]any)
	for chiave, pv := range provs {
		p, _ := pv.(map[string]any)
		lista := []any{}
		for _, m := range perRuntime[chiave] {
			if !m.InPi {
				continue
			}
			voce := map[string]any{
				"id": m.ID, "name": m.Nome, "reasoning": m.Reasoning,
				"input": []any{"text"}, "contextWindow": m.Context, "maxTokens": m.MaxTokens,
				"cost": map[string]any{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0},
			}
			if m.Reasoning {
				// Se una mappa c'era già, si tiene: i livelli ammessi li sa il modello,
				// non questo codice. Vedi il commento su MappaEffort.
				tlm := map[string]any{
					"minimal": nil, "low": "low", "medium": "medium",
					"high": "high", "xhigh": nil, "max": nil,
				}
				// Prima quella che porta il chiamante, poi quella già sul disco.
				// La generica solo se non esiste né l'una né l'altra.
				sorgente := m.MappaEffort
				if sorgente == nil {
					sorgente = mappeSulDisco[chiave+"|"+m.ID]
				}
				if sorgente != nil {
					tlm = map[string]any{}
					for k, v := range sorgente {
						tlm[k] = v
					}
				}
				// "off": null → Pi nasconde il livello, il thinking non si può spegnere
				if m.ThinkBloccato {
					tlm["off"] = nil
				}
				voce["thinkingLevelMap"] = tlm
			}
			lista = append(lista, voce)
		}
		p["models"] = lista
	}

	// ---- OpenCode ----
	provsOC, _ := oc["provider"].(map[string]any)
	for chiaveOC, pv := range provsOC {
		chiave := chiaveOC
		for _, rt := range configuredRuntimes() {
			if rt.ChiaveOC == chiaveOC {
				chiave = rt.Chiave
				break
			}
		}
		p, _ := pv.(map[string]any)
		ms := map[string]any{}
		for _, m := range perRuntime[chiave] {
			if !m.InOC {
				continue
			}
			ms[m.ID] = map[string]any{
				"name":  m.Nome,
				"limit": map[string]any{"context": m.Context, "output": m.MaxTokens},
			}
		}
		p["models"] = ms
	}

	// Prepara e valida entrambi i file prima di toccarne uno. Se la seconda
	// scrittura fallisce, il primo torna ai byte originali: Pi e OpenCode non
	// devono restare disallineati per un disco pieno o un permesso cambiato.
	type scrittura struct {
		path, nome     string
		nuovo, vecchio []byte
	}
	scritture := []scrittura{{path: filePi(), nome: "Pi"}, {path: fileOC(), nome: "OpenCode"}}
	dati := []map[string]any{pi, oc}
	for i := range scritture {
		b, err := json.MarshalIndent(dati[i], "", "  ")
		if err != nil {
			return err
		}
		var prova map[string]any
		if err := json.Unmarshal(b, &prova); err != nil {
			return fmt.Errorf("JSON generato non valido per %s", filepath.Base(scritture[i].path))
		}
		scritture[i].nuovo = append(b, '\n')
		scritture[i].vecchio, err = os.ReadFile(scritture[i].path)
		if err != nil {
			return err
		}
	}
	for _, s := range scritture {
		if err := backup(s.path); err != nil {
			return fmt.Errorf("backup %s: %w", filepath.Base(s.path), err)
		}
	}
	for i, s := range scritture {
		if err := writeAtomic(s.path, s.nuovo); err != nil {
			var rollback []string
			for j := 0; j < i; j++ {
				if e := writeAtomic(scritture[j].path, scritture[j].vecchio); e != nil {
					rollback = append(rollback, scritture[j].nome+": "+e.Error())
				}
			}
			if len(rollback) > 0 {
				return fmt.Errorf("scrittura %s fallita: %w; anche il rollback e' incompleto: %s",
					s.nome, err, strings.Join(rollback, "; "))
			}
			return fmt.Errorf("scrittura %s fallita: %w (l'altro client e' stato ripristinato)", s.nome, err)
		}
	}
	return nil
}

// apiRaw: modalità esperto — i due file di configurazione così come sono,
// leggibili e modificabili senza aprire un editor. In scrittura valida il JSON
// e fa la copia di sicurezza come per il resto.
func apiRaw(w http.ResponseWriter, r *http.Request) {
	quale := r.URL.Query().Get("file")
	path := filePi()
	if quale == "opencode" {
		path = fileOC()
	}
	if r.Method == http.MethodPost {
		var req struct {
			Contenuto string `json:"contenuto"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			errJSON(w, "corpo non valido")
			return
		}
		var prova map[string]any
		if err := json.Unmarshal([]byte(req.Contenuto), &prova); err != nil {
			errJSON(w, "il JSON non è valido: "+err.Error())
			return
		}
		if len(prova) == 0 {
			errJSON(w, "rifiuto di scrivere un file vuoto")
			return
		}
		if err := backup(path); err != nil {
			errJSON(w, "non riesco a fare la copia di sicurezza: "+err.Error())
			return
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(req.Contenuto), 0o644); err != nil {
			errJSON(w, err.Error())
			return
		}
		if err := os.Rename(tmp, path); err != nil {
			errJSON(w, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true,
			"messaggio": "salvato " + filepath.Base(path) + " (copia di sicurezza in " + BACKUP + ")"})
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	writeJSON(w, map[string]any{"file": path, "contenuto": string(b)})
}

func apiConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var modelli []Model
		if err := json.NewDecoder(r.Body).Decode(&modelli); err != nil {
			errJSON(w, "corpo non valido: "+err.Error())
			return
		}
		if err := writeConfig(modelli); err != nil {
			errJSON(w, err.Error())
			return
		}
		// I provider possono essere cambiati: la cache locale/remoto non vale più.
		forgetRemote()
		dopo, errori := configState()
		writeJSON(w, map[string]any{"ok": true, "modelli": dopo, "errori": errori,
			"messaggio": fmt.Sprintf("salvate %d voci in Pi e OpenCode (backup in %s)", len(modelli), BACKUP)})
		return
	}
	modelli, errori := configState()
	writeJSON(w, map[string]any{"modelli": modelli, "errori": errori})
}
