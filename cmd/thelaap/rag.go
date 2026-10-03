package main

import (
	"fmt"
	"sort"
	"strings"
)

// Mini-RAG: il modellino non deve spiegare l'intelligenza artificiale in generale,
// deve sapere COME FUNZIONA QUESTO PANNELLO e COSA STA SUCCEDENDO ADESSO su questa
// macchina. Quindi: una base di conoscenza scritta a mano + una fotografia dello
// stato generata a ogni domanda, e si passano al modello solo i pezzi pertinenti.

type Doc struct {
	Titolo string
	Testo  string
	Chiavi []string // termini che devono pesare di più del testo normale
}

var KNOWLEDGE = []Doc{
	{
		Titolo: "A cosa serve questo pannello",
		Chiavi: []string{"pannello", "webapp", "app", "sito", "pagina", "aipanel", "serve", "cos'è"},
		Testo: `Questo pannello serve a gestire i modelli AI installati sul Mac senza toccare file di configurazione.
Da qui si fanno quattro cose: vedere quanta memoria stanno usando i modelli, scegliere quali modelli compaiono nei menu di Pi, OpenCode e DeepSeek Harness, cercarne di nuovi su HuggingFace e scaricarli, e accendere o spegnere i modelli: il programma che li esegue parte e si ferma da solo.
Si apre all'indirizzo 127.0.0.1:7070 e parte da solo all'accensione del Mac. Funziona solo da questo computer.`,
	},
	{
		Titolo: "La barra della memoria in alto",
		Chiavi: []string{"barra", "memoria", "ram", "vram", "gb", "occupata", "libera", "alto", "colori"},
		Testo: `La barra colorata in cima rappresenta tutta la memoria del Mac (circa 128 GB, condivisa fra processore e scheda grafica: su questi Mac la RAM e la VRAM sono la stessa cosa).
Ogni segmento colorato è un programma che in questo momento tiene dei modelli in memoria; passandoci sopra col mouse compare quanti GB occupa e con quali modelli. Lo spazio grigio è quello ancora libero, la parte tratteggiata a destra è la riserva lasciata al sistema.
I modelli si caricano alla prima domanda e possono restare in memoria. Per toglierne uno senza cancellarlo: nella tabella di "Panoramica" spegni l'interruttore sulla sua riga.`,
	},
	{
		Titolo: "Cosa vuol dire che un modello ragiona",
		Chiavi: []string{"ragiona", "ragionamento", "thinking", "pensa", "viola", "etichetta"},
		Testo: `Alcuni modelli, prima di rispondere, scrivono un ragionamento: elencano i passaggi, poi danno la risposta.
Nel pannello lo vedi con l'etichetta viola "ragiona prima di rispondere". Il ragionamento viene tenuto separato dalla risposta, così nella chat leggi solo la conclusione.
Serve sui problemi complicati; sulle domande semplici fa solo perdere tempo. Le opzioni di ragionamento dipendono dal modello e dal client: non dedurle dalla sua dimensione.`,
	},
	{
		Titolo: "Perché ci sono due modelli Gemma",
		Chiavi: []string{"due", "gemma", "doppio", "uguali", "differenza", "chat", "veloce"},
		Testo: `Sono lo stesso tipo di modello in due taglie.
Gemma 26B-A4B e Gemma 31B hanno architetture diverse. Confronta le velocità misurate nelle loro schede e la qualità sulle tue richieste.
Per email, traduzioni e spiegazioni conviene sempre il veloce. Quello accurato ha senso solo se noti che il veloce sbaglia qualcosa. Se non lo usi mai, puoi toglierlo dal menu senza perdere nulla: il file resta sul disco.`,
	},
	{
		Titolo: "Il pulsante Cronometra",
		Chiavi: []string{"prova", "test", "provare", "testare", "pulsante", "bottone", "quanto", "aspetto"},
		Testo: `Il pulsante "Cronometra" manda al modello una domanda vera e cronometra la risposta.
Mentre lavora il pulsante diventa un contatore di secondi. Se il modello non è già in memoria deve prima caricarsi, e per i modelli grandi possono volerci diversi minuti: è normale.
Alla fine dice quanti token al secondo produce e se ragiona prima di rispondere. Se scopre che la scheda era sbagliata la corregge da sola, e basta salvare.`,
	},
	{
		Titolo: "Aggiungere un modello nuovo",
		Chiavi: []string{"aggiungere", "nuovo", "scaricare", "huggingface", "hf", "cercare", "installare", "download"},
		Testo: `Nella sezione "Scarica modelli" si cerca su HuggingFace scrivendo cosa serve, per esempio "qwen coder" oppure "gemma".
Vengono mostrati solo i modelli in formato MLX, il formato supportato da questa ricerca, e vengono messi per primi quelli a 8 bit, che sono la qualità scelta per questa macchina.
Premendo Scarica il download parte in sottofondo e si può continuare a usare il Mac. Quando finisce, se serve, riavvia oMLX con la freccia circolare sulla sua riga in "Panoramica", perché è al riavvio che rilegge la cartella dei modelli; dopo di che il modello compare nella tabella.`,
	},
	{
		Titolo: "Cosa sono i programmi",
		Chiavi: []string{"sistema", "programmi", "servizi", "runtime", "mtplx", "omlx", "lm studio", "ollama", "spento", "acceso"},
		Testo: `I modelli non girano da soli: ognuno viene eseguito da un programma di servizio.
MTPLX esegue il modello per il codice ed è il più veloce perché indovina più parole insieme. oMLX esegue i modelli grandi e quelli senza filtri, e tiene in memoria i pezzi delle conversazioni lunghe così non deve rileggerle ogni volta. LM Studio esegue i modelli di chat. Ollama ormai serve solo alla ricerca dentro i documenti.
In "Panoramica" ogni programma è una riga grigia con sotto i suoi modelli. Non serve accenderli a mano: accendendo un modello parte anche il suo programma, e spegnendo l'ultimo modello rimasto il programma si ferma da solo.`,
	},
	{
		Titolo: "Se qualcosa non funziona",
		Chiavi: []string{"non funziona", "problema", "errore", "rotto", "lento", "aiuto", "comando", "risolvere", "507"},
		Testo: `Prima cosa: apri "Controlli" e premi "Controlla ora". Fa una verifica completa e dice cosa non va.
Se un modello non risponde, spegni e riaccendi il suo interruttore in "Panoramica": il suo programma riparte con lui.
Se compare l'errore 507 vuol dire che il modello è troppo grande per lo spazio rimasto: succede quando ci sono altri modelli in memoria, e si risolve spegnendone uno dalla tabella prima di accendere quello grande.
Se tutto è diventato lento, guarda la barra in alto: se è quasi piena, spegni un modello che non stai usando.`,
	},
	{
		Titolo: "I modelli grandi in modalità esclusiva",
		Chiavi: []string{"qwen", "next", "flash", "grande", "esclusivo", "attiva"},
		Testo: `I modelli configurati come esclusivi si avviano uno alla volta, liberando prima la memoria degli altri programmi. Leggi nomi e stato attuale nell'elenco dei programmi e dei modelli, senza presumere che un modello sia installato.
Un modello spento per la modalità corrente non è guasto. Per avviarlo accendi il suo interruttore in "Panoramica": prima di partire il pannello mostra quali programmi verranno spenti e chiede conferma. Per fermarlo spegni lo stesso interruttore.`,
	},
	{
		Titolo: "Cosa succede quando salvo",
		Chiavi: []string{"salva", "salvare", "modifiche", "pi", "opencode", "menu", "configurazione"},
		Testo: `Il pannello scrive la lista dei modelli in tre client: Pi, OpenCode e DeepSeek Harness.
Usano formati diversi, ma non è un problema tuo: il pannello traduce fra JSON e YAML e permette di scegliere separatamente in quali menu compare ogni modello.
Prima di ogni salvataggio viene fatta una copia di sicurezza, e se qualcosa non torna il salvataggio viene annullato invece di scrivere un file rotto. Togliere un modello dal menu non cancella nulla dal disco.`,
	},
	{
		Titolo: "L'etichetta «non disponibile»",
		Chiavi: []string{"non disponibile", "disponibile", "grigio", "rosso", "manca", "sparito", "fantasma"},
		Testo: `Vuol dire che quel modello è scritto nella tua lista, ma in questo momento nessun programma lo sta offrendo.
Le cause sono due. O il programma che dovrebbe eseguirlo è spento: in "Panoramica" si vede dalla riga del programma, e accendendo l'interruttore del modello parte anche lui.
Oppure il file del modello non c'è più sul disco — succede se è stato cancellato, o se è stato scaricato ma il programma non è stato riavviato dopo: è al riavvio che rilegge la cartella.
Finché è così, sceglierlo in Pi o OpenCode dà errore. Puoi toglierlo dalla lista senza cancellare niente dal disco.`,
	},
	{
		Titolo: "L'etichetta «sempre in memoria»",
		Chiavi: []string{"sempre in memoria", "residente", "sempre attivo", "non si scarica", "occupa sempre"},
		Testo: `Il programma MTPLX funziona diversamente dagli altri: carica il suo modello all'accensione e lo tiene lì, sempre.
Non è un difetto ed è voluto: quel programma indovina più parole insieme per andare più veloce, e per farlo deve tenere pronte delle parti in più del modello. Se lo scaricasse a ogni pausa perderebbe il suo vantaggio.
Conseguenza pratica: finché MTPLX è acceso, quei GB restano occupati anche se non lo stai usando. Per liberarli spegni l'interruttore del suo modello in "Panoramica": si ferma anche MTPLX. Oppure usa "Ferma tutto".`,
	},
	{
		Titolo: "L'etichetta «velocità sconosciuta»",
		Chiavi: []string{"velocità sconosciuta", "sconosciuta", "non misurata", "tok/s", "quanto va", "velocità"},
		Testo: `Vuol dire semplicemente che quel modello non è mai stato provato da questo pannello, quindi non sappiamo quanto vada.
Premi «Cronometra» sulla sua scheda: gli manda una domanda vera, cronometra la risposta e scrive il risultato. Da quel momento la velocità resta lì, con la data della misura.
La misura serve anche a un'altra cosa: capisce da sola se quel modello ragiona prima di rispondere, e corregge la scheda se era sbagliata.`,
	},
	{
		Titolo: "Le etichette sui tratti del modello",
		Chiavi: []string{"senza filtri", "a esperti", "per il codice", "legge immagini", "molto piccolo", "tratti", "etichette"},
		Testo: `Sono indizi ricavati dal nome del modello, non certezze: chi pubblica un modello mette nel nome delle sigle che dicono com'è fatto.
«senza filtri» vuol dire che gli sono stati tolti i rifiuti, quindi non respinge le richieste. «a esperti» indica un modello con tanti parametri totali ma pochi usati per ogni parola: pesa in memoria ma è veloce. «per il codice» che è stato addestrato apposta sulla programmazione. «legge immagini» che sa estrarre testo dalle foto. «molto piccolo» che è leggero e rapido ma meno capace.
Passando il mouse sopra ognuna c'è scritto anche da quale parte del nome è stata dedotta.`,
	},
	{
		Titolo: "Ferma tutto — il freno d'emergenza",
		Chiavi: []string{"ferma tutto", "panic", "emergenza", "blocca", "libera memoria", "riaccendi"},
		Testo: `Il pulsante "Ferma tutto", sopra la tabella di "Panoramica", spegne di colpo tutti i programmi che tengono i modelli in memoria: LM Studio, oMLX, MTPLX, e scarica quelli di Ollama.
Serve quando il Mac è impantanato e serve memoria subito, senza aspettare che i modelli si liberino da soli.
I modelli smettono di rispondere finché non ne riaccendi uno dal suo interruttore, o premi "Riaccendi tutto". Non viene cancellato niente.`,
	},
	{
		Titolo: "I modelli che non vedi nel pannello",
		Chiavi: []string{"ocr", "whisper", "altri", "nascosti", "diffusiongemma", "embedding", "mancano"},
		Testo: `Sul Mac ci sono anche modelli che non compaiono in questa lista, ed è voluto: servono ad altri programmi, non agli assistenti di scrittura codice.
Sono i modelli che leggono il testo dalle immagini e dai PDF, quello che trascrive l'audio delle riunioni, e quelli che servono a cercare dentro i documenti. Li usano altri strumenti sul Mac.
Non toccarli: occupano poco e servono a lavori che stai già facendo.`,
	},
}

// QUESTIONS: il repertorio da cui la pagina pesca i suggerimenti, a rotazione.
// Meglio tante e varie che quattro sempre uguali: servono a far scoprire cosa
// si può chiedere.
var QUESTIONS = []string{
	"Cosa c'è in memoria adesso?",
	"Quanta memoria mi resta libera?",
	"Perché ci sono due modelli Gemma?",
	"Quale modello uso per scrivere una email?",
	"Quale modello è il più veloce che ho?",
	"Cosa vuol dire che un modello ragiona prima di rispondere?",
	"A cosa serve il pulsante Prova?",
	"Come aggiungo un modello nuovo?",
	"Dove finiscono i modelli che scarico?",
	"Un modello non risponde, cosa faccio?",
	"Cosa succede quando premo Salva?",
	"Perché i modelli grandi si avviano uno alla volta?",
	"Quanto ci mette il modello grande a caricarsi?",
	"Cos'è l'errore 507?",
	"Che differenza c'è fra MTPLX e oMLX?",
	"A cosa serve Ollama se non lo uso per la chat?",
	"Cosa fa il pulsante Controlla tutto?",
	"Come faccio a sapere se ci sono aggiornamenti?",
	"Il Mac è lento, cosa controllo?",
	"Perché alcuni modelli sono senza filtri?",
	"Quanto spazio occupano i modelli sul disco?",
	"Posso togliere un modello senza cancellarlo?",
	"Cosa vuol dire 8 bit in un modello?",
	"Che differenza c'è fra RAM e VRAM su questo Mac?",
	"Se spengo un programma, cosa smette di funzionare?",
	"Come torno indietro se sbaglio una modifica?",
	"Perché un modello grande può essere più veloce di uno piccolo?",
	"Quanti modelli posso tenere accesi insieme?",
}

// liveState: la fotografia di adesso. Senza questa il modellino risponderebbe
// in generale, mentre le domande sono quasi sempre "cosa c'è ADESSO in memoria".
func liveState() string {
	var b strings.Builder
	m := readsMemory()
	b.WriteString("SITUAZIONE DI QUESTO MOMENTO SU QUESTO MAC:\n")
	b.WriteString(fmt.Sprintf("- memoria totale %.0f GB, libera %.0f GB\n", m.TotaleGB, m.LiberaGB))
	if len(m.Caricati) == 0 {
		b.WriteString("- in questo momento NESSUN modello è caricato in memoria\n")
	} else {
		b.WriteString("- modelli caricati in memoria adesso:\n")
		for _, c := range m.Caricati {
			b.WriteString(fmt.Sprintf("    · %s, eseguito da %s, occupa %.1f GB\n", c.Nome, c.Runtime, c.GB))
		}
	}
	if m.CeilingGB > 0 {
		b.WriteString(fmt.Sprintf("- un singolo modello non può superare %.0f GB\n", m.CeilingGB))
	}
	if m.SwapUsatoGB > 5 {
		b.WriteString(fmt.Sprintf("- il Mac sta usando %.0f GB di memoria virtuale su disco\n", m.SwapUsatoGB))
	}
	for _, a := range m.Avvisi {
		b.WriteString("- ATTENZIONE: " + a + "\n")
	}

	b.WriteString("\nPROGRAMMI DI SERVIZIO:\n")
	nomi := map[string]string{"mtplx": "MTPLX (esegue il modello per il codice)",
		"omlx":     "oMLX (esegue i modelli grandi e senza filtri)",
		"lmstudio": "LM Studio (esegue i modelli di chat)", "ollama": "Ollama (ricerca nei documenti)"}
	for _, r := range discoverRuntimes() {
		stato := "SPENTO"
		if r.Attivo {
			stato = "acceso"
		}
		nome := nomi[r.Chiave]
		if nome == "" {
			nome = r.Nome
		}
		if nome == "" {
			nome = r.Chiave
		}
		b.WriteString(fmt.Sprintf("- %s: %s\n", nome, stato))
	}

	if modelli, _ := configState(); len(modelli) > 0 {
		b.WriteString("\nMODELLI NEI MENU DEI CLIENT ADESSO:\n")
		for _, mm := range modelli {
			if mm.InPi || mm.InOC || mm.InDSH {
				r := ""
				if mm.Reasoning {
					r = ", ragiona prima di rispondere"
				}
				b.WriteString(fmt.Sprintf("- %s (eseguito da %s%s)\n", mm.ID, mm.Runtime, r))
			}
		}
	}
	b.WriteString(`

AZIONI REALI DEL PANNELLO:
- Per accendere o spegnere un modello: "Panoramica" > interruttore sulla riga del modello. Il programma che lo esegue parte e si ferma da solo.
- Per vedere i dettagli di un modello, cronometrarlo o archiviarlo: "Panoramica" > clic sulla sua riga; la scheda si apre a destra.
- Per riavviare o spegnere a mano un programma: "Panoramica" > le due icone a destra sulla riga grigia del programma.
- Per spegnere tutto subito: "Panoramica" > "Ferma tutto".
- Per scegliere in quali menu compare un modello (Pi, OpenCode, DeepSeek Harness): "Client".
- Per eseguire la verifica generale: "Controlli" > "Controlla ora".
- Spegnere un modello non cancella file e non cambia Pi o OpenCode. "Archivia" sposta invece i file fuori dalle cartelle attive.
- Non esistono sezioni chiamate "Modelli", "Programmi", "Memoria unificata" o "Sistema".

COSA SUCCEDE AL PROGRAMMA QUANDO SI SPEGNE UN SUO MODELLO:
`)
	b.WriteString(stopActions(knownRuntimes(), "it"))
	b.WriteString("\nThe same, in English:\n" + stopActions(knownRuntimes(), "en"))
	return b.String()
}

var STOP = map[string]bool{"il": true, "lo": true, "la": true, "i": true, "gli": true, "le": true,
	"di": true, "a": true, "da": true, "in": true, "con": true, "su": true, "per": true, "tra": true,
	"e": true, "che": true, "un": true, "una": true, "del": true, "della": true, "dei": true,
	"come": true, "cosa": true, "quale": true, "quali": true, "mi": true, "si": true, "ho": true,
	"è": true, "sono": true, "ci": true, "non": true, "se": true, "ma": true, "o": true, "al": true}

func parole(s string) []string {
	s = strings.ToLower(s)
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == 'à' || r == 'è' ||
			r == 'é' || r == 'ì' || r == 'ò' || r == 'ù' || r == '\'')
	}) {
		p = strings.Trim(p, "'")
		if len(p) > 2 && !STOP[p] {
			out = append(out, p)
		}
	}
	return out
}

// recupera: scoring per sovrapposizione di parole; le "chiavi" pesano di più.
func recupera(domanda string, quanti int) []Doc {
	q := parole(domanda)
	type voce struct {
		d     Doc
		punti int
	}
	var vv []voce
	for _, d := range KNOWLEDGE {
		testo := strings.ToLower(d.Titolo + " " + d.Testo)
		p := 0
		for _, w := range q {
			if strings.Contains(testo, w) {
				p += 1
			}
			for _, k := range d.Chiavi {
				if strings.Contains(k, w) || strings.Contains(w, k) {
					p += 4
					break
				}
			}
		}
		if p > 0 {
			vv = append(vv, voce{d, p})
		}
	}
	sort.SliceStable(vv, func(i, j int) bool { return vv[i].punti > vv[j].punti })
	var out []Doc
	for i := 0; i < len(vv) && i < quanti; i++ {
		out = append(out, vv[i].d)
	}
	return out
}
