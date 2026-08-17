package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cambiare la chiave di un provider.
//
// Il pannello la leggeva per fare le richieste ma non aveva modo di
// modificarla. Quando una chiave viene revocata — e prima o poi succede — il
// file resta con quella vecchia, ogni richiesta torna 401, e l'unica strada è
// aprire il JSON a mano. È capitato il 29/07/2026 ed è per questo che esiste
// questo file.
//
// REGOLE, non negoziabili:
//   - la chiave attuale non si mostra MAI, nemmeno all'utente che l'ha scritta:
//     si dice se c'è e quanto è lunga, e si mostrano gli ultimi caratteri per
//     riconoscerla. Basta per capire "è quella nuova o la vecchia?", e non
//     basta per rubarla da uno schermo ripreso o da una registrazione.
//   - non finisce nei log, nei messaggi d'errore, né in una risposta.
//   - si scrive prima su file temporaneo e poi si rinomina, con copia di
//     sicurezza: un pannello che corrompe models.json ti lascia senza client.

type StatoCredenziale struct {
	Provider  string `json:"provider"`
	Nome      string `json:"nome"`
	Impostata bool   `json:"impostata"`
	Lunghezza int    `json:"lunghezza,omitempty"`
	// Ultimi caratteri, per riconoscere quale chiave c'è senza rivelarla.
	Coda   string `json:"coda,omitempty"`
	Remoto bool   `json:"remoto"`
	// Esito dell'ultima verifica, se richiesta.
	Verifica string `json:"verifica,omitempty"`
}

func mascheraCoda(k string) string {
	if len(k) < 6 {
		return ""
	}
	return "…" + k[len(k)-4:]
}

func apiCredenziali(w http.ResponseWriter, r *http.Request) {
	out := []StatoCredenziale{}
	for _, p := range leggiProvider() {
		s := StatoCredenziale{Provider: p.chiave, Nome: p.nome, Remoto: p.remoto}
		if p.apiKey != "" {
			s.Impostata, s.Lunghezza, s.Coda = true, len(p.apiKey), mascheraCoda(p.apiKey)
		}
		out = append(out, s)
	}
	scriviJSON(w, out)
}

// scriviChiave aggiorna la chiave nel file di un client.
//
// Modifica solo il campo della chiave e riscrive il resto identico: il file è
// dell'utente e può contenere di tutto, non è il pannello a decidere cosa
// merita di sopravvivere.
func scriviChiave(percorso, provider, chiave string, formato string) error {
	percorso = espandiHome(percorso)
	b, err := os.ReadFile(percorso)
	if err != nil {
		return err
	}
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		return fmt.Errorf("%s non è JSON valido, non lo tocco", filepath.Base(percorso))
	}

	radice := "providers"
	if formato == "opencode" {
		radice = "provider"
	}
	provs, _ := d[radice].(map[string]any)
	if provs == nil {
		return fmt.Errorf("nessun provider in %s", filepath.Base(percorso))
	}
	p, _ := provs[provider].(map[string]any)
	if p == nil {
		return fmt.Errorf("provider «%s» assente in %s", provider, filepath.Base(percorso))
	}
	if formato == "opencode" {
		o, _ := p["options"].(map[string]any)
		if o == nil {
			o = map[string]any{}
			p["options"] = o
		}
		o["apiKey"] = chiave
	} else {
		p["apiKey"] = chiave
	}

	nuovo, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	// Copia di sicurezza, poi scrittura atomica: mai un file mezzo scritto.
	_ = os.WriteFile(fmt.Sprintf("%s.bak-%d", percorso, time.Now().Unix()), b, 0o600)
	tmp := percorso + ".tmp"
	if err := os.WriteFile(tmp, append(nuovo, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, percorso)
}

func apiImpostaCredenziale(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Provider string `json:"provider"`
		Chiave   string `json:"chiave"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile")
		return
	}
	req.Chiave = strings.TrimSpace(req.Chiave)
	if req.Provider == "" || req.Chiave == "" {
		errJSON(w, "servono il provider e la chiave")
		return
	}
	// Una chiave con a capo o caratteri di controllo è quasi sempre un
	// incollaggio andato storto: meglio dirlo che scrivere spazzatura.
	if strings.ContainsAny(req.Chiave, "\n\r\t\x00") {
		errJSON(w, "la chiave contiene caratteri strani: probabilmente è stata incollata male")
		return
	}

	var scritti, errori []string
	for _, cl := range cfg().Clienti {
		if err := scriviChiave(cl.File, req.Provider, req.Chiave, cl.Formato); err != nil {
			errori = append(errori, cl.Nome+": "+err.Error())
			continue
		}
		scritti = append(scritti, cl.Nome)
	}
	if len(scritti) == 0 {
		errJSON(w, "non sono riuscito a scriverla — "+strings.Join(errori, "; "))
		return
	}
	scordaRemoto() // i provider vanno riletti

	// Verifica subito: dire "salvato" senza sapere se funziona è metà del
	// problema di partenza.
	esito := "non verificata"
	for _, p := range leggiProvider() {
		if p.chiave != req.Provider {
			continue
		}
		if b := httpGetAut(p.baseURL+"/models", p.apiKey, 15*time.Second); b != nil {
			esito = "il server la accetta"
		} else {
			esito = "salvata, ma il server non risponde o la rifiuta"
		}
	}
	scriviJSON(w, map[string]any{
		"ok": true, "scritti": scritti, "errori": errori, "verifica": esito,
	})
}

// httpGetAut: come httpGet ma con credenziale. Sta qui e non in discovery.go
// per non far girare una chiave dentro una funzione di uso generale.
func httpGetAut(url, chiave string, timeout time.Duration) []byte {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	if chiave != "" {
		req.Header.Set("Authorization", "Bearer "+chiave)
	}
	cl := &http.Client{Timeout: timeout}
	rsp, err := cl.Do(req)
	if err != nil {
		return nil
	}
	defer rsp.Body.Close()
	if rsp.StatusCode < 200 || rsp.StatusCode > 299 {
		return nil
	}
	b := make([]byte, 0, 4096)
	buf := make([]byte, 4096)
	for {
		n, err := rsp.Body.Read(buf)
		b = append(b, buf[:n]...)
		if err != nil || len(b) > 1<<20 {
			break
		}
	}
	return b
}
