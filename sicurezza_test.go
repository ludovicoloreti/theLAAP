package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOriginAmmesso(t *testing.T) {
	casi := []struct {
		nome    string
		origine string
		porta   int
		vuole   bool
	}{
		{"origin del pannello", "http://127.0.0.1:7070", 7070, true},
		{"localhost per nome", "http://localhost:7070", 7070, true},
		{"referer con percorso", "http://127.0.0.1:7070/", 7070, true},
		{"porta diversa", "http://127.0.0.1:9999", 7070, false},
		{"sito esterno", "https://esempio.invalid", 7070, false},
		{"sito che finge localhost", "https://127.0.0.1.esempio.invalid", 7070, false},
		{"vuoto", "", 7070, false},
		{"spazzatura", "::::", 7070, false},
		// Il caso che conta: una pagina qualunque che prova a comandare il
		// pannello. L'Origin è il suo, non il nostro.
		{"pagina ostile", "http://sito-cattivo.invalid", 7070, false},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			if got := originAmmesso(c.origine, c.porta); got != c.vuole {
				t.Errorf("originAmmesso(%q, %d) = %v, volevo %v", c.origine, c.porta, got, c.vuole)
			}
		})
	}
}

func TestIndirizzoLocale(t *testing.T) {
	casi := map[string]bool{
		"127.0.0.1:54321": true,
		"[::1]:54321":     true,
		"192.168.1.20:80": false,
		"10.0.0.5:1234":   false,
	}
	for remoto, vuole := range casi {
		if got := indirizzoLocale(remoto); got != vuole {
			t.Errorf("indirizzoLocale(%q) = %v, volevo %v", remoto, got, vuole)
		}
	}
}

// chiamata prepara una richiesta gia' passata per la guardia.
func richiestaGuardata(t *testing.T, metodo, origine, token string) *httptest.ResponseRecorder {
	t.Helper()
	raggiunto := false
	h := guardia(func(w http.ResponseWriter, r *http.Request) {
		raggiunto = true
		w.WriteHeader(http.StatusOK)
	})
	r := httptest.NewRequest(metodo, "/api/servizio", strings.NewReader("{}"))
	r.RemoteAddr = "127.0.0.1:54321"
	if origine != "" {
		r.Header.Set("Origin", origine)
	}
	if token != "" {
		r.Header.Set("X-theLAAP-Token", token)
	}
	w := httptest.NewRecorder()
	h(w, r)
	if raggiunto && w.Code != http.StatusOK {
		t.Fatal("incoerenza: handler raggiunto ma codice diverso da 200")
	}
	return w
}

func TestGuardiaBloccaCSRF(t *testing.T) {
	tokenSessione = "token-di-prova"
	portaInAscolto = 7070

	t.Run("POST da sito esterno con token indovinato", func(t *testing.T) {
		w := richiestaGuardata(t, http.MethodPost, "http://sito-cattivo.invalid", "token-di-prova")
		if w.Code != http.StatusForbidden {
			t.Errorf("una pagina esterna è passata: codice %d", w.Code)
		}
	})

	t.Run("POST senza Origin (form cross-site)", func(t *testing.T) {
		w := richiestaGuardata(t, http.MethodPost, "", "token-di-prova")
		if w.Code != http.StatusForbidden {
			t.Errorf("richiesta senza Origin è passata: codice %d", w.Code)
		}
	})

	t.Run("POST con Origin giusto ma senza token", func(t *testing.T) {
		w := richiestaGuardata(t, http.MethodPost, "http://127.0.0.1:7070", "")
		if w.Code != http.StatusForbidden {
			t.Errorf("richiesta senza token è passata: codice %d", w.Code)
		}
	})

	t.Run("POST con token sbagliato", func(t *testing.T) {
		w := richiestaGuardata(t, http.MethodPost, "http://127.0.0.1:7070", "altro")
		if w.Code != http.StatusForbidden {
			t.Errorf("token sbagliato è passato: codice %d", w.Code)
		}
	})

	t.Run("POST legittimo dal pannello", func(t *testing.T) {
		w := richiestaGuardata(t, http.MethodPost, "http://127.0.0.1:7070", "token-di-prova")
		if w.Code != http.StatusOK {
			t.Errorf("richiesta legittima rifiutata: codice %d, corpo %q", w.Code, w.Body.String())
		}
	})

	t.Run("GET in lettura passa senza token", func(t *testing.T) {
		w := richiestaGuardata(t, http.MethodGet, "", "")
		if w.Code != http.StatusOK {
			t.Errorf("lettura rifiutata: codice %d", w.Code)
		}
	})

	t.Run("richiesta dalla rete respinta", func(t *testing.T) {
		h := guardia(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
		r := httptest.NewRequest(http.MethodGet, "/api/memoria", nil)
		r.RemoteAddr = "192.168.1.50:1234"
		w := httptest.NewRecorder()
		h(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("richiesta dalla rete è passata: codice %d", w.Code)
		}
	})
}

func TestSoloPost(t *testing.T) {
	h := soloPost(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	// Il caso concreto: <img src="/api/esegui?cmd=ferma-tutto"> da una pagina
	// qualunque. Prima bastava questo per spegnere lo stack.
	r := httptest.NewRequest(http.MethodGet, "/api/esegui?cmd=ferma-tutto", nil)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET su rotta che esegue comandi: codice %d, volevo 405", w.Code)
	}

	r = httptest.NewRequest(http.MethodPost, "/api/esegui", strings.NewReader("{}"))
	w = httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("POST rifiutata: codice %d", w.Code)
	}
}

func TestGeneraTokenDiversoOgniVolta(t *testing.T) {
	generaToken()
	a := tokenSessione
	generaToken()
	if a == tokenSessione {
		t.Error("due token consecutivi identici")
	}
	if len(a) != 64 {
		t.Errorf("token lungo %d caratteri, volevo 64", len(a))
	}
}
