package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Chiedere ai provider cosa sanno servire.
//
// La lista dei modelli del pannello nasce dai file di Pi e OpenCode: mostra
// quello che TU hai già configurato. Per i runtime locali va bene, perché il
// pannello li interroga comunque. Per un provider remoto — un server aziendale
// — no: se lì compare un modello nuovo, o ne rinominano uno, il pannello non
// se ne accorge e tocca editare il JSON a mano.
//
// Qui si chiude il buco: si chiede al provider il suo /v1/models e si confronta
// con quello che hai in configurazione.
//
// REGOLA FERMA: baseUrl e chiave servono per fare la richiesta e non escono MAI
// dalla risposta, né nei log. Sono credenziali di terzi che stanno nei file dei
// client, e il pannello le usa senza mai mostrarle.

type ModelloOfferto struct {
	ID        string `json:"id"`
	Provider  string `json:"provider"`
	Configura bool   `json:"configurato"` // già presente nei tuoi client?
	Contesto  int    `json:"contesto,omitempty"`
}

type StatoProvider struct {
	Chiave string `json:"chiave"`
	Nome   string `json:"nome"`
	// Remoto: non gira su questa macchina. Per questi il disco non è tuo e i
	// comandi che parlano di file non hanno senso.
	Remoto   bool             `json:"remoto"`
	Risponde bool             `json:"risponde"`
	Errore   string           `json:"errore,omitempty"`
	Offerti  []ModelloOfferto `json:"offerti"`
}

// credenzialiProvider estrae il minimo per fare la chiamata. Il risultato non
// viene mai serializzato verso il client.
type credProvider struct {
	chiave  string
	nome    string
	baseURL string
	apiKey  string
	remoto  bool
}

// leggiProvider raccoglie i provider dai file dei client.
//
// Un provider è "remoto" quando il suo baseUrl non punta a questa macchina:
// è l'unico criterio affidabile, perché il nome non dice niente.
func leggiProvider() []credProvider {
	var out []credProvider
	visti := map[string]bool{}

	pi, err := leggiJSON(filePi())
	if err != nil {
		return out
	}
	provs, _ := pi["providers"].(map[string]any)
	for chiave, pv := range provs {
		p, _ := pv.(map[string]any)
		base, _ := p["baseUrl"].(string)
		if base == "" || visti[chiave] {
			continue
		}
		visti[chiave] = true
		key, _ := p["apiKey"].(string)
		nome, _ := p["name"].(string)
		if nome == "" {
			nome = chiave
		}
		out = append(out, credProvider{
			chiave: chiave, nome: nome, baseURL: strings.TrimRight(base, "/"),
			apiKey: key, remoto: !indirizzoDiQuestaMacchina(base),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].chiave < out[j].chiave })
	return out
}

// provRemoto: il provider gira su un'altra macchina?
//
// Serve in più punti (etichette automatiche, disponibilità, interfaccia) e la
// risposta cambia solo quando cambia la configurazione dei client, quindi si
// tiene in cache: leggere e riparsare due JSON a ogni scheda sarebbe assurdo.
var (
	cacheRemoto   map[string]bool
	cacheRemotoMu sync.RWMutex
)

func provRemoto(chiave string) bool {
	cacheRemotoMu.RLock()
	c := cacheRemoto
	cacheRemotoMu.RUnlock()
	if c == nil {
		c = map[string]bool{}
		for _, p := range leggiProvider() {
			c[p.chiave] = p.remoto
		}
		cacheRemotoMu.Lock()
		cacheRemoto = c
		cacheRemotoMu.Unlock()
	}
	return c[chiave]
}

// scordaRemoto invalida la cache dopo una scrittura delle configurazioni.
func scordaRemoto() {
	cacheRemotoMu.Lock()
	cacheRemoto = nil
	cacheRemotoMu.Unlock()
}

func indirizzoDiQuestaMacchina(u string) bool {
	l := strings.ToLower(u)
	for _, s := range []string{"//127.0.0.1", "//localhost", "//[::1]", "//0.0.0.0"} {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// interroga chiede /v1/models a un provider.
func interroga(c credProvider, configurati map[string]bool) StatoProvider {
	s := StatoProvider{Chiave: c.chiave, Nome: c.nome, Remoto: c.remoto, Offerti: []ModelloOfferto{}}

	url := c.baseURL + "/models"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		s.Errore = "indirizzo non valido"
		return s
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	// Tetto stretto: un provider aziendale irraggiungibile non deve tenere
	// la pagina in attesa.
	cl := &http.Client{Timeout: 8 * time.Second}
	rsp, err := cl.Do(req)
	if err != nil {
		// Il messaggio d'errore di Go contiene l'URL completo, che può avere
		// credenziali: si dice cos'è successo, non dove.
		s.Errore = "non risponde"
		return s
	}
	defer rsp.Body.Close()
	if rsp.StatusCode != http.StatusOK {
		s.Errore = fmt.Sprintf("ha risposto %d", rsp.StatusCode)
		if rsp.StatusCode == 401 || rsp.StatusCode == 403 {
			s.Errore += " (credenziale rifiutata)"
		}
		return s
	}
	var corpo struct {
		Data []struct {
			ID       string `json:"id"`
			Contesto int    `json:"context_length"`
			MaxLen   int    `json:"max_model_len"`
		} `json:"data"`
	}
	if json.NewDecoder(rsp.Body).Decode(&corpo) != nil {
		s.Errore = "risposta illeggibile"
		return s
	}
	s.Risponde = true
	for _, m := range corpo.Data {
		if m.ID == "" {
			continue
		}
		ctx := m.Contesto
		if ctx == 0 {
			ctx = m.MaxLen
		}
		s.Offerti = append(s.Offerti, ModelloOfferto{
			ID: m.ID, Provider: c.chiave, Contesto: ctx,
			Configura: configurati[c.chiave+"|"+m.ID],
		})
	}
	sort.Slice(s.Offerti, func(i, j int) bool { return s.Offerti[i].ID < s.Offerti[j].ID })
	return s
}

// apiProvider: cosa offre ciascun provider e cosa hai già in configurazione.
func apiProvider(w http.ResponseWriter, r *http.Request) {
	modelli, _ := statoConfig()
	configurati := map[string]bool{}
	for _, m := range modelli {
		configurati[m.Runtime+"|"+m.ID] = true
	}

	provs := leggiProvider()
	out := make([]StatoProvider, len(provs))
	var wg sync.WaitGroup
	for i, c := range provs {
		wg.Add(1)
		go func(i int, c credProvider) {
			defer wg.Done()
			defer func() {
				if rec := recover(); rec != nil {
					out[i] = StatoProvider{Chiave: c.chiave, Nome: c.nome,
						Remoto: c.remoto, Errore: "errore interno", Offerti: []ModelloOfferto{}}
				}
			}()
			out[i] = interroga(c, configurati)
		}(i, c)
	}
	wg.Wait()
	scriviJSON(w, out)
}

// destinazionePer: dove mandare una richiesta per un dato provider.
//
// Un runtime locale si raggiunge sulla sua porta; un provider remoto ha
// indirizzo e credenziale nei file dei client. Restituisce anche `trovato`
// falso quando il provider non è configurato, così il chiamante può ripiegare
// sulla porta locale invece di indovinare.
func destinazionePer(chiave string) (destinazione, bool) {
	for _, p := range leggiProvider() {
		if p.chiave == chiave {
			return destinazione{baseURL: p.baseURL, apiKey: p.apiKey}, true
		}
	}
	return destinazione{}, false
}
