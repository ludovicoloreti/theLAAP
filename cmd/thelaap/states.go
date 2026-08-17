package main

// states.go — la macchina a stati dei modelli, in un posto solo.
//
// Perché sta qui e non nella pagina: stato e classe li devono leggere il
// pannello, la voce nella barra dei menu e qualunque script. Calcolati nella
// pagina, ognuno se li rifà a modo suo e prima o poi divergono — è la stessa
// trappola del 16/08/2026, dove le voci del menu chiamavano id che nessuno
// serviva più e fallivano in silenzio.
//
// Qui non si esegue niente e non si legge niente dal sistema: si combinano
// schede(), memoriaCorrente(), budgetCorrente() e politicaCorrente(), che sono
// già le fonti di verità. Questo file non aggiunge dipendenze.
//
// Due assi, tenuti separati perché rispondono a due domande diverse:
//
//	STATO   com'è adesso        pronto · in-memoria · spento · in-arrivo · guasto · remoto
//	CLASSE  come può convivere  convivente · esclusivo · residente · remoto
//
// La classe NON è una soglia nuova: è la stessa SogliaGrandeByte che budget.go
// usa per la regola «un modello grande alla volta». Se cambia quella, cambiano
// insieme il verdetto del preflight e l'etichetta mostrata. Se restassero due
// numeri diversi, il pannello direbbe «convivente» su un modello che l'arbitro
// rifiuta.

import (
	"net/http"
	"sort"
	"strings"

	"github.com/ludovicoloreti/theLAAP/internal/budget"
)

// Stati e classi: le stringhe sono un contratto verso la pagina e verso Swift.
// Non tradurle qui: sono chiavi, non testo da leggere.
const (
	StatoPronto    = "pronto"
	StatoInMemoria = "in-memoria"
	StatoSpento    = "spento"
	StatoInArrivo  = "in-arrivo"
	StatoGuasto    = "guasto"
	StatoRemoto    = "remoto"

	ClasseConvivente = "convivente"
	ClasseEsclusivo  = "esclusivo"
	ClasseResidente  = "residente"
	ClasseRemoto     = "remoto"
)

// SchedaStato: una scheda più il giudizio. Il client non calcola niente.
type SchedaStato struct {
	Scheda
	Stato  string `json:"stato"`
	Classe string `json:"classe"`
	// Pronto: quello che risponde adesso senza attese. Ce n'è sempre al
	// massimo uno, e lo decide il server, non la pagina.
	Pronto bool `json:"pronto"`
	// Verdict se lo si carica adesso, dall'arbitro di budget.go. Solo per i
	// modelli non caricati: per quelli già in RAM la domanda non ha senso.
	Verdetto *budget.Verdict `json:"verdetto,omitempty"`
}

// RispostaModelli: tutto quello che serve a disegnare l'elenco in una richiesta
// sola. I numeri in testa sono la macchina, non i modelli: cambiando Mac
// cambiano da soli e il resto si comporta identico.
type RispostaModelli struct {
	TotaleGB   float64 `json:"totaleGB"`
	OccupatiGB float64 `json:"occupatiGB"`
	// LiberiGB è il resto aritmetico, totale meno occupati: è quello che la
	// barra disegna, e deve tornare a occhio.
	LiberiGB float64 `json:"liberiGB"`
	// DisponibiliGB è un'altra cosa: quanto resta per un modello NUOVO, tolta
	// la riserva del sistema operativo. È il numero su cui l'arbitro decide, e
	// va tenuto distinto da LiberiGB o il pannello promette spazio che non c'è.
	DisponibiliGB float64       `json:"disponibiliGB"`
	RiservaGB     float64       `json:"riservaGB"`
	SogliaGB      float64       `json:"sogliaGB"` // da qui in su, classe esclusivo
	TettoGB       float64       `json:"tettoGB"`  // tetto per un singolo modello, 0 = non dichiarato
	Modelli       []SchedaStato `json:"modelli"`
}

// classeDi: come questo modello può convivere con gli altri.
func classeDi(s Scheda, sogliaGB float64) string {
	if provRemoto(s.Runtime) {
		return ClasseRemoto
	}
	for _, rc := range cfg().Runtime {
		if rc.Chiave == s.Runtime && rc.ModelloResidente {
			return ClasseResidente
		}
	}
	if s.GB > 0 && s.GB >= sogliaGB {
		return ClasseEsclusivo
	}
	return ClasseConvivente
}

// statoDi: com'è adesso. L'ordine dei controlli è la priorità con cui vanno
// mostrati: un modello in scaricamento non è "spento", e uno che non risponde
// non è "in memoria" solo perché il programma è acceso.
func statoDi(s Scheda, m MemStato, inArrivo map[string]bool) string {
	if provRemoto(s.Runtime) {
		return StatoRemoto
	}
	if inArrivo[strings.ToLower(s.ID)] {
		return StatoInArrivo
	}
	if !s.Servito {
		return StatoGuasto
	}
	if caricato(s, m) {
		return StatoInMemoria // il pronto lo assegna dopo chi vede tutti
	}
	return StatoSpento
}

// caricato: il suo programma lo tiene in RAM adesso. I nomi arrivano da servizi
// di terzi e le maiuscole non combaciano mai: il confronto le ignora.
func caricato(s Scheda, m MemStato) bool {
	for _, c := range m.Caricati {
		if strings.EqualFold(c.Nome, s.ID) {
			return true
		}
	}
	return false
}

// scegliPronto: fra i modelli in memoria, quello che il pannello propone.
// Regola: uno solo, il più veloce misurato fra quelli che conversano. Un
// residente (l'embedding della ricerca) non è un candidato: sta in RAM per
// servire un'altra cosa e non risponde a domande. Se però in memoria ci sono
// solo residenti, meglio indicare quello che niente.
func scegliPronto(ss []SchedaStato) int {
	candidati := []int{}
	for i, s := range ss {
		if s.Stato == StatoInMemoria && s.Classe != ClasseResidente {
			candidati = append(candidati, i)
		}
	}
	if len(candidati) == 0 {
		for i, s := range ss {
			if s.Stato == StatoInMemoria {
				candidati = append(candidati, i)
			}
		}
	}
	if len(candidati) == 0 {
		return -1
	}
	sort.SliceStable(candidati, func(a, b int) bool {
		return ss[candidati[a]].TokS > ss[candidati[b]].TokS
	})
	return candidati[0]
}

// modelliConStato: la funzione che vale la pena chiamare da qualunque parte.
func modelliConStato() RispostaModelli {
	m := memoriaCorrente()
	p := politicaCorrente()
	b := budgetCorrente()
	sogliaGB := float64(p.LargeThresholdBytes) / 1e9

	inArrivo := map[string]bool{}
	for _, d := range scarichiInCorso() {
		inArrivo[strings.ToLower(d)] = true
	}

	// La barra disegna l'occupazione misurata sui processi, non i pesi dei
	// file: mtplx dichiara 29,3 GB e ne occupa 84,8 (vedi memory.go).
	var occupati float64
	for _, o := range m.Processi {
		occupati += float64(o.CurrentBytes) / 1e9
	}
	liberi := m.TotaleGB - occupati
	if liberi < 0 {
		liberi = 0
	}

	out := RispostaModelli{
		TotaleGB:      m.TotaleGB,
		OccupatiGB:    occupati,
		LiberiGB:      liberi,
		DisponibiliGB: float64(b.AvailableBytes()) / 1e9,
		RiservaGB:     riservaSistemaGB(),
		SogliaGB:      sogliaGB,
		TettoGB:       m.CeilingGB,
		Modelli:       make([]SchedaStato, 0, 8), // mai nil: il client la scorre sempre
	}

	for _, s := range schede() {
		x := SchedaStato{
			Scheda: s,
			Classe: classeDi(s, sogliaGB),
			Stato:  statoDi(s, m, inArrivo),
		}
		// «Se lo carico adesso, ci sta?» — la stessa risposta del preflight,
		// così la pagina non deve chiederla modello per modello, e non può
		// rispondere da sé una cosa diversa.
		if x.Stato == StatoSpento && s.GB > 0 {
			v := b.Admits(uint64(s.GB*1e9), p)
			x.Verdetto = &v
		}
		out.Modelli = append(out.Modelli, x)
	}
	if i := scegliPronto(out.Modelli); i >= 0 {
		out.Modelli[i].Pronto = true
		out.Modelli[i].Stato = StatoPronto
	}
	return out
}

func apiModelli(w http.ResponseWriter, r *http.Request) {
	scriviJSON(w, modelliConStato())
}

// ─────────────────────────────────────────────────────────────────────────
// Il registro dei comandi.
//
// Un elenco solo, letto dal pannello E dalla voce nella barra dei menu. Il
// difetto del 16/08/2026 era proprio questo: due elenchi scritti a mano in due
// programmi, con id che non combaciavano più (`laguna-on` contro
// `modello-grande-on`, `stoppa-tutto` contro `ferma-tutto`). Con una fonte sola
// quel difetto non si può riprodurre.
//
// Niente qui è scritto per nome: strumenti, regimi e programmi vengono tutti
// dalla configurazione. Su un'altra macchina cambia il file, non il programma.
// ─────────────────────────────────────────────────────────────────────────

// Comando: quello che il pannello mostra e quello che il server accetta.
type Comando struct {
	ID     string `json:"id"` // esattamente ciò che la rotta ammette
	Nome   string `json:"nome"`
	Gruppo string `json:"gruppo"` // machine · maintenance · regime · programs
	Cosa   string `json:"cosa,omitempty"`
	Durata string `json:"durata,omitempty"`
	// Rischio: chiede conferma prima di partire.
	Rischio bool `json:"rischio,omitempty"`
	// Rotta e corpo, per chi non è la pagina (la barra dei menu, uno script).
	Rotta string            `json:"rotta"`
	Corpo map[string]string `json:"corpo,omitempty"`
}

func comandi() []Comando {
	c := cfg()
	out := []Comando{}
	// I due comandi di macchina: esistono solo se la configurazione li dichiara.
	// comandoAmmesso conosce questi due id e nessun altro fuori dagli strumenti.
	if strings.TrimSpace(c.FermaTutto) != "" {
		out = append(out, Comando{
			ID: "ferma-tutto", Nome: "Ferma tutto", Gruppo: "machine", Rischio: true,
			Rotta: "/api/esegui", Corpo: map[string]string{"cmd": "ferma-tutto"},
		})
	}
	if strings.TrimSpace(c.RiaccendiTutto) != "" {
		out = append(out, Comando{
			ID: "riaccendi-tutto", Nome: "Riaccendi tutto", Gruppo: "machine",
			Rotta: "/api/esegui", Corpo: map[string]string{"cmd": "riaccendi-tutto"},
		})
	}
	// Gli strumenti dichiarati in configurazione: sono già una lista chiusa.
	for _, s := range c.Strumenti {
		out = append(out, Comando{
			ID: s.ID, Nome: s.Nome, Gruppo: "maintenance", Cosa: s.Cosa,
			Durata: s.Durata, Rischio: s.Rischio,
			Rotta: "/api/esegui", Corpo: map[string]string{"cmd": s.ID},
		})
	}
	// I regimi: il programma non ne conosce nessuno per nome, li dichiara la
	// configurazione. Entrano nel registro con la loro chiave.
	for _, g := range c.Regimi {
		out = append(out, Comando{
			ID: "regime:" + g.Chiave, Nome: g.Nome, Gruppo: "regime", Cosa: g.Cosa,
			Rotta: "/api/regime", Corpo: map[string]string{"chiave": g.Chiave, "azione": "on"},
		})
	}
	// I programmi riavviabili. Senza comando di riavvio non c'è niente da
	// offrire, e il pannello lo dice non mostrandolo.
	for _, rc := range c.Runtime {
		if strings.TrimSpace(rc.Riavvia) == "" &&
			(strings.TrimSpace(rc.Ferma) == "" || strings.TrimSpace(rc.Avvia) == "") {
			continue
		}
		out = append(out, Comando{
			ID: "riavvia:" + rc.Chiave, Nome: "Riavvia " + rc.Nome, Gruppo: "programs",
			Rotta: "/api/servizio", Corpo: map[string]string{"servizio": rc.Chiave, "azione": "restart"},
		})
	}
	return out
}

func apiComandi(w http.ResponseWriter, r *http.Request) {
	scriviJSON(w, comandi())
}
