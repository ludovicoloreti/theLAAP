package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Senza nomi di modelli scritti nel codice, un modello mai visto si presenta con
// il suo identificativo grezzo — illeggibile. Invece di reintrodurre un elenco
// fisso, chiediamo al modellino locale di proporre un nome breve a partire da
// quello che sappiamo davvero: identificativo, indizi dedotti, velocità misurata,
// dimensione. Resta una proposta: si può correggere a mano, e l'etichetta scritta
// dall'utente vince sempre.

// fattiDi: quello che sappiamo per certo di un modello, in prosa. È l'unico
// materiale che il modellino riceve: non gli si chiede di ricordare, gli si
// chiede di riassumere.
func fattiDi(s Scheda) string {
	var tratti []string
	for _, i := range s.Indizi {
		tratti = append(tratti, i.Tratto)
	}
	fatti := fmt.Sprintf("identificativo: %s\nprogramma che lo esegue: %s", s.ID, s.Runtime)
	if len(tratti) > 0 {
		fatti += "\ncaratteristiche dedotte dal nome: " + strings.Join(tratti, ", ")
	}
	if s.Misurato {
		fatti += fmt.Sprintf("\nvelocità misurata: %.0f token al secondo", s.TokS)
	}
	if s.GB > 0 {
		fatti += fmt.Sprintf("\noccupa %.0f GB", s.GB)
	}
	if s.Reasoning {
		fatti += "\nragiona prima di rispondere"
	}
	if s.Context > 0 {
		fatti += fmt.Sprintf("\ncontesto: %d mila token", s.Context/1024)
	}
	return fatti
}

// chiediAlModellino: una domanda al modello locale dell'aiuto. Sta in una
// funzione sola perché etichetta e descrizione differiscono nelle regole e nel
// tetto di parole, non nel modo di chiedere.
func chiediAlModellino(regole, fatti string, maxTok int) (string, error) {
	corpo, _ := json.Marshal(map[string]any{
		"model": modelloAiuto(),
		"messages": []any{
			map[string]any{"role": "system", "content": regole},
			map[string]any{"role": "user", "content": fatti},
		},
		"max_tokens":  maxTok,
		"temperature": 0.3,
	})
	cl := &http.Client{Timeout: 5 * time.Minute}
	resp, err := cl.Post("http://127.0.0.1:"+itoa(portaAiuto())+"/v1/chat/completions",
		"application/json", bytes.NewReader(corpo))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", err
	}
	scelte, _ := v["choices"].([]any)
	if len(scelte) == 0 {
		return "", fmt.Errorf("nessuna risposta")
	}
	c0, _ := scelte[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	testo, _ := msg["content"].(string)
	if strings.TrimSpace(testo) == "" {
		return "", fmt.Errorf("proposta vuota")
	}
	return testo, nil
}

// ── i nomi devono essere diversi fra loro ─────────────────────────────────
//
// Un elenco in cui tre modelli si chiamano tutti «Analisi testi lunghi» non
// serve a niente: il nome esiste per distinguere, e tre nomi uguali sono peggio
// di nessun nome, perché sembrano un'informazione. Succedeva perché al modellino
// non veniva detto quali nomi erano già presi — ha i fatti per differenziare
// (identificativo, tratti, velocità, peso), gli mancava il vincolo.

// chiaveEtichetta: la forma su cui due nomi si confrontano. Maiuscole, accenti,
// trattini e spazi doppi non sono differenze: «Chat-Veloce» e «chat veloce» sono
// lo stesso nome, e accettarli entrambi rimetterebbe il problema.
func chiaveEtichetta(s string) string {
	var b []rune
	spazio := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch r {
		case 'à':
			r = 'a'
		case 'è', 'é':
			r = 'e'
		case 'ì':
			r = 'i'
		case 'ò':
			r = 'o'
		case 'ù':
			r = 'u'
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if spazio && len(b) > 0 {
				b = append(b, ' ')
			}
			spazio = false
			b = append(b, r)
			continue
		}
		spazio = true
	}
	return string(b)
}

// etichetteInUso: i nomi già assegnati ad ALTRI modelli, dalla chiave normalizzata
// al nome come si legge. Il modello escluso è quello per cui stiamo proponendo:
// il suo nome attuale non deve impedirgli di riconfermarlo.
func etichetteInUso(ss []Scheda, escludiRuntime, escludiID string) map[string]string {
	out := map[string]string{}
	for _, s := range ss {
		if s.Runtime == escludiRuntime && s.ID == escludiID {
			continue
		}
		if k := chiaveEtichetta(s.Etichetta); k != "" {
			out[k] = s.Etichetta
		}
	}
	return out
}

// Le parole dei fatti che passiamo al modellino, che lui a volte ricopia nel
// nome. «Analisi del contesto e delle regole» non dice un mestiere: ripete quello
// che gli abbiamo dato. Sono le stesse parole che REGOLE vieta all'assistente del
// pannello, per la stessa ragione — chi legge non deve incontrarle.
// Radici, non parole intere: «contesto» al singolare passava e «contesti» al
// plurale no — il test l'ha preso prima che finisse in un nome. «esperti» resta
// intero di proposito: «Esperto di codice» sarebbe un buon nome, ed è «misto di
// esperti» il gergo da tenere fuori.
var paroleTecniche = []string{"token", "contest", "parametr", "esperti", "miliard", "quantizz"}

// etichettaSensata: è utilizzabile come NOME? Il modellino è piccolo, e chiederlo
// nelle regole non basta: risponde con frasi di sei parole e ricopia il gergo.
// Verificato qui, e in caso si richiede — è lo stesso trattamento dei doppioni.
func etichettaSensata(proposta string) error {
	parole := strings.Fields(proposta)
	if len(parole) == 0 {
		return fmt.Errorf("vuoto")
	}
	if len(parole) > 5 {
		return fmt.Errorf("%d parole: è una frase, non un nome", len(parole))
	}
	k := chiaveEtichetta(proposta)
	for _, t := range paroleTecniche {
		if strings.Contains(k, t) {
			return fmt.Errorf("contiene «%s», che è gergo: il nome deve dire il mestiere", t)
		}
	}
	return nil
}

func etichettaLibera(proposta string, presi map[string]string) bool {
	k := chiaveEtichetta(proposta)
	if k == "" {
		return false
	}
	_, c := presi[k]
	return !c
}

// etichettaNuova: chiede un nome finché non ne arriva uno diverso dagli altri.
// Tre tentativi e non di più: ogni giro è una chiamata al modello, e se non
// differenzia in tre volte non differenzia. Meglio nessun nome che un doppione:
// senza etichetta la pagina mostra l'identificativo, che almeno è unico.
func etichettaNuova(s Scheda, presi map[string]string) (string, error) {
	var ultimo error
	for i := 0; i < 3; i++ {
		nome, err := proponiEtichetta(s, presi)
		if err != nil {
			ultimo = err
			continue
		}
		if err := etichettaSensata(nome); err != nil {
			ultimo = fmt.Errorf("«%s»: %w", nome, err)
			continue
		}
		if etichettaLibera(nome, presi) {
			return nome, nil
		}
		ultimo = fmt.Errorf("«%s» è già il nome di un altro modello", nome)
	}
	return "", ultimo
}

func proponiEtichetta(s Scheda, presi map[string]string) (string, error) {
	regole := `Dai un nome breve e utile a un modello di intelligenza artificiale, per un elenco.
REGOLE:
- Da 2 a 4 parole, in italiano, in forma di ruolo: dice A COSA SERVE, non com'è fatto.
- Esempi dello stile giusto: "Scrivere codice", "Chat veloce", "Chat senza filtri", "Lavori lunghi sul codice", "Lettura di immagini", "Trascrizione audio".
- Se è senza filtri, dillo nel nome.
- Non ripetere l'identificativo, non mettere numeri di versione, non mettere virgolette.
- VIETATE queste parole: token, contesto, parametri, esperti, miliardi, quantizzazione.
  Sono i fatti che ti ho dato, non un mestiere: chi legge il nome non li deve vedere.
- Rispondi SOLO col nome, nient'altro.`

	if len(presi) > 0 {
		var nomi []string
		for _, n := range presi {
			nomi = append(nomi, "\""+n+"\"")
		}
		sort.Strings(nomi) // stabile: due giri uguali chiedono la stessa cosa
		regole += "\n- Questi nomi sono GIÀ USATI da altri modelli: " + strings.Join(nomi, ", ") +
			". Scegline uno DIVERSO, e non una variante minima di questi: il nome serve a distinguere."
	}

	testo, err := chiediAlModellino(regole, fattiDi(s), 30)
	if err != nil {
		return "", err
	}
	// ripulisco: capita che aggiunga virgolette, punti o una frase intorno
	testo = strings.TrimSpace(testo)
	if i := strings.IndexAny(testo, "\n"); i > 0 {
		testo = testo[:i]
	}
	testo = strings.Trim(testo, " \"'`.:-—*")
	if len(testo) > 45 {
		testo = testo[:45]
	}
	if testo == "" {
		return "", fmt.Errorf("proposta vuota")
	}
	return testo, nil
}

// proponiNota: due righe su cosa fa questo modello e quando conviene usarlo.
//
// Si genera una volta e si salva in profili.json, come l'etichetta. È la
// ragione per cui il modellino da 1,8 GB non deve stare in RAM per sempre:
// serve a scrivere frasi che non cambiano, non a rispondere in continuazione.
func proponiNota(s Scheda) (string, error) {
	regole := `Descrivi un modello di intelligenza artificiale a chi deve decidere se usarlo.
REGOLE:
- Due frasi al massimo, in italiano, meno di 40 parole in tutto.
- La prima dice a cosa serve; la seconda dice quando conviene o quando no.
- Parla di quello che si vede: peso in memoria, velocità misurata, se ragiona.
- Niente gergo di marketing, niente elenchi, niente virgolette, niente titoli.
- Rispondi SOLO con le due frasi.`

	testo, err := chiediAlModellino(regole, fattiDi(s), 160)
	if err != nil {
		return "", err
	}
	testo = strings.TrimSpace(strings.ReplaceAll(testo, "\n", " "))
	testo = strings.Trim(testo, " \"'`*")
	if testo == "" {
		return "", fmt.Errorf("proposta vuota")
	}
	return trunc(testo, 300), nil
}

// apiEtichettaAuto: propone nome e descrizione e li salva in profili.json.
// Uno alla volta: il modellino è piccolo ma non istantaneo.
//
// Tre modi, tutti dalla query perché il corpo non serve:
//
//	(niente)   riempie solo i buchi: chi non ha nome o non ha descrizione
//	?tutte=1   rifà tutto, anche quello che c'è già
//	?id=…      un modello solo, e lo rifà sempre (il pulsante «ridescrivi»)
//
// I modelli remoti si saltano: girano altrove e il nome glielo dà chi li ospita.
func apiEtichettaAuto(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	uno, tutte := strings.TrimSpace(q.Get("id")), q.Get("tutte") == "1"
	fatte := []map[string]string{}
	for _, s := range schede() {
		if provRemoto(s.Runtime) {
			continue
		}
		if uno != "" && !strings.EqualFold(s.ID, uno) {
			continue
		}
		rifai := tutte || uno != ""
		voce := map[string]string{"id": s.ID, "runtime": s.Runtime}
		if rifai || s.Etichetta == "" {
			// I nomi già presi si rileggono a ogni giro: dentro una sola corsa si
			// assegnano più etichette, e la seconda deve vedere la prima.
			presi := etichetteInUso(schede(), s.Runtime, s.ID)
			if nome, err := etichettaNuova(s, presi); err == nil {
				aggiornaProfilo(s.Runtime, s.ID, func(p *Profilo) { p.Etichetta = nome })
				voce["etichetta"] = nome
			} else {
				voce["etichettaMancata"] = err.Error()
			}
		}
		if rifai || s.Note == "" {
			if nota, err := proponiNota(s); err == nil {
				aggiornaProfilo(s.Runtime, s.ID, func(p *Profilo) { p.Note = nota })
				voce["note"] = nota
			}
		}
		if len(voce) > 2 {
			fatte = append(fatte, voce)
		}
	}
	scriviJSON(w, map[string]any{"ok": true, "fatte": fatte, "quante": len(fatte)})
}
