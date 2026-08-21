package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Runtime = un server di inferenza locale.
type Runtime struct {
	Chiave   string   `json:"chiave"`   // come lo chiama il primo client
	ChiaveOC string   `json:"chiaveOC"` // come lo chiama il secondo (spesso uguale)
	Nome     string   `json:"nome"`
	Cosa     string   `json:"cosa,omitempty"` // a cosa serve, in parole semplici
	Porta    int      `json:"porta"`
	Elenco   string   `json:"-"` // percorso che elenca i modelli
	Attivo   bool     `json:"attivo"`
	Modelli  []string `json:"modelli"`
	// Tiene il modello sempre caricato: l'interfaccia lo dice invece di
	// ricavarlo dal nome del programma.
	ModelloResidente bool `json:"modelloResidente"`
	// Che cosa si può davvero fare a questo programma. Sono i comandi
	// dichiarati in configurazione, ridotti a tre sì/no: le righe di shell non
	// escono verso il browser, e l'interfaccia mostra solo i pulsanti che
	// funzionano invece di offrirne uno che non fa niente.
	PuoAvviare   bool `json:"puoAvviare"`
	PuoFermare   bool `json:"puoFermare"`
	PuoRiavviare bool `json:"puoRiavviare"`
}

// I runtime non sono più scritti qui: vengono dalla configurazione, che
// descrive la macchina su cui il pannello sta girando.
func configuredRuntimes() []Runtime {
	out := []Runtime{}
	for _, r := range knownRuntimes() {
		chiaveOC := r.ChiaveOC
		if chiaveOC == "" {
			chiaveOC = r.Chiave
		}
		elenco := r.Elenco
		if elenco == "" {
			elenco = "/v1/models"
		}
		// Le tre capacità le decide serviceCommand, la stessa funzione che poi
		// esegue: così il pannello non può offrire un pulsante che il server
		// rifiuta. Le righe di shell non escono verso il browser.
		out = append(out, Runtime{Chiave: r.Chiave, ChiaveOC: chiaveOC,
			Nome: r.Nome, Cosa: r.Cosa, Porta: r.Porta, Elenco: elenco,
			ModelloResidente: r.ModelloResidente,
			PuoAvviare:       serviceCommand(r, "start") != "",
			PuoFermare:       serviceCommand(r, "stop") != "",
			PuoRiavviare:     serviceCommand(r, "restart") != ""})
	}
	return out
}

// httpGet: nil quando la richiesta non è andata a buon fine.
//
// Il codice di stato va guardato. Senza, un 401 o un 404 tornano come risposta
// valida — è il loro corpo d'errore in JSON — e il chiamante prova a leggerci
// dentro: un repo chiuso finiva fra i risultati di ricerca con dimensione 0, e
// un servizio che risponde 401 risultava acceso e funzionante.
func httpGet(url string, timeout time.Duration) []byte {
	cl := &http.Client{Timeout: timeout}
	resp, err := cl.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}
	return b
}

// discoverRuntimes interroga i server in parallelo e dice cosa serve DAVVERO ognuno.
func discoverRuntimes() []Runtime {
	out := configuredRuntimes()
	if out == nil {
		out = []Runtime{}
	}
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := &out[i]
			var url string
			if r.Porta == 11434 {
				url = "http://127.0.0.1:11434/api/tags"
			} else {
				url = "http://127.0.0.1:" + itoa(r.Porta) + "/v1/models"
			}
			// timeout corto: sono server sulla stessa macchina, se non rispondono
			// in 2 secondi sono bloccati — e il pannello non deve restare fermo con loro
			b := httpGet(url, 2*time.Second)
			if b == nil {
				return
			}
			r.Attivo = true
			if strings.Contains(r.Elenco, "/api/tags") { // formato Ollama
				var v struct {
					Models []struct {
						Name string `json:"name"`
					} `json:"models"`
				}
				if json.Unmarshal(b, &v) == nil {
					for _, m := range v.Models {
						r.Modelli = append(r.Modelli, m.Name)
					}
				}
				return
			}
			var v struct {
				Data []struct {
					ID            string `json:"id"`
					MaxModelLen   int    `json:"max_model_len"`
					ContextLength int    `json:"context_length"`
				} `json:"data"`
			}
			if json.Unmarshal(b, &v) == nil {
				for _, m := range v.Data {
					r.Modelli = append(r.Modelli, m.ID)
				}
			}
		}(i)
	}
	wg.Wait()
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func apiRuntime(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, discoverRuntimes())
}
