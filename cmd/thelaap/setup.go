package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Tutto quello che è specifico di UNA macchina sta qui dentro, in un file JSON,
// non nel codice: quali programmi eseguono i modelli, dove stanno le
// configurazioni dei client, quali comandi di manutenzione esistono.
// Al primo avvio, se il file non c'è, viene scritto rilevando cosa è installato.
//
// È questo che rende il pannello riusabile su un altro computer: si cambia il
// file, non il programma.

type RuntimeCfg struct {
	Chiave   string `json:"chiave"`              // come lo chiama il primo client
	ChiaveOC string `json:"chiaveAlt,omitempty"` // se il secondo client lo chiama diversamente
	Nome     string `json:"nome"`
	Cosa     string `json:"cosa,omitempty"` // a cosa serve, in parole semplici
	Porta    int    `json:"porta"`
	Elenco   string `json:"elencoModelli"` // percorso che elenca i modelli
	// Comandi per governarlo. Vuoti = il pannello mostra solo lo stato.
	Avvia   string `json:"avvia,omitempty"`
	Ferma   string `json:"ferma,omitempty"`
	Riavvia string `json:"riavvia,omitempty"`
	// Da dove si sa quali modelli sono caricati in memoria (facoltativo)
	Caricati string `json:"modelliCaricati,omitempty"`
	// Come togliere UN solo modello dalla memoria, senza fermare il programma.
	// {modello} viene sostituito col nome, messo fra apici. Vuoto = questo
	// programma non sa farlo, e il pannello lo dice invece di fingere.
	ScaricaModello string `json:"scaricaModello,omitempty"`
	// Perché non sa farlo, in italiano. Mostrato accanto al pulsante.
	NotaScarico string `json:"notaScarico,omitempty"`
	// Tiene il suo modello sempre caricato invece di prenderlo alla prima
	// domanda. Non è una proprietà del prodotto ma di come è fatto: va
	// dichiarata, non indovinata dal nome.
	ModelloResidente bool `json:"modelloResidente,omitempty"`
}

type ClientCfg struct {
	Nome    string `json:"nome"`
	File    string `json:"file"`
	Formato string `json:"formato"` // "pi" oppure "opencode"
}

type ToolCfg struct {
	ID      string `json:"id"`
	Nome    string `json:"nome"`
	Durata  string `json:"durata,omitempty"`
	Cosa    string `json:"cosa,omitempty"`
	Command string `json:"comando"`
	Rischio bool   `json:"rischio,omitempty"`
}

type Config struct {
	Porta          int          `json:"porta"`
	ModelloAiuto   string       `json:"modelloAiuto,omitempty"` // vuoto = lo sceglie da sé
	Runtime        []RuntimeCfg `json:"runtime"`
	Clienti        []ClientCfg  `json:"clienti"`
	Strumenti      []ToolCfg    `json:"strumenti"`
	FermaTutto     string       `json:"fermaTutto,omitempty"`
	RiaccendiTutto string       `json:"riaccendiTutto,omitempty"`
	// Configurazioni di macchina che si accendono e si spengono tutte insieme.
	// Vedi regimes.go.
	Regimi []RegimeCfg `json:"regimi,omitempty"`
	// Quanta memoria lasciare al sistema operativo, in GB. Sotto questa soglia
	// il Mac comincia a comprimere e poi a scrivere su disco, e da lì al blocco
	// totale è un attimo: il 27/07/2026 questa macchina è andata in kernel panic
	// con 6,4 GB lasciati al sistema. 24 è quasi quattro volte tanto.
	// Zero = usa il valore predefinito.
	RiservaSistemaGB float64 `json:"riservaSistemaGB,omitempty"`
	// Cartelle aggiuntive dove cercare i modelli, oltre a quelle standard dei
	// prodotti. Serve a chi tiene i modelli in un posto suo.
	ModelRoots []string `json:"radiciModelli,omitempty"`
	// Sopra questa soglia un modello è "grande" e ne resta uno solo alla volta.
	// Zero = usa il valore predefinito.
	SogliaModelloGrandeGB float64 `json:"sogliaModelloGrandeGB,omitempty"`
}

// Valori predefiniti, usati quando la configurazione non dice altro.
const (
	riservaSistemaGBDefault      = 24.0
	sogliaModelloGrandeGBDefault = 40.0
	// Soglia separata, e volutamente più bassa, per il tetto grafico. Il tetto
	// *autorizza* la GPU a bloccare memoria, non gliela prenota: da solo non
	// toglie niente al sistema. Applicargli la riserva sopra farebbe suonare
	// l'allarme su configurazioni deliberate e legittime. Qui interessa solo
	// il caso patologico — un tetto così permissivo che un singolo processo
	// può affamare il sistema operativo. Il kernel panic del 27/07/2026 è
	// avvenuto con 6,9 GB sotto il tetto.
	minimoSottoIlTettoGBDefault = 12.0
)

// knownDownload: cosa sappiamo di un programma già presente in una vecchia
// configurazione. Si cerca per chiave nella tabella dei candidati, così la
// verità sta in un posto solo.
func knownDownload(chiave string) (comando, nota string) {
	for _, k := range CANDIDATES {
		if k.chiave != chiave {
			continue
		}
		comando = k.scarica
		if comando != "" {
			// Il binario può non essere nel PATH: si prova a risolverlo come
			// al primo rilevamento, altrimenti si lascia il nome nudo.
			primo := strings.Split(comando, " ")[0]
			for _, b := range k.binari {
				if binaryExists(b) {
					comando = strings.Replace(comando, primo, espandi(b), 1)
					break
				}
			}
		}
		return comando, k.notaScarico
	}
	return "", ""
}

func systemReserveGB() float64 {
	if v := cfg().RiservaSistemaGB; v > 0 {
		return v
	}
	return riservaSistemaGBDefault
}

func largeModelThresholdGB() float64 {
	if v := cfg().SogliaModelloGrandeGB; v > 0 {
		return v
	}
	return sogliaModelloGrandeGBDefault
}

var (
	CFG     Config
	cfgMu   sync.RWMutex
	CFGFILE = home(".config/thelaap/configurazione.json")
)

func cfg() Config {
	cfgMu.RLock()
	defer cfgMu.RUnlock()
	return CFG
}

// loadConfig legge il file; se non c'è lo crea rilevando la macchina.
func loadConfig() {
	b, err := os.ReadFile(CFGFILE)
	if err == nil {
		var c Config
		if json.Unmarshal(b, &c) == nil && len(c.Runtime) > 0 {
			// Rattoppo quello che manca invece di ignorarlo: una configurazione
			// scritta da una versione precedente resta valida, e i campi nuovi
			// si riempiono da soli.
			cambiata := false
			if c.Porta == 0 {
				c.Porta, cambiata = 7070, true
			}
			for i := range c.Runtime {
				if c.Runtime[i].Elenco == "" {
					c.Runtime[i].Elenco, cambiata = "/v1/models", true
				}
				// Le capacità di scarico sono arrivate dopo: una configurazione
				// scritta prima non le ha, e senza questo rattoppo il pannello
				// direbbe che nessun programma sa scaricare un modello.
				if c.Runtime[i].ScaricaModello == "" && c.Runtime[i].NotaScarico == "" {
					if s, n := knownDownload(c.Runtime[i].Chiave); s != "" || n != "" {
						c.Runtime[i].ScaricaModello, c.Runtime[i].NotaScarico = s, n
						cambiata = true
					}
				}
				// launchctl load/unload sono deprecati, e il loro parente
				// `kickstart` NON rilegge il plist: se la configurazione del
				// servizio cambia, riavvia con quella vecchia dicendo che e'
				// andato tutto bene. bootout/bootstrap fanno la cosa giusta.
				// Chi tiene il modello sempre in memoria: campo aggiunto dopo, quindi
				// le configurazioni scritte prima non ce l'hanno.
				if !c.Runtime[i].ModelloResidente && knownResident(c.Runtime[i].Chiave) {
					c.Runtime[i].ModelloResidente, cambiata = true, true
				}
				if modernizeLaunchdCommands(&c.Runtime[i]) {
					cambiata = true
				}
			}
			if c.FermaTutto == "" || c.RiaccendiTutto == "" {
				c.FermaTutto, c.RiaccendiTutto = stopAllCommands(c.Runtime)
				cambiata = true
			}
			cfgMu.Lock()
			CFG = c
			cfgMu.Unlock()
			if cambiata {
				saveConfig()
			}
			return
		}
		fmt.Fprintf(os.Stderr, "configurazione illeggibile, la rigenero: %s\n", CFGFILE)
	}
	c := detectMachine()
	cfgMu.Lock()
	CFG = c
	cfgMu.Unlock()
	saveConfig()
}

func saveConfig() error {
	cfgMu.RLock()
	b, err := json.MarshalIndent(CFG, "", "  ")
	cfgMu.RUnlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(CFGFILE), 0o755); err != nil {
		return err
	}
	return os.WriteFile(CFGFILE, append(b, '\n'), 0o644)
}

// ── rilevamento della macchina ──────────────────────────────
//
// Si cercano i programmi noti dove di solito stanno, e per ognuno si prepara la
// voce con i comandi giusti per il sistema operativo in uso. Quello che non c'è
// non viene messo: la configurazione descrive QUESTA macchina.

type candidato struct {
	chiave, chiaveOC, nome, cosa string
	porta                        int
	elenco                       string
	// come si riconosce che è installato
	binari   []string // percorsi da provare (~ espansa)
	comandi  []string // eseguibili da cercare nel PATH
	caricati string   // comando che elenca i modelli in memoria
	// Su macOS l'etichetta launchd NON si dichiara: cambia da installazione a
	// installazione e viene scoperta (launchdLabel in system_darwin.go).
	unitaLinux string // unità systemd --user, se usa systemd
	// Come togliere UN solo modello dalla memoria. {modello} = il nome.
	// Vuoto = non sa farlo, e `notaScarico` spiega perché.
	scarica     string
	notaScarico string
	residente   bool
}

var CANDIDATES = []candidato{
	{chiave: "ollama", chiaveOC: "ollama", nome: "Ollama", cosa: "esegue modelli in formato GGUF",
		porta: 11434, elenco: "/api/tags", comandi: []string{"ollama"},
		caricati: "ollama ps", unitaLinux: "ollama",
		scarica: "ollama stop {modello}"},
	{chiave: "lmstudio", chiaveOC: "mlx", nome: "LM Studio", cosa: "esegue i modelli di chat",
		porta: 1234, elenco: "/v1/models",
		binari:   []string{"~/.lmstudio/bin/lms", "~/.cache/lm-studio/bin/lms"},
		caricati: "lms ps", scarica: "lms unload {modello}"},
	{chiave: "omlx", chiaveOC: "omlx", nome: "oMLX", cosa: "esegue i modelli grandi su Apple Silicon",
		porta: 8000, elenco: "/v1/models", comandi: []string{"omlx"},
		unitaLinux: "omlx",
		// La rotta esiste (POST /admin/api/models/{id}/unload) ma vuole una
		// sessione admin. Finché non si decide come custodire la credenziale,
		// non si promette una cosa che poi fallisce.
		notaScarico: "sa scaricare un solo modello, ma richiede l'accesso da amministratore al suo pannello"},
	{chiave: "mtplx", chiaveOC: "mtplx", nome: "MTPLX", cosa: "esegue il modello per il codice, con decodifica speculativa",
		porta: 8080, elenco: "/v1/models", comandi: []string{"mtplx"},
		unitaLinux:  "mtplx",
		notaScarico: "tiene un modello solo e ci nasce insieme: toglierlo dalla memoria vuol dire fermarlo",
		residente:   true},
	{chiave: "llamacpp", chiaveOC: "llamacpp", nome: "llama.cpp", cosa: "server llama.cpp",
		porta: 8081, elenco: "/v1/models", comandi: []string{"llama-server"}},
	{chiave: "vllm", chiaveOC: "vllm", nome: "vLLM", cosa: "server vLLM",
		porta: 8001, elenco: "/v1/models", comandi: []string{"vllm"}},
}

func espandi(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, _ := os.UserHomeDir()
		return filepath.Join(h, p[2:])
	}
	return p
}

func binaryExists(p string) bool {
	st, err := os.Stat(espandi(p))
	return err == nil && !st.IsDir()
}

func inPath(nome string) bool {
	_, err := exec_LookPath(nome)
	return err == nil
}

func detectMachine() Config {
	c := Config{Porta: 7070, Runtime: []RuntimeCfg{},
		Clienti: []ClientCfg{}, Strumenti: []ToolCfg{}}

	for _, k := range CANDIDATES {
		trovato := false
		binario := ""
		for _, b := range k.binari {
			if binaryExists(b) {
				trovato, binario = true, espandi(b)
				break
			}
		}
		for _, cmd := range k.comandi {
			if inPath(cmd) {
				trovato, binario = true, cmd
				break
			}
		}
		// se non è installato ma risponde sulla porta, vale lo stesso
		if !trovato && answersOnPort(k.porta, k.elenco) {
			trovato = true
		}
		if !trovato {
			continue
		}

		r := RuntimeCfg{Chiave: k.chiave, ChiaveOC: k.chiaveOC, Nome: k.nome,
			Cosa: k.cosa, Porta: k.porta, Elenco: k.elenco}
		if k.caricati != "" && binario != "" {
			r.Caricati = strings.Replace(k.caricati, strings.Split(k.caricati, " ")[0], binario, 1)
		}
		// Stessa sostituzione del binario: `lms` può stare in ~/.lmstudio/bin
		// e non nel PATH.
		if k.scarica != "" && binario != "" {
			r.ScaricaModello = strings.Replace(k.scarica, strings.Split(k.scarica, " ")[0], binario, 1)
		}
		r.NotaScarico = k.notaScarico
		r.ModelloResidente = k.residente
		r.Avvia, r.Ferma, r.Riavvia = serviceCommands(k, binario)
		c.Runtime = append(c.Runtime, r)
	}

	// client noti, se le loro configurazioni esistono
	for _, cl := range []ClientCfg{
		{Nome: "Pi", File: "~/.pi/agent/models.json", Formato: "pi"},
		{Nome: "OpenCode", File: "~/.config/opencode/opencode.json", Formato: "opencode"},
	} {
		if binaryExists(cl.File) {
			c.Clienti = append(c.Clienti, cl)
		}
	}

	c.Strumenti = detectedTools()
	c.FermaTutto, c.RiaccendiTutto = stopAllCommands(c.Runtime)
	return c
}

// answersOnPort: un programma può girare senza che il suo eseguibile sia
// dove ce lo aspettiamo (container, installazione strana): se risponde, c'è.
func answersOnPort(porta int, path string) bool {
	return httpGet(fmt.Sprintf("http://127.0.0.1:%d%s", porta, path), 900_000_000) != nil
}

func apiSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var c Config
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			errJSON(w, "configurazione non valida: "+err.Error())
			return
		}
		if len(c.Runtime) == 0 {
			errJSON(w, "serve almeno un programma che esegua i modelli")
			return
		}
		cfgMu.Lock()
		CFG = c
		cfgMu.Unlock()
		if err := saveConfig(); err != nil {
			errJSON(w, err.Error())
			return
		}
		writeJSON(w, map[string]any{"ok": true, "messaggio": "configurazione salvata in " + CFGFILE})
		return
	}
	if r.URL.Query().Get("rileva") == "1" {
		writeJSON(w, map[string]any{"proposta": detectMachine(), "file": CFGFILE,
			"sistema": runtime.GOOS})
		return
	}
	writeJSON(w, map[string]any{"config": cfg(), "file": CFGFILE, "sistema": runtime.GOOS})
}

// detectedTools: i comandi di manutenzione che esistono su questa macchina.
// Si cercano gli script noti dello stack; quelli che non ci sono non compaiono,
// e restano solo le voci che il pannello sa fare da sé.
func detectedTools() []ToolCfg {
	var out []ToolCfg
	base := ""
	for _, d := range []string{"~/Desktop/AI/localstack", "~/AI/localstack", "~/localstack"} {
		if st, err := os.Stat(espandi(d)); err == nil && st.IsDir() {
			base = espandi(d)
			break
		}
	}
	if base == "" {
		return out
	}
	py := "python3"
	if p, err := exec_LookPath("python3"); err == nil {
		py = p
	}
	noti := []struct {
		file, id, nome, durata, cosa string
		rischio                      bool
	}{
		{"aistack.py", "stato", "Stato rapido", "2 secondi",
			"Chi è acceso, quanta memoria è libera, quanto spazio resta sul disco.", false},
		{"aicheck.py", "controllo", "Controllo veloce", "30 secondi",
			"Verifica versioni, programmi e configurazioni allineate.", false},
		{"aicheck.py", "controllo-completo", "Controllo completo", "diversi minuti",
			"Come sopra, ma prova ogni modello e ne misura la velocità.", false},
		{"aiupdate.py", "aggiornamenti", "Cerca aggiornamenti", "30 secondi",
			"Guarda se ci sono versioni nuove dei programmi e dei modelli. Non installa niente.", false},
		{"aiupdate.py", "aggiorna", "Installa aggiornamenti", "qualche minuto",
			"Aggiorna i programmi e li riavvia. I modelli non li tocca.", true},
	}
	for _, n := range noti {
		f := filepath.Join(base, n.file)
		if _, err := os.Stat(f); err != nil {
			continue
		}
		cmd := py + " " + f
		switch n.id {
		case "controllo":
			cmd += " --fast"
		case "aggiorna":
			cmd += " apply"
		}
		out = append(out, ToolCfg{ID: n.id, Nome: n.nome, Durata: n.durata,
			Cosa: n.cosa, Command: cmd, Rischio: n.rischio})
	}
	// Script liberi accanto agli altri. Dal 16/08/2026 il modello grande è
	// DeepSeek V4 Flash su ds4.sh; laguna.sh resta come ripiego finché esiste,
	// così un'installazione non ancora migrata continua a funzionare.
	grande := filepath.Join(base, "ds4.sh")
	if !binaryExists(grande) {
		grande = filepath.Join(base, "laguna.sh")
	}
	if f := grande; binaryExists(f) {
		out = append(out,
			ToolCfg{ID: "modello-grande-on", Nome: "Attiva il modello grande", Durata: "un minuto",
				Cosa:    "Spegne gli altri programmi e carica il modello che vuole la macchina libera.",
				Command: f + " on"},
			ToolCfg{ID: "modello-grande-off", Nome: "Disattiva il modello grande", Durata: "pochi secondi",
				Cosa: "Rimette in piedi i programmi normali.", Command: f + " off"})
	}
	return out
}

// modernizeLaunchdCommands sostituisce load/unload con bootout/bootstrap
// conservando l'etichetta già scoperta. Tocca solo i comandi che riconosce:
// qualunque altra cosa l'utente abbia scritto resta com'è.
func modernizeLaunchdCommands(r *RuntimeCfg) bool {
	etichetta := ""
	for _, c := range []string{r.Ferma, r.Avvia, r.Riavvia} {
		if i := strings.Index(c, "LaunchAgents/"); i >= 0 {
			resto := c[i+len("LaunchAgents/"):]
			if j := strings.Index(resto, ".plist"); j > 0 {
				etichetta = resto[:j]
				break
			}
		}
	}
	if etichetta == "" {
		return false
	}
	vecchi := strings.Contains(r.Avvia, "launchctl load") ||
		strings.Contains(r.Ferma, "launchctl unload") ||
		strings.Contains(r.Riavvia, "kickstart")
	if !vecchi {
		return false
	}
	p := "~/Library/LaunchAgents/" + etichetta + ".plist"
	g := "gui/$(id -u)/" + etichetta
	r.Avvia = "launchctl bootstrap gui/$(id -u) " + p
	r.Ferma = "launchctl bootout " + g
	r.Riavvia = "launchctl bootout " + g + " 2>/dev/null; sleep 3; launchctl bootstrap gui/$(id -u) " + p
	return true
}

// knownResident: sappiamo che questo programma tiene il modello sempre in
// memoria? Serve solo a riempire il campo nelle configurazioni scritte prima
// che esistesse.
func knownResident(chiave string) bool {
	for _, k := range CANDIDATES {
		if k.chiave == chiave {
			return k.residente
		}
	}
	return false
}
