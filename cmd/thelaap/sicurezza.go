package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Il pannello può fermare servizi ed eseguire comandi. Controllare solo che la
// richiesta arrivi da 127.0.0.1 NON basta: quando è il browser dell'utente a
// mandarla, l'indirizzo di partenza è sempre 127.0.0.1 anche se a chiederlo è
// stato un sito qualunque aperto in un'altra scheda. Senza questo file una
// pagina web poteva spegnere lo stack con un <img src=...>.
//
// Tre difese, indipendenti fra loro:
//  1. solo da localhost           — chiude l'accesso dalla rete
//  2. Origin/Referer combaciante  — chiude le richieste partite da altri siti
//  3. token per sessione          — copre i casi in cui il browser non manda Origin
//
// Le letture restano libere: non cambiano niente e tenerle semplici significa
// che la pagina si carica anche se il token è scaduto.

var tokenSessione string

// La porta su cui stiamo davvero ascoltando. Non coincide con quella della
// configurazione quando si usa il flag -porta, e l'Origin va confrontato con
// quella vera: altrimenti il pannello rifiuta le proprie stesse richieste.
var portaInAscolto int

// generaToken: nuovo a ogni avvio. Se un token trapela, basta riavviare.
func generaToken() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand che fallisce è un guasto grave del sistema: meglio non
		// partire che partire senza difesa.
		panic("impossibile generare il token di sessione: " + err.Error())
	}
	tokenSessione = hex.EncodeToString(b)
}

// originAmmesso: l'indirizzo da cui la pagina dice di arrivare deve essere
// questo stesso pannello. Confronta host e porta, non la stringa intera, così
// 127.0.0.1 e localhost sono entrambi validi.
func originAmmesso(grezzo string, porta int) bool {
	if grezzo == "" {
		return false
	}
	u, err := url.Parse(grezzo)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	// Referer completo di percorso: la porta è quella che conta.
	if p := u.Port(); p != "" && p != fmt.Sprint(porta) {
		return false
	}
	return true
}

// indirizzoLocale: vero se la connessione arriva dalla macchina stessa.
func indirizzoLocale(remoto string) bool {
	host := remoto
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// guardia avvolge ogni rotta delle API.
//
// Le richieste in sola lettura (GET, HEAD) passano col solo controllo di
// localhost. Tutto ciò che muta stato deve in più presentare un Origin o
// Referer di questo pannello e il token di sessione.
func guardia(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !indirizzoLocale(r.RemoteAddr) {
			http.Error(w, "solo da localhost", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next(w, r)
			return
		}

		porta := portaInAscolto
		if porta == 0 {
			porta = cfg().Porta
		}
		if porta == 0 {
			porta = 7070
		}
		// Origin è quello che manda un fetch; Referer copre i form.
		sorgente := r.Header.Get("Origin")
		if sorgente == "" {
			sorgente = r.Header.Get("Referer")
		}
		if !originAmmesso(sorgente, porta) {
			http.Error(w, "richiesta non partita dal pannello", http.StatusForbidden)
			return
		}

		// Confronto a tempo costante: non serve a molto su localhost, ma
		// costa nulla e evita di doverci ripensare.
		dato := r.Header.Get("X-theLAAP-Token")
		if subtle.ConstantTimeCompare([]byte(dato), []byte(tokenSessione)) != 1 {
			http.Error(w, "token mancante o non valido", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// soloPost rifiuta i metodi diversi da POST. Serve per le rotte che eseguono
// comandi o modificano le schede: da GET erano raggiungibili con un semplice
// <img src=...> da qualsiasi pagina web.
func soloPost(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "serve POST", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
