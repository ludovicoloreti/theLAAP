package main

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// adoption.go — i motori che girano qui ma nessuno ha dichiarato.
//
// Il pannello aveva due elenchi di runtime che non si parlavano: i modelli
// venivano dai file dei client, i runtime dalla configurazione. Un motore
// presente solo nel primo — ds4, sulla porta 8090 — esisteva come modello e non
// esisteva come programma. Il 18/08/2026 questo si vedeva così: il modello che
// l'utente stava usando in quel momento risultava «guasto», di classe
// «convivente» su 81 GB, e la sua memoria non compariva in nessun totale.
//
// La correzione non è aggiungere ds4 da qualche parte: è togliere il secondo
// elenco. knownRuntimes() è ora l'unica risposta alla domanda «quali motori
// girano su questa macchina», e mette insieme le due fonti con una regola
// sola — la configurazione vince, il resto si adotta.
//
// Cosa si adotta: un provider dei client il cui baseUrl punta a questa
// macchina. È lo stesso criterio già usato da provRemote() per decidere se un
// provider è remoto, non un secondo giudizio che potrebbe divergere.
//
// Cosa NON si adotta e perché:
//
//	un provider remoto      non gira qui: nessuna porta da interrogare, nessun
//	                        comando locale che lo governi. Adottarlo riempirebbe
//	                        «programmi» di righe inerti.
//	un provider senza porta un baseUrl senza porta esplicita non dice dove
//	                        bussare, e indovinarla è come non saperla.
//
// Un runtime adottato non ha comandi: il pannello non sa avviarlo né fermarlo,
// e lo dice non mostrando quei pulsanti — è la stessa regola di
// configuredRuntimes(). Quello che guadagna è tutto il resto: viene
// interrogato, quindi i suoi modelli non sono più «guasti»; viene misurato,
// quindi la sua memoria entra nei totali; ha una porta, quindi «Provalo»
// funziona.

// localPort: la porta di un baseUrl che punta a questa macchina.
// Zero quando l'indirizzo è di un'altra macchina o non dichiara una porta.
func localPort(base string) int {
	if !addressOfThisMachine(base) {
		return 0
	}
	u, err := url.Parse(base)
	if err != nil {
		return 0
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil || p <= 0 || p > 65535 {
		return 0
	}
	return p
}

// adoptedRuntimes: i provider locali dei client che la configurazione non
// dichiara, resi runtime a tutti gli effetti.
func adoptedRuntimes(dichiarati []RuntimeCfg) []RuntimeCfg {
	gia := map[string]bool{}
	porte := map[int]bool{}
	for _, r := range dichiarati {
		gia[r.Chiave] = true
		if r.Porta > 0 {
			porte[r.Porta] = true
		}
	}

	var out []RuntimeCfg
	for _, p := range readProviders() {
		if gia[p.chiave] {
			continue
		}
		porta := localPort(p.baseURL)
		if porta == 0 {
			continue
		}
		// Stessa porta di un runtime già dichiarato: è lo stesso programma
		// chiamato con due nomi dai due client. Adottarlo significherebbe
		// contarne la memoria due volte.
		if porte[porta] {
			continue
		}
		porte[porta] = true
		out = append(out, RuntimeCfg{
			Chiave: p.chiave, ChiaveOC: p.chiave, Nome: p.nome, Porta: porta,
			Elenco: "/v1/models",
			Cosa:   "dichiarato nei tuoi client, non in configurazione",
			// Nessun comando: il pannello non sa governarlo, e i pulsanti
			// «avvia» e «ferma» non compariranno. Nessuna nota di scarico
			// diversa dal solito: non sappiamo niente di questo programma.
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Chiave < out[j].Chiave })
	return out
}

// knownRuntimes: TUTTI i motori locali, dichiarati e adottati.
//
// Questa è la funzione da chiamare. cfg().Runtime da solo risponde a una
// domanda più stretta — «quali motori ho scritto in configurazione» — e usarlo
// dove serviva questa è il difetto che ha reso ds4 invisibile.
func knownRuntimes() []RuntimeCfg {
	dichiarati := cfg().Runtime
	adottati := adoptedRuntimes(dichiarati)
	if len(adottati) == 0 {
		return dichiarati
	}
	out := make([]RuntimeCfg, 0, len(dichiarati)+len(adottati))
	out = append(out, dichiarati...)
	out = append(out, adottati...)
	return out
}

// runtimeByKey: la voce con questa chiave, o nil. Cerca fra tutti i motori
// noti, non solo fra i dichiarati: un pulsante su un runtime adottato deve
// trovare la sua voce, altrimenti risponde «non conosco il programma».
func runtimeByKey(chiave string) *RuntimeCfg {
	for _, r := range knownRuntimes() {
		if r.Chiave == chiave {
			c := r
			return &c
		}
	}
	return nil
}

// adopted: questo motore è stato adottato invece che dichiarato? Serve
// all'interfaccia per spiegare perché non ha i pulsanti di avvio e arresto.
func adopted(chiave string) bool {
	for _, r := range cfg().Runtime {
		if r.Chiave == chiave {
			return false
		}
	}
	return runtimeByKey(chiave) != nil
}

// stoppedRuntimes: i motori noti che adesso non rispondono, per chiave.
//
// Serve a distinguere «spento» da «guasto»: un modello che il suo programma non
// serve perché il programma è spento non è rotto, e il rimedio è diverso.
func stoppedRuntimes() map[string]bool {
	fuori := map[string]bool{}
	for _, r := range discoverRuntimes() {
		if !r.Attivo {
			fuori[strings.ToLower(r.Chiave)] = true
		}
	}
	return fuori
}
