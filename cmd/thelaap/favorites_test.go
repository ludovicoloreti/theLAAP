package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// I preferiti stanno in profili.json insieme al resto di ciò che si sa di un
// modello. Ogni prova usa un file suo, vuoto, e rimette a posto quello vero.
func profiliDiProva(t *testing.T) {
	t.Helper()
	vecchio := PROFILI
	PROFILI = filepath.Join(t.TempDir(), "profili.json")
	profiliMu.Lock()
	vecchi := profili
	profili = map[string]*Profile{}
	profiliMu.Unlock()
	t.Cleanup(func() { PROFILI = vecchio; profiliMu.Lock(); profili = vecchi; profiliMu.Unlock() })
}

// rileggiProfili: come dopo un riavvio del pannello, resta solo ciò che è su disco.
func rileggiProfili() {
	profiliMu.Lock()
	profili = map[string]*Profile{}
	profiliMu.Unlock()
	loadProfiles()
}

func quantiProfili() int {
	profiliMu.RLock()
	defer profiliMu.RUnlock()
	return len(profili)
}

// segnaDaPagina: la richiesta che fa la pagina, con il token e l'origine giusti.
func segnaDaPagina(t *testing.T, corpo string) *httptest.ResponseRecorder {
	t.Helper()
	sessionToken, listeningPort = "prova-token", 7070
	r := httptest.NewRequest(http.MethodPost, "/api/preferito", strings.NewReader(corpo))
	r.RemoteAddr = "127.0.0.1:5555"
	r.Host = "127.0.0.1:7070"
	r.Header.Set("Origin", "http://127.0.0.1:7070")
	r.Header.Set("X-theLAAP-Token", sessionToken)
	w := httptest.NewRecorder()
	testRoutes().ServeHTTP(w, r)
	return w
}

// Il preferito lo sceglie chi usa il pannello, dal dettaglio del modello: deve
// esserci ancora dopo un riavvio.
func TestUnModelloSegnatoPreferitoLoEAncoraDopoUnRiavvio(t *testing.T) {
	profiliDiProva(t)
	if w := segnaDaPagina(t, `{"runtime":"omlx","id":"gemma","preferito":true}`); w.Code != http.StatusOK {
		t.Fatalf("codice %d: %s", w.Code, w.Body)
	}
	if p := readProfile("omlx", "gemma"); p == nil || !p.Preferito {
		t.Fatalf("preferito non segnato: %+v", p)
	}
	rileggiProfili()
	if p := readProfile("omlx", "gemma"); p == nil || !p.Preferito {
		t.Fatalf("preferito perso dopo il riavvio: %+v", p)
	}
}

func TestUnPreferitoSiPuoTogliere(t *testing.T) {
	profiliDiProva(t)
	segnaDaPagina(t, `{"runtime":"omlx","id":"gemma","preferito":true}`)
	if w := segnaDaPagina(t, `{"runtime":"omlx","id":"gemma","preferito":false}`); w.Code != http.StatusOK {
		t.Fatalf("codice %d: %s", w.Code, w.Body)
	}
	rileggiProfili()
	if p := readProfile("omlx", "gemma"); p != nil && p.Preferito {
		t.Fatalf("è rimasto tra i preferiti: %+v", p)
	}
}

// La risposta dice com'è rimasto il modello: la pagina disegna quello, non ciò
// che sperava di aver ottenuto.
func TestLaRispostaDiceSeIlModelloEPreferito(t *testing.T) {
	profiliDiProva(t)
	for _, voluto := range []bool{true, false} {
		corpo, _ := json.Marshal(map[string]any{"runtime": "omlx", "id": "gemma", "preferito": voluto})
		var risposta struct {
			OK        bool `json:"ok"`
			Preferito bool `json:"preferito"`
		}
		w := segnaDaPagina(t, string(corpo))
		if err := json.Unmarshal(w.Body.Bytes(), &risposta); err != nil || !risposta.OK || risposta.Preferito != voluto {
			t.Fatalf("chiesto %v, risposta %s (%v)", voluto, w.Body, err)
		}
	}
}

// Segnare un preferito non cancella ciò che del modello si sa già.
func TestSegnareUnPreferitoNonToccaMisureENome(t *testing.T) {
	profiliDiProva(t)
	updateProfile("omlx", "gemma", func(p *Profile) { p.TokS, p.Etichetta = 14.7, "Il mio Gemma" })
	segnaDaPagina(t, `{"runtime":"omlx","id":"gemma","preferito":true}`)
	p := readProfile("omlx", "gemma")
	if p == nil || !p.Preferito || p.TokS != 14.7 || p.Etichetta != "Il mio Gemma" {
		t.Fatalf("profilo rovinato: %+v", p)
	}
}

// Senza dire di quale modello si parla non nasce un profilo fantasma.
func TestUnPreferitoSenzaModelloVieneRifiutato(t *testing.T) {
	profiliDiProva(t)
	for _, corpo := range []string{`{"preferito":true}`, `{"runtime":"omlx","preferito":true}`,
		`{"id":"gemma","preferito":true}`, `{"runtime":" ","id":" ","preferito":true}`, `non è json`} {
		if w := segnaDaPagina(t, corpo); w.Code != http.StatusBadRequest {
			t.Errorf("%s: codice %d invece di 400", corpo, w.Code)
		}
	}
	if n := quantiProfili(); n != 0 {
		t.Fatalf("sono nati %d profili fantasma", n)
	}
}

// Cambia qualcosa su disco: vale la stessa protezione delle altre rotte che mutano.
func TestLaRottaDeiPreferitiEProtetta(t *testing.T) {
	sessionToken, listeningPort = "prova-token", 7070
	m := testRoutes()
	if w := chiedi(t, m, http.MethodPost, "/api/preferito", false); w.Code != http.StatusForbidden {
		t.Errorf("passata senza token: codice %d", w.Code)
	}
	if w := chiedi(t, m, http.MethodGet, "/api/preferito", true); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET accettata su una rotta che cambia stato: codice %d", w.Code)
	}
}

// La pagina riceve il preferito insieme al resto della scheda del modello.
func TestIlPreferitoArrivaAllaPaginaConLaScheda(t *testing.T) {
	profiliDiProva(t)
	piPath := filepath.Join(t.TempDir(), "models.json")
	if os.WriteFile(piPath, []byte(`{"providers":{"x":{"models":[{"id":"amato"},{"id":"altro"}]}}}`), 0o644) != nil {
		t.Fatal("non riesco a scrivere il file del client")
	}
	withConfig(t, Config{
		Runtime: []RuntimeCfg{{Chiave: "x", ChiaveOC: "x", Nome: "X", Porta: 8000}},
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"}},
	})
	segnaDaPagina(t, `{"runtime":"x","id":"amato","preferito":true}`)

	per := map[string]Card{}
	for _, s := range schede() {
		per[s.ID] = s
	}
	if _, c := per["amato"]; !c {
		t.Fatalf("il modello di prova non è tra le schede: %+v", per)
	}
	if !per["amato"].Preferito || per["altro"].Preferito {
		t.Fatalf("preferito sbagliato: amato=%v altro=%v", per["amato"].Preferito, per["altro"].Preferito)
	}
	if b, _ := json.Marshal(per["amato"]); !strings.Contains(string(b), `"preferito":true`) {
		t.Fatalf("la pagina non riceve il campo: %s", b)
	}
}

// ── la pagina ────────────────────────────────────────────────────────────

// La scheda «Preferiti» c'è sempre, anche vuota: è lì che si scopre che i
// preferiti esistono, e quando è vuota dice come se ne aggiunge uno.
func TestLaTabellaHaLaSchedaDeiPreferiti(t *testing.T) {
	tabella := corpoFunzione(t, "vistaModelli", "mostraTip")
	for _, atteso := range []string{"preferiti:[T().favorites", "m=>!!m.preferito", "T().noFavorites"} {
		if !strings.Contains(tabella, atteso) {
			t.Errorf("vistaModelli: manca %q", atteso)
		}
	}
	mustContain(t, "testi dei preferiti", `favorites:"Preferiti"`, `favorites:"Favorites"`, `noFavorites:"`)
}

// Si aggiunge e si toglie dal dettaglio del modello, con un pulsante che dice
// in che stato è (anche a chi non vede la stella).
func TestIlDettaglioHaIlPulsanteDeiPreferiti(t *testing.T) {
	dettaglio := corpoFunzione(t, "inspModello", "formNomi")
	for _, atteso := range []string{"cambiaPreferito('+arg(m.id)+')", "aria-pressed", "T().favAdd", "T().favRemove"} {
		if !strings.Contains(dettaglio, atteso) {
			t.Errorf("inspModello: manca %q", atteso)
		}
	}
	gesto := corpoFunzione(t, "cambiaPreferito", "inspModello")
	for _, atteso := range []string{"'/api/preferito'", "preferito:!m.preferito", "m.preferito=!!r.preferito"} {
		if !strings.Contains(gesto, atteso) {
			t.Errorf("cambiaPreferito: manca %q", atteso)
		}
	}
}

// Nella tabella un preferito si riconosce senza aprirlo.
func TestLaRigaSegnaIPreferiti(t *testing.T) {
	riga := corpoFunzione(t, "rigaModello", "vistaModelli")
	if !strings.Contains(riga, "m.preferito") || !strings.Contains(riga, "T().favorite") {
		t.Error("la riga non segna i preferiti")
	}
}

// La pagina richiede lo stato ogni cinque secondi. Una risposta partita prima
// del clic non deve riportare indietro la stella appena cambiata: sembrerebbe
// che il clic non abbia fatto niente.
func TestUnaRispostaPartitaPrimaDelClicNonRiportaIndietroLaStella(t *testing.T) {
	if giro := corpoFunzione(t, "aggiorna", "caricaLento"); !strings.Contains(giro, "S.prefLocali") {
		t.Error("aggiorna sovrascrive il preferito appena cambiato")
	}
	if gesto := corpoFunzione(t, "cambiaPreferito", "inspModello"); !strings.Contains(gesto, "S.prefLocali[m.id]=") {
		t.Error("cambiaPreferito non ricorda la scelta appena fatta")
	}
}

// Se il server non salva, la pagina lo dice nel dettaglio invece di tacere.
func TestUnPreferitoNonSalvatoSiVede(t *testing.T) {
	if gesto := corpoFunzione(t, "cambiaPreferito", "inspModello"); !strings.Contains(gesto, "S.esito[m.id]={ok:false") {
		t.Error("un errore nel salvare il preferito non viene mostrato")
	}
}
