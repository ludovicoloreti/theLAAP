package main

// lifecycle_test.go — accendere e spegnere un modello, col suo programma.
//
// Niente qui tocca i programmi veri della macchina: il «programma» è un server
// di prova che risponde solo se esiste un file-bandierina, e i suoi comandi di
// avvio e arresto creano e tolgono quel file. Così si prova il comportamento
// vero — comandi eseguiti, porta interrogata, modello scaldato — senza che un
// test possa spegnere il modello con cui qualcuno sta lavorando.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeProgram struct {
	t      *testing.T
	dir    string
	flag   string // esiste = il programma è acceso
	table  string // la tabella che stampa il comando «modelliCaricati»
	port   int
	mu     sync.Mutex
	status []string // i modelli caricati, per chi li dichiara su /v1/models/status
	warmed []string // i modelli che hanno ricevuto una richiesta
}

func newFakeProgram(t *testing.T) *fakeProgram {
	t.Helper()
	p := &fakeProgram{t: t, dir: t.TempDir()}
	p.flag = filepath.Join(p.dir, "acceso")
	p.table = filepath.Join(p.dir, "caricati")
	if err := os.WriteFile(p.table, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(p.flag); err != nil {
			http.Error(w, "spento", http.StatusServiceUnavailable)
			return
		}
		switch {
		case r.URL.Path == "/v1/models":
			w.Write([]byte(`{"data":[{"id":"gemma"},{"id":"qwen"}]}`))
		case r.URL.Path == "/v1/models/status":
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.status == nil {
				http.NotFound(w, r)
				return
			}
			type voce struct {
				ID     string  `json:"id"`
				Loaded bool    `json:"loaded"`
				Size   float64 `json:"actual_size"`
			}
			out := struct {
				Models []voce `json:"models"`
			}{Models: []voce{}}
			for _, id := range p.status {
				out.Models = append(out.Models, voce{ID: id, Loaded: true, Size: 5e9})
			}
			json.NewEncoder(w).Encode(out)
		case r.URL.Path == "/v1/chat/completions":
			var req struct {
				Model string `json:"model"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			p.mu.Lock()
			p.warmed = append(p.warmed, req.Model)
			p.mu.Unlock()
			if req.Model == "sconosciuto" {
				w.Write([]byte(`{"error":{"message":"model not found"}}`))
				return
			}
			w.Write([]byte(`{"choices":[{"message":{"content":"OK"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	p.port, _ = strconv.Atoi(u.Port())
	return p
}

func (p *fakeProgram) on() bool {
	_, err := os.Stat(p.flag)
	return err == nil
}

func (p *fakeProgram) turnOnByHand() {
	p.t.Helper()
	if err := os.WriteFile(p.flag, nil, 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *fakeProgram) load(modelli ...string) {
	p.t.Helper()
	var righe []string
	for _, m := range modelli {
		righe = append(righe, m+" idle 5.00 GB")
	}
	if err := os.WriteFile(p.table, []byte(strings.Join(righe, "\n")+"\n"), 0o644); err != nil {
		p.t.Fatal(err)
	}
}

func (p *fakeProgram) warmedUp() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string{}, p.warmed...)
}

// unloadsOne: un programma che sa togliere un solo modello dalla memoria e
// dice con un comando cosa ha caricato — come LM Studio e Ollama.
func (p *fakeProgram) unloadsOne() RuntimeCfg {
	rc := p.stopOnly()
	rc.Caricati = "cat " + shQuote(p.table)
	rc.ScaricaModello = "sh -c 'grep -v -- \"$1\" \"$0\" > \"$0.tmp\"; mv \"$0.tmp\" \"$0\"' " +
		shQuote(p.table) + " {modello}"
	return rc
}

// stopOnly: un programma che si può solo accendere e fermare — come oMLX e MTPLX.
func (p *fakeProgram) stopOnly() RuntimeCfg {
	return RuntimeCfg{Chiave: "finto", Nome: "Finto", Porta: p.port, Elenco: "/v1/models",
		Avvia: "touch " + shQuote(p.flag), Ferma: "rm -f " + shQuote(p.flag)}
}

func fastStart(t *testing.T) {
	t.Helper()
	vecchioLimite, vecchioPasso, vecchiaPausa := runtimeStartTimeout, runtimeStartPoll, commandRetryPause
	runtimeStartTimeout, runtimeStartPoll, commandRetryPause = 400*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		runtimeStartTimeout, runtimeStartPoll, commandRetryPause = vecchioLimite, vecchioPasso, vecchiaPausa
	})
}

// ── accendere ───────────────────────────────────────────────────────────────

func TestAccendereUnModelloAvviaIlProgrammaSpento(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()

	r, err := turnOn(rc, localDestination(p.port), "gemma")
	if err != nil {
		t.Fatalf("non acceso: %v", err)
	}
	if !p.on() {
		t.Fatal("il programma è rimasto spento: il comando di avvio non è partito")
	}
	if !r.ProgrammaAvviato {
		t.Error("la risposta non dice che ha avviato il programma")
	}
	if got := p.warmedUp(); len(got) != 1 || got[0] != "gemma" {
		t.Errorf("il modello non è stato caricato: richieste arrivate %v", got)
	}
}

func TestAccendereConIlProgrammaGiaAccesoNonLoRiavvia(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	p.turnOnByHand()
	rc := p.unloadsOne()
	spia := filepath.Join(p.dir, "avviato-di-nuovo")
	rc.Avvia = "touch " + shQuote(spia)

	r, err := turnOn(rc, localDestination(p.port), "gemma")
	if err != nil {
		t.Fatalf("non acceso: %v", err)
	}
	if _, err := os.Stat(spia); err == nil {
		t.Fatal("ha rieseguito il comando di avvio su un programma già acceso")
	}
	if r.ProgrammaAvviato {
		t.Error("dice di aver avviato un programma che era già acceso")
	}
}

func TestAccendereSenzaComandoDiAvvioNonMandaRichieste(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()
	rc.Avvia = ""

	if _, err := turnOn(rc, localDestination(p.port), "gemma"); err == nil {
		t.Fatal("un programma spento e senza comando di avvio non può accendere niente")
	}
	if got := p.warmedUp(); len(got) != 0 {
		t.Errorf("ha mandato richieste a un programma spento: %v", got)
	}
}

func TestProgrammaCheNonSiAlzaDaErroreInvecheDiAspettarePerSempre(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()
	rc.Avvia = "true" // il comando riesce, ma la porta non risponde mai

	inizio := time.Now()
	_, err := turnOn(rc, localDestination(p.port), "gemma")
	if err == nil {
		t.Fatal("ha dichiarato acceso un programma che non risponde")
	}
	if time.Since(inizio) > 5*time.Second {
		t.Errorf("ha aspettato %s: il tetto di attesa non vale", time.Since(inizio))
	}
	if got := p.warmedUp(); len(got) != 0 {
		t.Errorf("ha mandato richieste a un programma che non si è alzato: %v", got)
	}
}

func TestAvvioFallitoRiportaIlMessaggioDelComando(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()
	rc.Avvia = "echo 'porta occupata'; exit 3"

	_, err := turnOn(rc, localDestination(p.port), "gemma")
	if err == nil || !strings.Contains(err.Error(), "porta occupata") {
		t.Fatalf("il motivo scritto dal comando è andato perso: %v", err)
	}
}

// Il controllore dello stack ha un lock non bloccante: se sta già facendo
// altro rifiuta subito. Un rifiuto immediato si riprova, invece di restituire
// all'utente un errore che fra tre secondi non sarebbe più vero.
func TestUnRifiutoImmediatoDelComandoSiRiprova(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()
	primo := filepath.Join(p.dir, "primo-tentativo")
	rc.Avvia = "if [ -f " + shQuote(primo) + " ]; then touch " + shQuote(p.flag) +
		"; else touch " + shQuote(primo) + `; echo '{"ok": false, "error": "Un'"'"'altra operazione sullo stack è già in corso"}'; exit 1; fi`

	if _, err := turnOn(rc, localDestination(p.port), "gemma"); err != nil {
		t.Fatalf("al secondo tentativo sarebbe partito: %v", err)
	}
	if !p.on() {
		t.Fatal("il programma è rimasto spento")
	}
}

// Il programma è stato acceso solo per questo modello: se il modello non
// parte, non deve restare acceso a vuoto.
func TestSeIlModelloNonParteIlProgrammaAppenaAvviatoSiRispegne(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()

	if _, err := turnOn(rc, localDestination(p.port), "sconosciuto"); err == nil {
		t.Fatal("il programma ha rifiutato il modello: andava riportato")
	}
	if p.on() {
		t.Fatal("il programma è rimasto acceso senza nessun modello")
	}
}

// Ma se era già acceso per conto suo, un modello che non parte non è un
// motivo per spegnerlo.
func TestSeIlModelloNonParteUnProgrammaGiaAccesoRestaAcceso(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	p.turnOnByHand()
	rc := p.unloadsOne()

	if _, err := turnOn(rc, localDestination(p.port), "sconosciuto"); err == nil {
		t.Fatal("il programma ha rifiutato il modello: andava riportato")
	}
	if !p.on() {
		t.Fatal("ha spento un programma che non aveva acceso lui")
	}
}

// ── spegnere ────────────────────────────────────────────────────────────────

func TestSpegnereLUltimoModelloSpegneAncheIlProgramma(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.load("gemma")
	rc := p.unloadsOne()

	r, err := turnOff(rc, "gemma", nil)
	if err != nil {
		t.Fatalf("non spento: %v", err)
	}
	if b, _ := os.ReadFile(p.table); strings.Contains(string(b), "gemma") {
		t.Error("il modello è ancora in memoria")
	}
	if p.on() {
		t.Fatal("era l'unico modello: il programma doveva spegnersi")
	}
	if !r.Scaricato || !r.ProgrammaSpento {
		t.Errorf("la risposta non racconta cosa è successo: %+v", r)
	}
}

func TestSpegnereUnModelloLasciaAccesoIlProgrammaSeNeRestanoAltri(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.load("gemma", "qwen")
	rc := p.unloadsOne()

	r, err := turnOff(rc, "gemma", nil)
	if err != nil {
		t.Fatalf("non spento: %v", err)
	}
	if !p.on() {
		t.Fatal("ha spento il programma con un altro modello ancora dentro")
	}
	if r.ProgrammaSpento || len(r.Restano) != 1 || r.Restano[0] != "qwen" {
		t.Errorf("atteso programma acceso con qwen dentro, ottenuto %+v", r)
	}
}

func TestProgrammaCheNonScaricaIlSingoloSiFermaSeHaSoloQuelModello(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.status = []string{"gemma"}
	rc := p.stopOnly()

	r, err := turnOff(rc, "gemma", nil)
	if err != nil {
		t.Fatalf("non spento: %v", err)
	}
	if p.on() || !r.ProgrammaSpento {
		t.Fatalf("doveva fermare il programma: acceso=%v risposta=%+v", p.on(), r)
	}
}

func TestProgrammaCheNonScaricaIlSingoloNonSiFermaSeHaAltriModelli(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.status = []string{"gemma", "qwen"}
	rc := p.stopOnly()

	r, err := turnOff(rc, "gemma", nil)
	if err == nil {
		t.Fatal("fermare il programma avrebbe tolto anche qwen: andava rifiutato")
	}
	if !p.on() {
		t.Fatal("ha fermato il programma portandosi via un altro modello")
	}
	if len(r.Restano) != 1 || r.Restano[0] != "qwen" {
		t.Errorf("il rifiuto non dice quale modello resterebbe coinvolto: %+v", r)
	}
}

// Lo stesso modello può comparire nel programma con un altro nome: non è un
// «altro modello» che impedisce di spegnere.
func TestGliAliasNonContanoComeAltriModelli(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.status = []string{"Gemma-Alias"}
	rc := p.stopOnly()

	if _, err := turnOff(rc, "gemma", []string{"gemma-alias"}); err != nil {
		t.Fatalf("l'alias è stato contato come un altro modello: %v", err)
	}
	if p.on() {
		t.Fatal("doveva fermare il programma")
	}
}

func TestNonSpegneIlProgrammaMentreUnAltroModelloStaCaricando(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.load("gemma")
	rc := p.unloadsOne()
	fine := markLoading(rc.Chiave, "qwen")
	defer fine()

	r, err := turnOff(rc, "gemma", nil)
	if err != nil {
		t.Fatalf("non spento: %v", err)
	}
	if !p.on() || r.ProgrammaSpento {
		t.Fatal("ha spento il programma sotto un modello che stava caricando")
	}
}

func TestSpegnereConIlProgrammaGiaSpentoNonEsegueNiente(t *testing.T) {
	p := newFakeProgram(t)
	rc := p.unloadsOne()
	spia := filepath.Join(p.dir, "fermato")
	rc.Ferma = "touch " + shQuote(spia)

	if _, err := turnOff(rc, "gemma", nil); err != nil {
		t.Fatalf("spegnere ciò che è già spento non è un errore: %v", err)
	}
	if _, err := os.Stat(spia); err == nil {
		t.Fatal("ha eseguito il comando di arresto su un programma già spento")
	}
}

func TestSeNonSaCosaRestaInMemoriaNonSpegneIlProgramma(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.load("gemma")
	rc := p.unloadsOne()
	rc.Caricati = "exit 7" // il comando che elenca i modelli è rotto

	r, _ := turnOff(rc, "gemma", nil)
	if !p.on() || r.ProgrammaSpento {
		t.Fatal("ha spento il programma senza sapere cosa c'era dentro")
	}
}

func TestProgrammaResidenteSiFermaConIlSuoModello(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	rc := p.stopOnly() // non dice cosa ha caricato: il modello è il programma
	rc.ModelloResidente = true

	r, err := turnOff(rc, "gemma", nil)
	if err != nil || p.on() || !r.ProgrammaSpento {
		t.Fatalf("un residente si spegne fermando il programma: err=%v acceso=%v %+v", err, p.on(), r)
	}
}

func TestSenzaComandoDiArrestoLoDiceENonFaNiente(t *testing.T) {
	p := newFakeProgram(t)
	p.turnOnByHand()
	p.status = []string{"gemma"}
	rc := p.stopOnly()
	rc.Ferma = ""

	if _, err := turnOff(rc, "gemma", nil); err == nil {
		t.Fatal("senza comando di arresto non c'è modo di spegnere: va detto")
	}
}

// ── l'interruttore mostrato dalla pagina ────────────────────────────────────

func TestInterruttoreDiceCosaSuccedePrimaDiPremere(t *testing.T) {
	conScarico := RuntimeCfg{Chiave: "a", Nome: "A", Avvia: "x", Ferma: "y", ScaricaModello: "z {modello}"}
	soloStop := RuntimeCfg{Chiave: "b", Nome: "B", Avvia: "x", Ferma: "y"}
	muto := RuntimeCfg{Chiave: "c", Nome: "C"}
	casi := []struct {
		nome   string
		rc     *RuntimeCfg
		stato  string
		acceso bool
		altri  int
		atteso Switch
	}{
		{"spento con programma spento: parte anche il programma", &conScarico, StatoSpento, false, 0,
			Switch{Puo: true, AvviaProgramma: true}},
		{"spento con programma acceso: solo il modello", &conScarico, StatoSpento, true, 0,
			Switch{Puo: true}},
		{"spento, programma spento e senza comando", &muto, StatoSpento, false, 0,
			Switch{Blocco: "senza-avvio"}},
		{"ultimo modello: si ferma anche il programma", &conScarico, StatoPronto, true, 0,
			Switch{Puo: true, FermaProgramma: true}},
		{"non è l'ultimo: il programma resta", &conScarico, StatoInMemoria, true, 1,
			Switch{Puo: true}},
		{"solo stop, unico modello", &soloStop, StatoPronto, true, 0,
			Switch{Puo: true, FermaProgramma: true}},
		{"solo stop, con altri dentro", &soloStop, StatoPronto, true, 2,
			Switch{Blocco: "altri-modelli"}},
		{"acceso ma senza nessun comando", &muto, StatoPronto, true, 0,
			Switch{Blocco: "senza-arresto"}},
		{"remoto", nil, StatoRemoto, false, 0, Switch{Blocco: "remoto"}},
		{"in arrivo", &conScarico, StatoInArrivo, true, 0, Switch{Blocco: "in-arrivo"}},
		{"guasto", &conScarico, StatoGuasto, true, 0, Switch{Blocco: "guasto"}},
	}
	for _, c := range casi {
		if got := switchOf(c.rc, c.stato, c.acceso, c.altri); got != c.atteso {
			t.Errorf("%s: %+v, atteso %+v", c.nome, got, c.atteso)
		}
	}
}

// ── le rotte ────────────────────────────────────────────────────────────────

func TestRotteAccendiESpegniSonoProtette(t *testing.T) {
	sessionToken = "prova-token"
	listeningPort = 7070
	m := testRoutes()
	for _, p := range []string{"/api/modello/accendi", "/api/modello/spegni"} {
		if w := chiedi(t, m, http.MethodPost, p, false); w.Code != http.StatusForbidden {
			t.Errorf("%s passata senza token: codice %d", p, w.Code)
		}
		if w := chiedi(t, m, http.MethodGet, p, true); w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s accettata in GET: codice %d", p, w.Code)
		}
		// Col token e un corpo vuoto la rotta esiste e rifiuta la richiesta
		// incompleta: 404 vorrebbe dire che non è stata montata.
		if w := chiedi(t, m, http.MethodPost, p, true); w.Code != http.StatusBadRequest {
			t.Errorf("%s con corpo vuoto: codice %d, atteso 400", p, w.Code)
		}
	}
}

// Un rifiuto immediato NON è per forza il lock occupato. Il 03/10/2026
// `stackctl mode qwen-next` ha cominciato a rifiutare subito, per memoria
// insufficiente, prima di fermare qualunque servizio: riprovarlo tre volte non
// cambia la risposta e fa solo perdere tempo. Si riprova solo se il testo
// dell'errore dice che il lock è occupato.
func TestUnRifiutoPerMemoriaInsufficienteNonSiRiprova(t *testing.T) {
	fastStart(t)
	p := newFakeProgram(t)
	rc := p.unloadsOne()
	conteggio := filepath.Join(p.dir, "tentativi")
	rc.Avvia = "echo x >> " + shQuote(conteggio) +
		`; echo '{"ok": false, "error": "Memoria insufficiente per il modello esclusivo: servono 122 GiB liberi. Mancano 40 GiB: nessun avvio"}'; exit 1`

	_, err := turnOn(rc, localDestination(p.port), "gemma")
	if err == nil {
		t.Fatal("un avvio rifiutato è stato dato per riuscito")
	}
	b, _ := os.ReadFile(conteggio)
	if n := strings.Count(string(b), "x"); n != 1 {
		t.Errorf("il comando è stato eseguito %d volte: un rifiuto che non è il lock non si riprova", n)
	}
	// Il messaggio arriva così com'è, senza la busta JSON del controllore.
	if !strings.Contains(err.Error(), "Memoria insufficiente per il modello esclusivo: servono 122 GiB liberi") ||
		strings.Contains(err.Error(), `"ok"`) {
		t.Errorf("il motivo non arriva leggibile: %v", err)
	}
}
