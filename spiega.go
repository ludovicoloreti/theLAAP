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
var MODELLO_SPIEGA_FISSO = os.Getenv("LAAP_MODELLO_AIUTO") // se vuoi imporne uno

var (
	aiutoScelto   string
	aiutoSceltoMu sync.RWMutex
)

// tettoModellinoB: sopra questi miliardi di parametri non è più un modellino.
// Serve a scrivere etichette e a rispondere a domande sul pannello: deve costare
// poco e potersi tenere scaricato, non essere il modello più capace.
const tettoModellinoB = 8.0

// numeriNelNome trova le taglie scritte nel nome: il gruppo 1 è un'eventuale "a"
// (parametri ATTIVI), il 3 un eventuale "it" (è una quantizzazione, non una taglia).
var numeriNelNome = regexp.MustCompile(`(a?)(\d+(?:\.\d+)?)b(it)?`)

// parametriMiliardi: quanti miliardi di parametri dice il nome, 0 se non lo dice.
//
// Conta i parametri TOTALI, non quelli attivi. `gemma-4-26b-a4b` è un modello a
// esperti che ne attiva 4 su 26: è veloce, ma in memoria ce ne stanno 26 — e per
// il modellino conta il peso, non la velocità. Leggere «a4b» come «4 miliardi»
// faceva scegliere un 26B come modellino, ed è quello che è successo.
//
// Ignora anche la quantizzazione: `-8bit` non sono 8 miliardi di parametri.
func parametriMiliardi(id string) float64 {
	piu := 0.0
	for _, m := range numeriNelNome.FindAllStringSubmatch(strings.ToLower(id), -1) {
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
// I tratti li deduce indizi() in profili.go, la stessa tabella che l'interfaccia
// mostra accanto al modello. Scrivere qui un secondo elenco di parole chiave
// voleva dire tenerne due allineati a mano — e il primo tentativo non conosceva
// «bge-», che è una delle famiglie di embedding più diffuse: l'avrebbe proposto
// come aiuto.
var mestieriCheNonConversano = map[string]bool{
	"ricerca-testi": true, "vede-immagini": true, "trascrive": true, "diffusione": true,
}

func nonPuoFareAiuto(id string) bool {
	for _, i := range indizi(id) {
		if mestieriCheNonConversano[i.Tratto] {
			return true
		}
	}
	return false
}

// aiutoRipiego: vero quando fra i modelli serviti non ce n'era nessuno piccolo e
// si è dovuto usare quello che c'era. Non è un dettaglio da tacere: il pannello
// lo dice, perché un 26B che scrive etichette occupa memoria che serve altrove.
var aiutoRipiego bool

// scegliModellino: fra i modelli serviti, il più piccolo che sappia conversare.
// Il secondo valore dice che è un RIPIEGO: nessuno era abbastanza piccolo e si è
// preso quello che c'era. Sta fuori da modelloAiuto perché quella interroga i
// runtime, e la regola va potuta provare senza accendere niente.
func scegliModellino(candidati []string) (string, bool) {
	scelto, taglia := "", 0.0
	for _, id := range candidati {
		if nonPuoFareAiuto(id) {
			continue
		}
		n := parametriMiliardi(id)
		if n == 0 || n > tettoModellinoB {
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
		if !nonPuoFareAiuto(id) {
			return id, true
		}
	}
	return "", false
}

func modelloAiuto() string {
	// Ordine di precedenza: la variabile d'ambiente (per provarlo), poi la
	// configurazione, poi la scelta automatica. `modelloAiuto` in configurazione
	// prometteva «vuoto = lo sceglie da sé» e non veniva letta da nessuno: chi
	// la scriveva non otteneva niente, in silenzio.
	if MODELLO_SPIEGA_FISSO != "" {
		return MODELLO_SPIEGA_FISSO
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
	for _, rt := range scopriRuntime() {
		if rt.Chiave != "omlx" && rt.Chiave != "lmstudio" {
			continue
		}
		candidati = append(candidati, rt.Modelli...)
	}
	scelto, ripiego := scegliModellino(candidati)
	aiutoSceltoMu.Lock()
	aiutoScelto, aiutoRipiego = scelto, ripiego
	aiutoSceltoMu.Unlock()
	return scelto
}

// Non è un assistente generico: è il libretto di istruzioni di QUESTO pannello.
// Deve rispondere solo con quello che gli passiamo (manuale + stato di adesso),
// e ammettere di non sapere invece di inventare.
const REGOLE = `Sei l'aiuto del pannello di controllo dei modelli AI installati su questo Mac.
Chi ti scrive è la persona che usa il pannello, non un tecnico.

REGOLE:
- Rispondi SOLO con le informazioni che trovi qui sotto. Se la risposta non c'è, dillo e basta: "Questo non lo so, prova con Controlla tutto nella sezione Sistema".
- Rispondi in italiano, breve: 2-4 frasi. Nessun elenco puntato se non serve davvero.
- Parla di cose concrete che la persona vede sullo schermo (pulsanti, sezioni, la barra della memoria).
- Non usare termini tecnici inglesi se puoi evitarli. Mai parlare di "token", "reasoning_content", "provider", "runtime", "quantizzazione": usa parole normali.
- Se la domanda riguarda com'è messo il Mac adesso, usa i numeri della situazione qui sotto, non parlare in generale.
- Non citare mai i titoli in maiuscolo di questo testo: sono appunti per te, non cose che la persona vede sullo schermo. Le sezioni che lei vede si chiamano "I tuoi modelli", "Aggiungi un modello" e "Sistema".
- Non fare somme o calcoli sui numeri: riporta quelli che trovi, e basta.`

type reqSpiega struct {
	Domanda string `json:"domanda"`
}

// apiDomande: il repertorio dei suggerimenti, mescolato dal client.
func apiDomande(w http.ResponseWriter, r *http.Request) {
	scriviJSON(w, DOMANDE)
}

// apiAiuto: quale modello risponde alle domande e scrive le descrizioni.
//
// Serve al pannello per nominarlo invece di dire «il modellino»: senza questo,
// l'unico modo di scriverlo nella pagina sarebbe cablarne il nome, e cambiando
// macchina la pagina mentirebbe. Sta su una rotta sua e non dentro
// /api/modelli perché sceglierlo interroga i runtime, e /api/modelli viene
// richiesto ogni cinque secondi.
func apiAiuto(w http.ResponseWriter, r *http.Request) {
	m := modelloAiuto()
	scriviJSON(w, map[string]any{
		"modello": m, "porta": portaAiuto(),
		"fisso":      MODELLO_SPIEGA_FISSO != "" || strings.TrimSpace(cfg().ModelloAiuto) != "",
		"parametriB": parametriMiliardi(m),
		"tettoB":     tettoModellinoB,
		"ripiego":    aiutoRipiego,
	})
}

func apiSpiega(w http.ResponseWriter, r *http.Request) {
	var req reqSpiega
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domanda == "" {
		errJSON(w, "domanda mancante")
		return
	}
	// Mini-RAG: prendo dal manuale solo i pezzi che c'entrano con la domanda,
	// e ci aggiungo sempre la fotografia dello stato attuale della macchina.
	var ctx strings.Builder
	ctx.WriteString(REGOLE)
	ctx.WriteString("\n\n" + statoLive())
	docs := recupera(req.Domanda, 3)
	if len(docs) == 0 {
		docs = CONOSCENZA[:2] // almeno "a cosa serve il pannello"
	}
	ctx.WriteString("\nDAL MANUALE DEL PANNELLO:\n")
	for _, d := range docs {
		ctx.WriteString("\n## " + d.Titolo + "\n" + d.Testo + "\n")
	}

	corpo, _ := json.Marshal(map[string]any{
		"model": modelloAiuto(),
		"messages": []any{
			map[string]any{"role": "system", "content": ctx.String()},
			map[string]any{"role": "user", "content": trunc(req.Domanda, 500)},
		},
		"max_tokens":  400,
		"temperature": 0.2,
	})
	cl := &http.Client{Timeout: 10 * time.Minute}
	resp, err := cl.Post("http://127.0.0.1:"+itoa(portaAiuto())+"/v1/chat/completions",
		"application/json", bytes.NewReader(corpo))
	if err != nil {
		errJSON(w, "il mini-modello non risponde: "+err.Error())
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
	scriviJSON(w, map[string]any{"ok": true, "risposta": testo})
}

// portaAiuto: su quale runtime vive il modello scelto per l'aiuto.
func portaAiuto() int {
	m := modelloAiuto()
	for _, rt := range scopriRuntime() {
		for _, id := range rt.Modelli {
			if id == m {
				return rt.Porta
			}
		}
	}
	return 8000
}
