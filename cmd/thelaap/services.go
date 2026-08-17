package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

// Comandi ammessi: nessun input libero finisce mai in una shell.
// I comandi eseguibili vengono dalla configurazione: cambiano da macchina a
// macchina. Restano una lista chiusa — nessun testo scritto dall'utente finisce
// mai in una shell.
func allowedCommand(id string) (string, bool) {
	c := cfg()
	switch id {
	case "ferma-tutto":
		return c.FermaTutto, c.FermaTutto != ""
	case "riaccendi-tutto":
		return c.RiaccendiTutto, c.RiaccendiTutto != ""
	}
	for _, s := range c.Strumenti {
		if s.ID == id {
			return s.Command, true
		}
	}
	return "", false
}

// apiRun: esegue un comando della whitelist e ne trasmette l'output in tempo reale.
//
// Il nome arriva nel corpo della POST, non nella query: da GET la rotta era
// raggiungibile con un <img src="...?cmd=ferma-tutto"> da qualunque pagina web.
func apiRun(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cmd string `json:"cmd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "corpo non leggibile", http.StatusBadRequest)
		return
	}
	linea, ok := allowedCommand(req.Cmd)
	if !ok {
		http.Error(w, "comando non ammesso", http.StatusForbidden)
		return
	}
	streamCommand(w, r, linea)
}

// streamCommand esegue una riga di shell e ne trasmette l'output in tempo
// reale. Condivisa fra i comandi di manutenzione e i passaggi di regime:
// entrambi durano minuti e devono poter essere interrotti chiudendo la pagina.
func streamCommand(w http.ResponseWriter, r *http.Request, linea string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming non supportato", http.StatusInternalServerError)
		return
	}

	// Tetto di tempo, e contesto legato alla richiesta.
	//
	// Prima non c'era né l'uno né l'altro — nonostante il README dicesse che
	// ogni comando esterno ha un tetto. Un comando impiantato teneva la
	// connessione aperta per sempre; se l'utente chiudeva la pagina, il
	// processo restava a girare senza che nessuno lo aspettasse più.
	ctx, annulla := context.WithTimeout(r.Context(), maintenanceCommandTimeout)
	defer annulla()

	cmd := exec.Command("/bin/sh", "-c", linea)
	isolateProcessGroup(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(w, "data: __ERRORE__ non riesco a leggere l'output: %s\n\n", err)
		flusher.Flush()
		return
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "data: __ERRORE__ non sono riuscito ad avviarlo: %s\n\n", err)
		flusher.Flush()
		return
	}

	// Alla scadenza o alla disconnessione del client si uccide tutto il gruppo:
	// i comandi sono pipeline di shell, e uccidere solo `sh` lascerebbe i figli
	// a girare tenendo la memoria.
	finito := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			killGroup(cmd)
		case <-finito:
		}
	}()

	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		riga := strings.ReplaceAll(sc.Text(), "\r", "")
		fmt.Fprintf(w, "data: %s\n\n", riga)
		flusher.Flush()
	}
	errScan := sc.Err()
	errAttesa := cmd.Wait()
	close(finito)

	// L'esito arriva alla pagina. Prima veniva scartato: un comando fallito
	// era indistinguibile da uno riuscito, e la console diceva "finito".
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		fmt.Fprintf(w, "data: __ERRORE__ interrotto dopo %s: non stava finendo\n\n",
			maintenanceCommandTimeout)
	case r.Context().Err() != nil:
		return // il client se n'è andato: non c'è nessuno da avvisare
	case errAttesa != nil:
		fmt.Fprintf(w, "data: __ERRORE__ %s\n\n", withoutAnsi(errAttesa.Error()))
	case errScan != nil:
		fmt.Fprintf(w, "data: __ERRORE__ output troncato: %s\n\n", withoutAnsi(errScan.Error()))
	default:
		fmt.Fprint(w, "data: __FINE__\n\n")
	}
	flusher.Flush()
}

// Quanto può durare al massimo un comando di manutenzione. Il più lungo
// dichiarato nella configurazione è il controllo completo, che misura ogni
// modello: minuti, non ore.
const maintenanceCommandTimeout = 15 * time.Minute

// apiService: start / stop / restart di un LaunchAgent, oppure LM Studio e Ollama.
// serviceCommand: la riga di shell per accendere, fermare o riavviare un
// programma, oppure "" se la configurazione non lo permette.
//
// Sta in una funzione sola perché la risposta serve in due posti: qui, per
// eseguirla, e in discovery.go, per dire al pannello quali pulsanti mostrare.
// Con due copie della regola il pannello offrirebbe un riavvio che il server
// rifiuta — e il pulsante non farebbe niente, in silenzio.
func serviceCommand(rc RuntimeCfg, azione string) string {
	switch azione {
	case "start":
		return strings.TrimSpace(rc.Avvia)
	case "stop":
		return strings.TrimSpace(rc.Ferma)
	case "restart":
		if l := strings.TrimSpace(rc.Riavvia); l != "" {
			return l
		}
		// Senza un comando suo, riavviare è fermare e riaccendere: si può solo
		// se esistono entrambi.
		if strings.TrimSpace(rc.Ferma) != "" && strings.TrimSpace(rc.Avvia) != "" {
			return strings.TrimSpace(rc.Ferma) + "; sleep 2; " + strings.TrimSpace(rc.Avvia)
		}
	}
	return ""
}

func apiService(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Servizio string `json:"servizio"`
		Azione   string `json:"azione"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, err.Error())
		return
	}
	var rc *RuntimeCfg
	for _, x := range cfg().Runtime {
		if x.Chiave == req.Servizio {
			c := x
			rc = &c
			break
		}
	}
	if rc == nil {
		errJSON(w, "non conosco il programma: "+req.Servizio)
		return
	}
	if req.Azione != "start" && req.Azione != "stop" && req.Azione != "restart" {
		errJSON(w, "azione sconosciuta: "+req.Azione)
		return
	}
	linea := serviceCommand(*rc, req.Azione)
	if linea == "" {
		errJSON(w, rc.Nome+" non si può governare da qui: manca il comando nella configurazione")
		return
	}
	out := sh(linea + " 2>&1")

	if strings.TrimSpace(out) == "" {
		out = "eseguito"
	}
	writeJSON(w, map[string]any{"ok": true, "output": trunc(out, 500)})
}
