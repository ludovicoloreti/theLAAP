package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Test end-to-end del server: monta le rotte come fa main() e le interroga.
//
// Mancava, e le rotte finora le avevo provate solo a mano con curl — cioè non
// le riprovava nessuno. Qui si verifica quello che una prova manuale non
// ripete mai: che ogni rotta risponda, che quelle che cambiano qualcosa siano
// protette, e soprattutto che NESSUNA risposta si porti dietro una credenziale.

// Le rotte sono quelle vere: rotteDiProva() sta in main.go. La copia che stava qui era
// un secondo elenco da tenere allineato, e ha smesso di somigliare a quello
// spedito appena una rotta ha cambiato protezione.
func rotteDiProva() *http.ServeMux { return rotte("<pagina di prova>") }

func chiedi(t *testing.T, m *http.ServeMux, metodo, percorso string, token bool) *httptest.ResponseRecorder {
	t.Helper()
	var corpo *strings.Reader
	if metodo == http.MethodPost {
		corpo = strings.NewReader("{}")
	} else {
		corpo = strings.NewReader("")
	}
	r := httptest.NewRequest(metodo, percorso, corpo)
	r.RemoteAddr = "127.0.0.1:5555"
	// httptest mette Host: example.com, che la guardia rifiuta.
	r.Host = "127.0.0.1:7070"
	if token {
		r.Header.Set("Origin", "http://127.0.0.1:7070")
		r.Header.Set("X-theLAAP-Token", tokenSessione)
	}
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	return w
}

func TestRotteInLetturaRispondonoJSONValido(t *testing.T) {
	tokenSessione = "prova-token"
	portaInAscolto = 7070
	m := rotteDiProva()

	for _, p := range []string{
		"/api/runtime", "/api/memoria", "/api/capacita", "/api/regimi",
		"/api/credenziali", "/api/strumenti", "/api/modello/esamina?id=inesistente",
		"/api/modelli/archivio",
	} {
		t.Run(p, func(t *testing.T) {
			w := chiedi(t, m, http.MethodGet, p, false)
			if w.Code != http.StatusOK {
				t.Fatalf("codice %d", w.Code)
			}
			var v any
			if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
				t.Fatalf("risposta non è JSON: %v", err)
			}
			// Una lista nil esce come "null" e fa cadere la pagina appena
			// prova a scorrerla: è già successo due volte.
			if strings.TrimSpace(w.Body.String()) == "null" {
				t.Error("ha risposto null: le liste vanno inizializzate")
			}
		})
	}
}

func TestRotteMutantiProtette(t *testing.T) {
	tokenSessione = "prova-token"
	portaInAscolto = 7070
	m := rotteDiProva()

	for _, p := range []string{"/api/modello/rimuovi", "/api/modello/ripristina",
		"/api/modello/elimina", "/api/credenziale", "/api/regime"} {
		t.Run(p+" senza token", func(t *testing.T) {
			if w := chiedi(t, m, http.MethodPost, p, false); w.Code != http.StatusForbidden {
				t.Errorf("passata senza token: codice %d", w.Code)
			}
		})
		t.Run(p+" in GET", func(t *testing.T) {
			w := chiedi(t, m, http.MethodGet, p, true)
			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("GET accettata su rotta che muta stato: codice %d", w.Code)
			}
		})
	}

	t.Run("editor in scrittura senza token", func(t *testing.T) {
		if w := chiedi(t, m, http.MethodPost, "/api/documento?id=thelaap", false); w.Code != http.StatusForbidden {
			t.Errorf("editor passato senza token: codice %d", w.Code)
		}
	})
}

// Il test che conta di più: nessuna risposta deve contenere una credenziale.
// Il pannello legge chiavi API dai file dei client per interrogare i provider,
// e basta una struct serializzata con disattenzione per rimandarle al browser.
func TestNessunaRispostaContieneCredenziali(t *testing.T) {
	tokenSessione = "prova-token"
	portaInAscolto = 7070
	m := rotteDiProva()

	// Le chiavi vere di questa macchina, lette dai file dei client.
	var segreti []string
	for _, p := range leggiProvider() {
		if len(p.apiKey) >= 8 {
			segreti = append(segreti, p.apiKey)
		}
	}
	if len(segreti) == 0 {
		t.Skip("nessuna credenziale configurata: niente da verificare")
	}

	for _, p := range []string{
		"/api/runtime", "/api/memoria", "/api/capacita", "/api/regimi",
		"/api/credenziali", "/api/strumenti", "/api/documenti", "/api/modelli/archivio",
	} {
		corpo := chiedi(t, m, http.MethodGet, p, false).Body.String()
		for _, s := range segreti {
			if strings.Contains(corpo, s) {
				t.Errorf("%s rimanda una credenziale nella risposta", p)
			}
		}
		// Anche i frammenti lunghi: una chiave troncata resta pericolosa.
		for _, s := range segreti {
			if len(s) > 16 && strings.Contains(corpo, s[:16]) {
				t.Errorf("%s rimanda l'inizio di una credenziale", p)
			}
		}
	}
}

func TestGuardiaRifiutaDallaRete(t *testing.T) {
	m := rotteDiProva()
	r := httptest.NewRequest(http.MethodGet, "/api/memoria", nil)
	r.RemoteAddr = "192.168.1.99:1234"
	w := httptest.NewRecorder()
	m.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("richiesta dalla rete accettata: codice %d", w.Code)
	}
}

// Le tre rotte che restituiscono i file dei client chiedono il token anche in
// lettura: e' la' che il pannello scrive la chiave dei provider (scriviChiave).
// Restavano aperte a chiunque riuscisse a farsi passare per same-origin.
func TestLeRotteDiConfigurazioneNonSiLeggonoSenzaToken(t *testing.T) {
	tokenSessione = "prova-token"
	portaInAscolto = 7070
	m := rotteDiProva()

	for _, p := range []string{"/api/documenti", "/api/documento?id=thelaap", "/api/grezzo?file=pi"} {
		t.Run(p, func(t *testing.T) {
			if w := chiedi(t, m, http.MethodGet, p, false); w.Code != http.StatusForbidden {
				t.Errorf("letta senza token: codice %d", w.Code)
			}
			// E col token deve funzionare, o l'editor delle configurazioni resta
			// chiuso fuori.
			if w := chiedi(t, m, http.MethodGet, p, true); w.Code == http.StatusForbidden {
				t.Errorf("col token giusto risponde ancora 403: %s", w.Body.String())
			}
		})
	}
}
