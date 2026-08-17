package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Il pannello può fermare servizi ed eseguire comandi. Controllare solo che la
// richiesta arrivi da 127.0.0.1 NON basta: quando è il browser dell'utente a
// mandarla, l'indirizzo di partenza è sempre 127.0.0.1 anche se a chiederlo è
// stato un sito qualunque aperto in un'altra scheda. Senza questo file una
// pagina web poteva spegnere lo stack con un <img src=...>.
//
// Quattro difese, indipendenti fra loro:
//  1. solo da localhost           — chiude l'accesso dalla rete
//  2. Host combaciante            — chiude il DNS rebinding
//  3. Origin/Referer combaciante  — chiude le richieste partite da altri siti
//  4. token per sessione          — copre i casi in cui il browser non manda Origin
//
// Le letture generiche restano libere, così la pagina si carica anche col token
// scaduto. Quelle che restituiscono file di configurazione no: vedi
// restrictedRead.

var sessionToken string

// La porta su cui stiamo davvero ascoltando. Non coincide con quella della
// configurazione quando si usa il flag -porta, e l'Origin va confrontato con
// quella vera: altrimenti il pannello rifiuta le proprie stesse richieste.
var listeningPort int

// generateToken: nuovo a ogni avvio. Se un token trapela, basta riavviare.
func generateToken() {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand che fallisce è un guasto grave del sistema: meglio non
		// partire che partire senza difesa.
		panic("impossibile generare il token di sessione: " + err.Error())
	}
	sessionToken = hex.EncodeToString(b)
}

// allowedOrigin: l'indirizzo da cui la pagina dice di arrivare deve essere
// questo stesso pannello. Confronta host e porta, non la stringa intera, così
// 127.0.0.1 e localhost sono entrambi validi.
func allowedOrigin(grezzo string, porta int) bool {
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

// allowedHost: l'intestazione Host deve nominare questo pannello.
//
// Senza questo controllo il pannello è aperto al DNS rebinding. Un dominio
// dell'attaccante che risolve a 127.0.0.1 rende la sua pagina same-origin col
// pannello, e da lì il browser le lascia leggere le risposte: fra queste ci sono
// i file di configurazione dei client, che è dove il pannello scrive le chiavi
// dei provider. Guardare l'indirizzo di partenza non serve a niente, perché è
// il browser dell'utente a fare la richiesta e resta 127.0.0.1.
//
// Con SplitHostPort e non a confronto di stringhe: [::1]:7070 romperebbe il
// confronto ingenuo.
func allowedHost(grezzo string, porta int) bool {
	if grezzo == "" {
		return false
	}
	host, p, err := net.SplitHostPort(grezzo)
	if err != nil {
		// Host senza porta vuol dire porta 80, che non è mai la nostra.
		return false
	}
	host = strings.Trim(host, "[]")
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return false
	}
	return p == strconv.Itoa(porta)
}

// effectivePort: quella su cui ascoltiamo davvero, con i ripieghi in ordine.
func effectivePort() int {
	if listeningPort != 0 {
		return listeningPort
	}
	if p := cfg().Porta; p != 0 {
		return p
	}
	return 7070
}

// localAddress: vero se la connessione arriva dalla macchina stessa.
func localAddress(remoto string) bool {
	host := remoto
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	host = strings.Trim(host, "[]")
	return host == "127.0.0.1" || host == "::1" || host == "localhost"
}

// guardia avvolge ogni rotta delle API.
//
// Localhost e Host valgono per tutti i metodi. Le richieste in sola lettura
// (GET, HEAD) si fermano lì; tutto ciò che muta stato deve in più presentare un
// Origin o Referer di questo pannello e il token di sessione.
func guardia(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !localAddress(r.RemoteAddr) {
			http.Error(w, "solo da localhost", http.StatusForbidden)
			return
		}
		porta := effectivePort()
		// Anche in lettura: il rebinding attacca proprio le GET.
		if !allowedHost(r.Host, porta) {
			http.Error(w, "host non riconosciuto", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next(w, r)
			return
		}

		// Origin è quello che manda un fetch; Referer copre i form.
		sorgente := r.Header.Get("Origin")
		if sorgente == "" {
			sorgente = r.Header.Get("Referer")
		}
		if !allowedOrigin(sorgente, porta) {
			http.Error(w, "richiesta non partita dal pannello", http.StatusForbidden)
			return
		}

		// Confronto a tempo costante: non serve a molto su localhost, ma
		// costa nulla e evita di doverci ripensare.
		dato := r.Header.Get("X-theLAAP-Token")
		if subtle.ConstantTimeCompare([]byte(dato), []byte(sessionToken)) != 1 {
			http.Error(w, "token mancante o non valido", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// restrictedRead: il token anche in lettura.
//
// /api/grezzo e /api/documento restituiscono i file dei client tali e quali, e
// lì dentro sta la chiave dei provider (writeKey). /api/documenti ne
// espone i percorsi assoluti. Il controllo dell'Host basta a fermare il
// rebinding; questo è la seconda serratura, per il giorno in cui una pagina
// riuscisse comunque a farsi passare per same-origin.
//
// La pagina stessa non passa da qui: senza il token nessuno potrebbe caricarla,
// ed è la pagina a portarlo.
func restrictedRead(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dato := r.Header.Get("X-theLAAP-Token")
		if subtle.ConstantTimeCompare([]byte(dato), []byte(sessionToken)) != 1 {
			http.Error(w, "token mancante o non valido", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// postOnly rifiuta i metodi diversi da POST. Serve per le rotte che eseguono
// comandi o modificano le schede: da GET erano raggiungibili con un semplice
// <img src=...> da qualsiasi pagina web.
func postOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "serve POST", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	}
}
