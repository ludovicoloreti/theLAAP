package main

// lifecycle.go — accendere e spegnere un modello, col suo programma.
//
// Prima erano due gesti separati e toccava alla persona metterli in fila:
// accendere il programma dalla scheda Programmi, poi attivare il modello; e al
// contrario scaricare il modello e ricordarsi di fermare il programma rimasto
// acceso a vuoto. Qui diventano uno:
//
//	accendi   se il programma è spento lo avvia, aspetta che risponda, poi
//	          carica il modello
//	spegni    toglie il modello dalla memoria e, se in quel programma non ne
//	          resta nessun altro, ferma anche il programma
//
// I comandi sono sempre quelli dichiarati in configurazione (serviceCommand):
// qui non si lancia nessun processo per conto proprio. Su questa macchina
// passano tutti dal controllore dello stack, che tiene il suo lock e il suo
// stato desiderato — avviare i programmi da Go lo lascerebbe al buio.

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	// Quanto si aspetta che un programma appena avviato risponda sulla porta.
	runtimeStartTimeout = 2 * time.Minute
	runtimeStartPoll    = time.Second
	// Il controllore dello stack rifiuta subito se sta già facendo altro (il
	// suo lock non aspetta, e il guardiano lo prende ogni due minuti). Un
	// rifiuto immediato si riprova poco dopo; un comando che fallisce dopo
	// averci lavorato sopra no, perché rifarlo costerebbe altri minuti.
	commandRetries    = 3
	commandRetryPause = 3 * time.Second
	commandFastFail   = 2 * time.Second
)

// lockBusyMarkers: come il controllore dice «sto già facendo altro». Un rifiuto
// immediato NON basta a riprovare: il 03/10/2026 `stackctl mode qwen-next` ha
// cominciato a rifiutare subito per memoria insufficiente, e riprovarlo non
// cambia la risposta. Si riprova solo se il testo dice che il lock è occupato.
var lockBusyMarkers = []string{"già in corso", "gia' in corso", "already in progress"}

func lockBusy(out string) bool {
	basso := strings.ToLower(out)
	for _, m := range lockBusyMarkers {
		if strings.Contains(basso, m) {
			return true
		}
	}
	return false
}

// stackMu: un comando di avvio o arresto alla volta, per tutti i programmi.
// Due lanciati insieme farebbero fallire il secondo sul lock del controllore.
var stackMu sync.Mutex

// execStack esegue un comando di avvio o arresto. Chi chiama tiene stackMu.
func execStack(linea string) (string, error) {
	var out string
	var err error
	for tentativo := 1; ; tentativo++ {
		inizio := time.Now()
		out, err = shErr(6*time.Minute, linea+" 2>&1")
		if err == nil || tentativo == commandRetries || time.Since(inizio) > commandFastFail || !lockBusy(out) {
			return out, err
		}
		time.Sleep(commandRetryPause)
	}
}

// commandFailure: il messaggio del programma vale più del codice di uscita.
func commandFailure(out string, err error) string {
	if m := withoutAnsi(out); m != "" {
		// Il controllore risponde con {"ok": false, "error": "…"}: all'utente
		// arriva il motivo così com'è, non la busta.
		var j struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(m), &j) == nil && strings.TrimSpace(j.Error) != "" {
			return trunc(j.Error, 400)
		}
		return trunc(m, 400)
	}
	return err.Error()
}

// runtimeUp: il programma risponde sulla sua porta. È la stessa domanda di
// discoverRuntimes, e la risposta viene dalla porta, non da uno stato salvato:
// dopo un riavvio del Mac launchd rialza i servizi anche se l'ultimo gesto era
// stato spegnerli.
func runtimeUp(rc RuntimeCfg) bool {
	elenco := rc.Elenco
	if elenco == "" {
		elenco = "/v1/models"
	}
	return httpGet("http://127.0.0.1:"+itoa(rc.Porta)+elenco, 2*time.Second) != nil
}

// ensureRuntime avvia il programma se è spento e aspetta che risponda.
// Torna true quando l'ha avviato lui.
func ensureRuntime(rc RuntimeCfg) (bool, error) {
	if runtimeUp(rc) {
		return false, nil
	}
	linea := serviceCommand(rc, "start")
	if linea == "" {
		return false, fmt.Errorf("%s è spento e il pannello non ha il comando per accenderlo", rc.Nome)
	}
	stackMu.Lock()
	defer stackMu.Unlock()
	// Di nuovo, col lock in mano: due richieste per lo stesso programma spento
	// arrivano qui una dopo l'altra, e la seconda lo trova già acceso.
	if runtimeUp(rc) {
		return false, nil
	}
	if out, err := execStack(linea); err != nil {
		return false, fmt.Errorf("%s non si è avviato: %s", rc.Nome, commandFailure(out, err))
	}
	limite := time.Now().Add(runtimeStartTimeout)
	for !runtimeUp(rc) {
		if time.Now().After(limite) {
			return true, fmt.Errorf("%s è stato avviato ma dopo %s non risponde ancora",
				rc.Nome, runtimeStartTimeout)
		}
		time.Sleep(runtimeStartPoll)
	}
	return true, nil
}

// stopRuntime ferma il programma col suo comando.
func stopRuntime(rc RuntimeCfg) error {
	linea := serviceCommand(rc, "stop")
	if linea == "" {
		return fmt.Errorf("%s non si può fermare da qui: manca il comando nella configurazione", rc.Nome)
	}
	stackMu.Lock()
	defer stackMu.Unlock()
	if out, err := execStack(linea); err != nil {
		return fmt.Errorf("%s non si è fermato: %s", rc.Nome, commandFailure(out, err))
	}
	return nil
}

// ── i modelli che stanno caricando ──────────────────────────────────────────
//
// Caricare un modello grande dura minuti, e per tutto quel tempo il programma
// non lo elenca ancora fra i caricati. Senza questo registro, spegnere un altro
// modello dello stesso programma lo troverebbe «vuoto» e lo fermerebbe sotto
// quello che sta entrando.

var loading = struct {
	sync.Mutex
	m map[string]map[string]int // programma → modello → richieste in corso
}{m: map[string]map[string]int{}}

func markLoading(runtime, model string) func() {
	k, id := strings.ToLower(runtime), strings.ToLower(model)
	loading.Lock()
	if loading.m[k] == nil {
		loading.m[k] = map[string]int{}
	}
	loading.m[k][id]++
	loading.Unlock()
	return func() {
		loading.Lock()
		if loading.m[k][id]--; loading.m[k][id] <= 0 {
			delete(loading.m[k], id)
		}
		loading.Unlock()
	}
}

// loadingOthers: in questo programma sta entrando qualcosa di diverso da
// questi nomi?
func loadingOthers(runtime string, propri []string) bool {
	loading.Lock()
	defer loading.Unlock()
	for id := range loading.m[strings.ToLower(runtime)] {
		if !containsFold(propri, id) {
			return true
		}
	}
	return false
}

// ── accendi ─────────────────────────────────────────────────────────────────

// LifecycleResult: cosa è successo, a campi. La pagina lo disegna; non c'è
// una frase da mostrare.
type LifecycleResult struct {
	OK      bool   `json:"ok"`
	Model   string `json:"modello"`
	Runtime string `json:"runtime"`
	// accendi
	ProgrammaAvviato bool    `json:"programmaAvviato,omitempty"`
	LoadSec          float64 `json:"loadSec,omitempty"`
	// spegni
	Scaricato       bool `json:"scaricato,omitempty"`
	ProgrammaSpento bool `json:"programmaSpento,omitempty"`
	// Gli altri modelli che quel programma tiene ancora in memoria: è il motivo
	// per cui resta acceso, o per cui lo spegnimento è stato rifiutato.
	Restano []string `json:"restano,omitempty"`
}

func turnOn(rc RuntimeCfg, d destinazione, model string) (LifecycleResult, error) {
	r := LifecycleResult{Model: model, Runtime: rc.Chiave}
	// Segnato prima ancora di avviare il programma: chi nel frattempo spegne
	// un altro modello non deve fermare il programma che sto per usare.
	fine := markLoading(rc.Chiave, model)
	defer fine()

	avviato, err := ensureRuntime(rc)
	r.ProgrammaAvviato = avviato
	if err != nil {
		return r, err
	}
	e := activateModelAt(d, model)
	if !e.OK {
		// Il programma è stato acceso solo per questo modello: se il modello
		// non parte non deve restare acceso a vuoto. Uno che era già acceso
		// per conto suo, invece, non si tocca.
		if avviato {
			if dentro, errLista := loadedIn(rc); errLista == nil && len(dentro) == 0 &&
				!loadingOthers(rc.Chiave, []string{model}) && stopRuntime(rc) == nil {
				r.ProgrammaAvviato = false
			}
		}
		return r, errors.New(e.Errore)
	}
	r.OK, r.LoadSec = true, e.LoadSec
	return r, nil
}

// ── spegni ──────────────────────────────────────────────────────────────────

func turnOff(rc RuntimeCfg, model string, alias []string) (LifecycleResult, error) {
	r := LifecycleResult{Model: model, Runtime: rc.Chiave}
	if !runtimeUp(rc) {
		r.OK = true // già spento: niente in memoria, niente da fermare
		return r, nil
	}
	propri := append([]string{model}, alias...)
	dentro, errLista := loadedIn(rc)
	for _, c := range dentro {
		if !containsFold(propri, c.Nome) {
			r.Restano = append(r.Restano, c.Nome)
		}
	}
	nome := rc.Nome
	if nome == "" {
		nome = rc.Chiave
	}

	if strings.TrimSpace(rc.ScaricaModello) != "" {
		// Si scarica col nome con cui il programma lo tiene, che può essere un
		// alias. Se l'elenco non si legge si prova comunque col nome chiesto.
		daScaricare := ""
		if errLista != nil {
			daScaricare = model
		}
		for _, c := range dentro {
			if containsFold(propri, c.Nome) {
				daScaricare = c.Nome
				break
			}
		}
		if daScaricare != "" {
			if _, err := unloadFrom(rc, daScaricare); err != nil {
				return r, err
			}
			r.Scaricato = true
		}
		r.OK = true
		// Senza sapere cosa resta dentro, il programma resta acceso: fermarlo
		// alla cieca può portarsi via il modello che qualcuno sta usando.
		if errLista != nil || len(r.Restano) > 0 || loadingOthers(rc.Chiave, propri) ||
			serviceCommand(rc, "stop") == "" {
			return r, nil
		}
		if err := stopRuntime(rc); err != nil {
			return r, err
		}
		r.ProgrammaSpento = true
		return r, nil
	}

	// Questo programma non sa togliere un solo modello: spegnere il modello
	// vuol dire fermare il programma, e si può solo se dentro non c'è altro.
	if errLista != nil {
		return r, fmt.Errorf("non riesco a sapere cosa tiene in memoria %s: non lo fermo alla cieca", nome)
	}
	if len(r.Restano) > 0 || loadingOthers(rc.Chiave, propri) {
		return r, fmt.Errorf("%s non sa togliere un solo modello dalla memoria, e ne tiene altri: "+
			"fermarlo li spegnerebbe tutti", nome)
	}
	if err := stopRuntime(rc); err != nil {
		return r, err
	}
	r.OK, r.Scaricato, r.ProgrammaSpento = true, true, true
	return r, nil
}

// ── l'interruttore ──────────────────────────────────────────────────────────

// Switch: cosa farebbe l'interruttore di un modello se lo si premesse adesso.
// Lo calcola il server, che è lo stesso che poi esegue: la pagina lo disegna e
// non decide da sé cosa si può fare a un programma.
type Switch struct {
	Puo bool `json:"puo"`
	// Accendendo parte anche il programma; spegnendo si ferma anche lui.
	AvviaProgramma bool `json:"avviaProgramma,omitempty"`
	FermaProgramma bool `json:"fermaProgramma,omitempty"`
	// Perché non si può. È una chiave, non una frase:
	// remoto · in-arrivo · guasto · senza-avvio · senza-arresto · altri-modelli
	Blocco string `json:"blocco,omitempty"`
}

// switchOf: `acceso` dice se il programma risponde, `altri` quanti altri
// modelli tiene in memoria oltre a questo.
func switchOf(rc *RuntimeCfg, stato string, acceso bool, altri int) Switch {
	switch stato {
	case StatoRemoto:
		return Switch{Blocco: "remoto"}
	case StatoInArrivo:
		return Switch{Blocco: "in-arrivo"}
	case StatoGuasto:
		return Switch{Blocco: "guasto"}
	}
	if rc == nil {
		return Switch{Blocco: "guasto"}
	}
	if stato == StatoSpento {
		if acceso {
			return Switch{Puo: true}
		}
		if serviceCommand(*rc, "start") == "" {
			return Switch{Blocco: "senza-avvio"}
		}
		return Switch{Puo: true, AvviaProgramma: true}
	}
	// pronto o in memoria
	puoFermare := serviceCommand(*rc, "stop") != ""
	if strings.TrimSpace(rc.ScaricaModello) != "" {
		return Switch{Puo: true, FermaProgramma: altri == 0 && puoFermare}
	}
	switch {
	case altri > 0:
		return Switch{Blocco: "altri-modelli"}
	case !puoFermare:
		return Switch{Blocco: "senza-arresto"}
	}
	return Switch{Puo: true, FermaProgramma: true}
}

// ── le rotte ────────────────────────────────────────────────────────────────

type lifecycleRequest struct {
	Model   string `json:"modello"`
	Runtime string `json:"runtime"`
	Porta   int    `json:"porta,omitempty"` // solo per i chiamanti di prima: non serve più
}

func decodeLifecycle(w http.ResponseWriter, r *http.Request) (lifecycleRequest, bool) {
	var req lifecycleRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile: "+err.Error())
		return req, false
	}
	if req.Model == "" || req.Runtime == "" {
		errJSON(w, "modello o programma mancante")
		return req, false
	}
	return req, true
}

// cardOf: la scheda di questo modello, per sapere quanto pesa e con quali
// altri nomi compare.
func cardOf(runtime, id string) *Card {
	for _, s := range schede() {
		if s.Runtime == runtime && s.ID == id {
			c := s
			return &c
		}
	}
	return nil
}

func lifecycleRefused(w http.ResponseWriter, codice string, err error, extra map[string]any) {
	out := map[string]any{"ok": false, "codice": codice, "errore": err.Error()}
	for k, v := range extra {
		out[k] = v
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(out)
}

// freshMemory chiede una fotografia nuova e aspetta che arrivi: una cominciata
// DOPO questa chiamata, perché quella in corso può aver letto prima del gesto.
// Così la pagina, che subito dopo rilegge lo stato, non mostra ancora acceso
// quello che è appena stato spento.
func freshMemory(limite time.Duration) {
	inizio := time.Now()
	refreshMemory()
	for time.Since(inizio) < limite {
		if currentMemory().Istante.After(inizio) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func apiTurnOn(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeLifecycle(w, r)
	if !ok {
		return
	}
	d, haProvider := destinationFor(req.Runtime)
	rc := runtimeByKey(req.Runtime)
	if rc == nil {
		// Un provider remoto non ha un programma da avviare qui: resta la sola
		// richiesta di prova, come prima.
		if !haProvider || d.baseURL == "" {
			errJSON(w, "non conosco il programma: "+req.Runtime)
			return
		}
		if e := activateModelAt(d, req.Model); !e.OK {
			errJSON(w, e.Errore)
			return
		}
		updateProfile(req.Runtime, req.Model, func(p *Profile) { p.UltimoUso = time.Now() })
		writeJSON(w, LifecycleResult{OK: true, Model: req.Model, Runtime: req.Runtime})
		return
	}
	if !haProvider || d.baseURL == "" {
		d = localDestination(rc.Porta)
	}
	// «Ci sta?» si chiede prima di avviare qualunque cosa. Vale anche qui e non
	// solo nella pagina: questa rotta la può chiamare chiunque, e l'arbitro
	// esiste proprio perché nessun programma vede la macchina intera.
	s := cardOf(req.Runtime, req.Model)
	if s == nil || !caricato(*s, currentMemory()) {
		// Un modello di cui non si conosce il peso non salta l'arbitro: lo si
		// tratta come un modello grande, perché potrebbe esserlo.
		pesoGB := largeModelThresholdGB()
		if s != nil && s.GB > 0 {
			pesoGB = s.GB
		}
		if v := currentBudget().Admits(uint64(pesoGB*1e9), currentPolicy()); !v.Allowed {
			lifecycleRefused(w, "non-ci-sta", errors.New("non ci sta in memoria adesso"),
				map[string]any{"verdetto": v})
			return
		}
	}
	res, err := turnOn(*rc, d, req.Model)
	if err != nil {
		freshMemory(8 * time.Second)
		lifecycleRefused(w, "non-acceso", err, map[string]any{"programmaAvviato": res.ProgrammaAvviato})
		return
	}
	rememberActive(req.Runtime, req.Model)
	updateProfile(req.Runtime, req.Model, func(p *Profile) { p.UltimoUso = time.Now() })
	freshMemory(8 * time.Second)
	writeJSON(w, res)
}

func apiTurnOff(w http.ResponseWriter, r *http.Request) {
	req, ok := decodeLifecycle(w, r)
	if !ok {
		return
	}
	rc := runtimeByKey(req.Runtime)
	if rc == nil {
		errJSON(w, "non conosco il programma: "+req.Runtime)
		return
	}
	var alias []string
	if s := cardOf(req.Runtime, req.Model); s != nil {
		for _, a := range s.Alias {
			alias = append(alias, a.ID)
		}
	}
	res, err := turnOff(*rc, req.Model, alias)
	if err != nil {
		codice := "non-spento"
		if len(res.Restano) > 0 {
			codice = "altri-modelli"
		}
		lifecycleRefused(w, codice, err, map[string]any{"restano": res.Restano})
		return
	}
	forgetActive(req.Runtime, req.Model)
	freshMemory(8 * time.Second)
	writeJSON(w, res)
}
