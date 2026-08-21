// aipanel — pannello di controllo dello stack AI locale.
//
//	go build -o aipanel . && ./aipanel      → http://127.0.0.1:7070
//
// Legge lo stato reale dai runtime, non si fida di file di stato propri:
// la verità restano i due JSON di Pi e OpenCode, questo è solo un editor
// che non sbaglia la traduzione fra i due schemi.
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

//go:embed ui.html
var UI string

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, msg string) {
	errJSONStatus(w, http.StatusBadRequest, msg)
}

func errJSONStatus(w http.ResponseWriter, stato int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(stato)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "errore": msg})
}

func trunc(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func sprint(v any) string { return fmt.Sprint(v) }

// La guardia delle rotte sta in security.go: localhost per tutto, piu'
// Origin/Referer e token per ciò che muta stato.

// rotte: la tabella delle rotte, una sola.
//
// Sta qui e non nei test perche' era copiata a mano la': due elenchi da tenere
// allineati, e i test finivano per verificare un instradamento diverso da
// quello spedito. Ora sbagliare una protezione qui fa fallire i test.
func rotte(pagina string) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", guardia(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// La pagina contiene il token: che non finisca in nessuna cache.
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprint(w, pagina)
	}))
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/svg+xml")
		fmt.Fprint(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32">`+
			`<text y="26" font-size="26">🎛</text></svg>`)
	})
	mux.HandleFunc("/api/runtime", guardia(apiRuntime))
	mux.HandleFunc("/api/memoria", guardia(apiMemory))
	mux.HandleFunc("/api/config", guardia(apiConfig))
	mux.HandleFunc("/api/prova", guardia(postOnly(apiProbe)))
	mux.HandleFunc("/api/attiva", guardia(postOnly(apiActivate)))
	// Eseguono comandi o riscrivono le schede: da GET erano raggiungibili con
	// un <img src=...> da qualunque pagina web.
	mux.HandleFunc("/api/esegui", guardia(postOnly(apiRun)))
	mux.HandleFunc("/api/servizio", guardia(postOnly(apiService)))
	mux.HandleFunc("/api/spiega", guardia(postOnly(apiExplain)))
	mux.HandleFunc("/api/domande", guardia(apiQuestions))
	// Chi è il modellino che risponde: il pannello lo nomina invece di cablarlo.
	mux.HandleFunc("/api/aiuto", guardia(apiHelper))
	mux.HandleFunc("/api/configurazione", guardia(apiSetup))
	mux.HandleFunc("/api/strumenti", guardia(func(w http.ResponseWriter, r *http.Request) {
		// Mai nil: uscirebbe "null" e la pagina cade appena prova a scorrerlo.
		if s := cfg().Strumenti; s != nil {
			writeJSON(w, s)
		} else {
			writeJSON(w, []ToolCfg{})
		}
	}))
	// Governo della memoria: la domanda «ci sta?» prima di caricare, e lo
	// scarico del singolo modello dove il programma sa farlo.
	mux.HandleFunc("/api/capacita", guardia(apiCapability))
	mux.HandleFunc("/api/regimi", guardia(apiRegimes))
	mux.HandleFunc("/api/regime", guardia(postOnly(apiRegime)))
	mux.HandleFunc("/api/preflight", guardia(postOnly(apiPreflight)))
	mux.HandleFunc("/api/modello/libera-memoria", guardia(postOnly(apiUnloadModel)))
	// Cosa sanno servire i provider, compresi quelli remoti: senza questo il
	// pannello mostra solo ciò che hai già scritto a mano nei client.
	mux.HandleFunc("/api/provider", guardia(apiProvider))
	// Cambiare la chiave di un provider: si legge se c'è, non quale.
	mux.HandleFunc("/api/credenziali", guardia(apiCredentials))
	mux.HandleFunc("/api/credenziale", guardia(postOnly(apiSetCredential)))
	// Togliere un modello dal disco: prima si guarda chi ci dipende. La prima
	// azione lo archivia; la cancellazione definitiva accetta solo voci gia'
	// isolate nel deposito.
	mux.HandleFunc("/api/modello/esamina", guardia(apiExamineModel))
	mux.HandleFunc("/api/modello/rimuovi", guardia(postOnly(apiRemoveModel)))
	mux.HandleFunc("/api/modelli/archivio", guardia(apiModelArchive))
	mux.HandleFunc("/api/modello/ripristina", guardia(postOnly(apiRestoreModel)))
	mux.HandleFunc("/api/modello/elimina", guardia(postOnly(apiDeleteArchived)))
	mux.HandleFunc("/api/schede", guardia(apiCards))
	// Stato, classe e verdetto calcolati dove stanno già i numeri (states.go).
	// La pagina li legge, non li rifà: con due calcoli separati il pannello
	// finirebbe per dire «convivente» su un modello che l'arbitro rifiuta.
	mux.HandleFunc("/api/modelli", guardia(apiModels))
	// Un registro solo di comandi, letto dal pannello e dalla barra dei menu.
	mux.HandleFunc("/api/comandi", guardia(apiCommands))
	mux.HandleFunc("/api/etichetta", guardia(postOnly(apiLabel)))
	mux.HandleFunc("/api/etichetta-auto", guardia(postOnly(apiAutoLabel)))
	// Il tema del Mac letto dal Mac: un browser aperto in modalità applicazione
	// riporta `prefers-color-scheme: light` anche col sistema in scuro.
	mux.HandleFunc("/api/tema", guardia(func(w http.ResponseWriter, r *http.Request) {
		scuro := strings.TrimSpace(sh("defaults read -g AppleInterfaceStyle 2>/dev/null")) == "Dark"
		writeJSON(w, map[string]any{"scuro": scuro})
	}))
	mux.HandleFunc("/api/documenti", guardia(restrictedRead(apiDocuments)))
	mux.HandleFunc("/api/documento", guardia(restrictedRead(apiDocument)))
	mux.HandleFunc("/api/grezzo", guardia(restrictedRead(apiRaw)))
	mux.HandleFunc("/api/hf/cerca", guardia(apiHFSearch))
	mux.HandleFunc("/api/modello/installa", guardia(postOnly(apiHFDownload)))
	mux.HandleFunc("/api/hf/stato", guardia(apiHFStatus))
	return mux
}

func main() {
	porta := flag.Int("porta", 0, "porta di ascolto (0 = quella della configurazione)")
	flag.Parse()

	loadConfig()    // per prima: tutto il resto ne dipende
	generateToken() // subito dopo: serve per costruire la pagina
	loadProfiles()
	startMemoryMonitor() // la prima fotografia costa ~4s: falla ora, non alla prima richiesta

	// Il token finisce nella pagina una volta sola, all'avvio: il corpo è
	// costante e non c'è motivo di ricomporlo a ogni richiesta.
	pagina := strings.Replace(UI, "__TOKEN__", sessionToken, 1)

	mux := rotte(pagina)

	p := *porta
	if p == 0 {
		p = cfg().Porta
	}
	if p == 0 {
		p = 7070
	}
	listeningPort = p // la guardia confronta l'Origin con questa, non con la configurazione
	addr := fmt.Sprintf("127.0.0.1:%d", p)
	log.Printf("aipanel → http://%s", addr)
	log.Printf("config: %s", PI_CFG)
	log.Printf("        %s", OC_CFG)
	log.Fatal(http.ListenAndServe(addr, mux))
}
