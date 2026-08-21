package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Il modello che risponde alle domande sull'app dev'essere piccolo, per non
// rubare memoria ai modelli veri. Non lo fissiamo per nome: lo cerchiamo fra
// quelli serviti, preferendo il più piccolo. Così se domani i modelli sono altri,
// l'aiuto continua a funzionare.
var EXPLAIN_MODEL_PINNED = os.Getenv("LAAP_MODELLO_AIUTO") // se vuoi imporne uno

var (
	aiutoScelto   string
	aiutoSceltoMu sync.RWMutex
)

// helperCeilingB: sopra questi miliardi di parametri non è più un modellino.
// Serve a scrivere etichette e a rispondere a domande sul pannello: deve costare
// poco e potersi tenere scaricato, non essere il modello più capace.
const helperCeilingB = 8.0

// numbersInName trova le taglie scritte nel nome: il gruppo 1 è un'eventuale "a"
// (parametri ATTIVI), il 3 un eventuale "it" (è una quantizzazione, non una taglia).
var numbersInName = regexp.MustCompile(`(a?)(\d+(?:\.\d+)?)b(it)?`)

// paramsBillions: quanti miliardi di parametri dice il nome, 0 se non lo dice.
//
// Conta i parametri TOTALI, non quelli attivi. `gemma-4-26b-a4b` è un modello a
// esperti che ne attiva 4 su 26: è veloce, ma in memoria ce ne stanno 26 — e per
// il modellino conta il peso, non la velocità. Leggere «a4b» come «4 miliardi»
// faceva scegliere un 26B come modellino, ed è quello che è successo.
//
// Ignora anche la quantizzazione: `-8bit` non sono 8 miliardi di parametri.
func paramsBillions(id string) float64 {
	piu := 0.0
	for _, m := range numbersInName.FindAllStringSubmatch(strings.ToLower(id), -1) {
		if m[1] == "a" || m[3] == "it" {
			continue // parametri attivi, oppure bit di quantizzazione
		}
		if v := parseFloat(m[2]); v > piu {
			piu = v
		}
	}
	return piu
}

// I mestieri che non sono conversare. Un OCR o un embedding non sanno rispondere
// a una domanda, e proporglielo è farli fallire.
//
// I tratti li deduce indizi() in profiles.go, la stessa tabella che l'interfaccia
// mostra accanto al modello. Scrivere qui un secondo elenco di parole chiave
// voleva dire tenerne due allineati a mano — e il primo tentativo non conosceva
// «bge-», che è una delle famiglie di embedding più diffuse: l'avrebbe proposto
// come aiuto.
var jobsThatDoNotChat = map[string]bool{
	"ricerca-testi": true, "vede-immagini": true, "trascrive": true, "diffusione": true,
}

func cannotHelp(id string) bool {
	for _, i := range indizi(id) {
		if jobsThatDoNotChat[i.Tratto] {
			return true
		}
	}
	return false
}

// helperFallback: vero quando fra i modelli serviti non ce n'era nessuno piccolo e
// si è dovuto usare quello che c'era. Non è un dettaglio da tacere: il pannello
// lo dice, perché un 26B che scrive etichette occupa memoria che serve altrove.
var helperFallback bool

// pickHelper: fra i modelli serviti, il più piccolo che sappia conversare.
// Il secondo valore dice che è un RIPIEGO: nessuno era abbastanza piccolo e si è
// preso quello che c'era. Sta fuori da helperModel perché quella interroga i
// runtime, e la regola va potuta provare senza accendere niente.
func pickHelper(candidati []string) (string, bool) {
	scelto, taglia := "", 0.0
	for _, id := range candidati {
		if cannotHelp(id) {
			continue
		}
		n := paramsBillions(id)
		if n == 0 || n > helperCeilingB {
			continue
		}
		if scelto == "" || n < taglia {
			scelto, taglia = id, n
		}
	}
	if scelto != "" {
		return scelto, false
	}
	// Meglio uno grosso che nessuno — senza aiuto il pannello perde le
	// descrizioni e la chat — ma chi lo usa deve saperlo.
	for _, id := range candidati {
		if !cannotHelp(id) {
			return id, true
		}
	}
	return "", false
}

func helperModel() string {
	// Ordine di precedenza: la variabile d'ambiente (per provarlo), poi la
	// configurazione, poi la scelta automatica. `helperModel` in configurazione
	// prometteva «vuoto = lo sceglie da sé» e non veniva letta da nessuno: chi
	// la scriveva non otteneva niente, in silenzio.
	if EXPLAIN_MODEL_PINNED != "" {
		return EXPLAIN_MODEL_PINNED
	}
	if m := strings.TrimSpace(cfg().ModelloAiuto); m != "" {
		return m
	}
	aiutoSceltoMu.RLock()
	if aiutoScelto != "" {
		defer aiutoSceltoMu.RUnlock()
		return aiutoScelto
	}
	aiutoSceltoMu.RUnlock()

	var candidati []string
	for _, rt := range discoverRuntimes() {
		if rt.Chiave != "omlx" && rt.Chiave != "lmstudio" {
			continue
		}
		candidati = append(candidati, rt.Modelli...)
	}
	scelto, ripiego := pickHelper(candidati)
	aiutoSceltoMu.Lock()
	aiutoScelto, helperFallback = scelto, ripiego
	aiutoSceltoMu.Unlock()
	return scelto
}

// Non è un assistente generico: è il libretto di istruzioni di QUESTO pannello.
// Deve rispondere solo con quello che gli passiamo (manuale + stato di adesso),
// e ammettere di non sapere invece di inventare.
const RULES = `Sei Gellow, l'assistente del pannello di controllo dei modelli AI installati su questo Mac.
Chi ti scrive è la persona che usa il pannello, non un tecnico.

REGOLE:
- Rispondi SOLO con le informazioni che trovi qui sotto. Se manca davvero un passaggio, scrivi «Il rapporto non indica dove cambiare questa impostazione»; non inventare una sezione e non chiedere di ripetere il controllo che stai già spiegando.
- Rispondi in italiano chiaro. A una domanda semplice bastano 3-6 frasi; quando devi spiegare un controllo usa anche 6-12 punti brevi se servono a non omettere problemi.
- Parla di cose concrete che la persona vede sullo schermo (pulsanti, sezioni, la barra della memoria).
- Non usare termini tecnici inglesi se puoi evitarli. Mai parlare di "token", "reasoning_content", "provider", "runtime", "quantizzazione": usa parole normali.
- Se la domanda riguarda com'è messo il Mac adesso, usa i numeri della situazione qui sotto, non parlare in generale.
- Non citare mai i titoli in maiuscolo di questo testo: sono appunti per te. Le sezioni visibili si chiamano "Panoramica", "Modelli", "Configurazioni", "Controlla" e, sotto "Altro", "Programmi" e "Memoria unificata".
- Usa soltanto pulsanti e sezioni elencati nelle AZIONI REALI DEL PANNELLO. La sezione "Sistema" non esiste: non nominarla mai. Non dire mai di usare "Attiva" per spegnere qualcosa.
- Non fare somme o calcoli sui numeri: riporta quelli che trovi, e basta.`

type reqExplain struct {
	Domanda  string `json:"domanda"`
	Contesto string `json:"contesto,omitempty"`
}

// apiQuestions: il repertorio dei suggerimenti, mescolato dal client.
func apiQuestions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, QUESTIONS)
}

// apiHelper: quale modello risponde alle domande e scrive le descrizioni.
//
// Serve al pannello per nominarlo invece di dire «il modellino»: senza questo,
// l'unico modo di scriverlo nella pagina sarebbe cablarne il nome, e cambiando
// macchina la pagina mentirebbe. Sta su una rotta sua e non dentro
// /api/modelli perché sceglierlo interroga i runtime, e /api/modelli viene
// richiesto ogni cinque secondi.
func apiHelper(w http.ResponseWriter, r *http.Request) {
	m := helperModel()
	writeJSON(w, map[string]any{
		"modello": m, "porta": helperPort(),
		"fisso":      EXPLAIN_MODEL_PINNED != "" || strings.TrimSpace(cfg().ModelloAiuto) != "",
		"parametriB": paramsBillions(m),
		"tettoB":     helperCeilingB,
		"ripiego":    helperFallback,
	})
}

func apiExplain(w http.ResponseWriter, r *http.Request) {
	var req reqExplain
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domanda == "" {
		errJSON(w, "domanda mancante")
		return
	}
	// Le istruzioni operative più semplici non hanno bisogno di essere
	// reinventate ogni volta da un modello da 4B. Sono un contratto della UI:
	// così Gellow non può mandare la persona in una sezione che non esiste o
	// confondere «Attiva» con «spegni».
	if risposta := aiutoDiretto(req.Domanda); risposta != "" {
		writeJSON(w, map[string]any{"ok": true, "risposta": risposta})
		return
	}
	// Mini-RAG: prendo dal manuale solo i pezzi che c'entrano con la domanda,
	// e ci aggiungo sempre la fotografia dello stato attuale della macchina.
	var ctx strings.Builder
	ctx.WriteString(RULES)
	ctx.WriteString("\n\n" + liveState())
	if c := strings.TrimSpace(req.Contesto); c != "" {
		// Il risultato dei controlli arriva da comandi già in whitelist, non
		// dall'utente e non viene mai eseguito. Lo passiamo al modellino come
		// testo da spiegare: senza, «Chiedi all'aiutante» vedrebbe soltanto lo
		// stato corrente e non le righe rosse che la persona sta guardando.
		ctx.WriteString("\n\nRISULTATO DEL CONTROLLO DA SPIEGARE:\n")
		ctx.WriteString(trunc(withoutAnsi(c), 8000))
		ctx.WriteString(`

ISTRUZIONI SPECIALI PER QUESTO CONTROLLO:
- Non fermarti al primo errore. Copri ogni causa distinta indicata da una croce rossa o da un avviso importante; riunisci soltanto le righe che dipendono chiaramente dalla stessa causa.
- In questo impianto CODE dipende da MTPLX, CODE-FREE dipende da oMLX e CHAT dipende da LM Studio. Se il programma corrispondente e' spento, il modello che non risponde e' una conseguenza della stessa causa, non un nuovo problema.
- «solo in Pi: X» significa che X e' gia' in Pi ma manca da OpenCode. «solo in OpenCode» significa il contrario.
- «NON risponde» significa spento o irraggiungibile: non dire mai che quel programma e' attivo.
- Un limite di memoria sbagliato e un programma spento sono due cause distinte, anche se riguardano entrambi oMLX. In particolare: oMLX spento + CODE-FREE muto e' una causa; il tetto oMLX insufficiente e' un'altra causa di configurazione.
- Le righe «COSA FARE» del rapporto hanno precedenza: usale e non contraddirle.
- Apri con un riepilogo concreto: quante cause reali vedi e quali funzioni sono bloccate.
- Per ogni causa scrivi: «Problema», «Conseguenza» e «Cosa fare adesso».
- In «Cosa fare adesso» indica il percorso preciso nell'interfaccia. Se il rapporto contiene gia' un comando necessario, riportalo esattamente in un blocco di codice e spiega in una frase cosa fa.
- Distingui cio' che e' davvero guasto da cio' che e' spento per scelta. Non dire di accendere tutto se basta un solo programma.
- Chiudi dicendo quale controllo ripetere e quale risultato verde aspettarsi.
- Non rimandare genericamente a un altro controllo: il rapporto da spiegare e' gia' qui e devi trasformarlo in una soluzione eseguibile.`)
	}
	docs := recupera(req.Domanda, 3)
	if len(docs) == 0 {
		docs = KNOWLEDGE[:2] // almeno "a cosa serve il pannello"
	}
	ctx.WriteString("\nDAL MANUALE DEL PANNELLO:\n")
	for _, d := range docs {
		ctx.WriteString("\n## " + d.Titolo + "\n" + d.Testo + "\n")
	}

	maxTokens := 500
	if strings.TrimSpace(req.Contesto) != "" {
		maxTokens = 1000
	}
	corpo, _ := json.Marshal(map[string]any{
		"model": helperModel(),
		"messages": []any{
			map[string]any{"role": "system", "content": ctx.String()},
			map[string]any{"role": "user", "content": trunc(req.Domanda, 500)},
		},
		"max_tokens":  maxTokens,
		"temperature": 0.2,
	})
	cl := &http.Client{Timeout: 10 * time.Minute}
	resp, err := cl.Post("http://127.0.0.1:"+itoa(helperPort())+"/v1/chat/completions",
		"application/json", bytes.NewReader(corpo))
	if err != nil {
		errJSON(w, "Gellow non risponde: "+err.Error())
		return
	}
	defer resp.Body.Close()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		errJSON(w, err.Error())
		return
	}
	if e, ok := v["error"]; ok {
		errJSON(w, trunc(sprint(e), 200))
		return
	}
	scelte, _ := v["choices"].([]any)
	if len(scelte) == 0 {
		errJSON(w, "nessuna risposta")
		return
	}
	c0, _ := scelte[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	testo, _ := msg["content"].(string)
	writeJSON(w, map[string]any{"ok": true, "risposta": testo})
}

func aiutoDiretto(domanda string) string {
	q := strings.ToLower(strings.TrimSpace(domanda))
	vuoleSpegnere := strings.Contains(q, "spegn") || strings.Contains(q, "disattiv") || strings.Contains(q, "togli") && strings.Contains(q, "ram")
	parlaDiModello := strings.Contains(q, "modell")
	if !vuoleSpegnere || !parlaDiModello {
		return ""
	}
	risposta := "Vai in «Modelli» e clicca la riga del modello. Se è caricato, nella sua scheda trovi il pulsante «Disattiva modello»: lo toglie dalla RAM, ma non cancella i file e non lo rimuove da Pi o OpenCode. Il programma che lo esegue resta acceso; se quel programma non sa scaricare un singolo modello, il pulsante dice invece chiaramente quale programma verrà spento."
	if caricati := readsMemory().Caricati; len(caricati) > 0 {
		nomi := make([]string, 0, len(caricati))
		for _, c := range caricati {
			nomi = append(nomi, c.Nome)
		}
		risposta += " Adesso risultano in memoria: " + strings.Join(nomi, ", ") + ". Questa risposta non spegne nulla."
	}
	return risposta
}

// helperPort: su quale runtime vive il modello scelto per l'aiuto.
func helperPort() int {
	m := helperModel()
	for _, rt := range discoverRuntimes() {
		for _, id := range rt.Modelli {
			if id == m {
				return rt.Porta
			}
		}
	}
	return 8000
}
