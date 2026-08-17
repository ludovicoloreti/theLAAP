<div align="center">

# theLAAP

### Il pannello di amministrazione dell'AI locale

**Un posto solo per vedere e governare tutti i modelli che girano sul tuo Mac.**

Memoria unificata misurata sui processi, non sui file. Un arbitro che rifiuta un
caricamento prima che porti giù la macchina. Due assi per modello, calcolati una
volta sola sul server.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Swift](https://img.shields.io/badge/Swift-6.3-F05138?logo=swift&logoColor=white)](https://swift.org)
[![macOS](https://img.shields.io/badge/macOS-13%2B-000000?logo=apple&logoColor=white)](#-installazione)
[![Dipendenze](https://img.shields.io/badge/dipendenze-1-brightgreen)](go.mod)
[![Test](https://img.shields.io/badge/test-73-success)](#i-file)
[![Licenza](https://img.shields.io/badge/licenza-MIT-blue)](LICENSE)

[English](README.md) · **Italiano**

</div>

---

App per macOS in due pezzi: una **voce nella barra dei menu** (Swift) e un **pannello web** in Go su **http://127.0.0.1:7070**. L'unica dipendenza del server è il parser YAML usato dall'editor delle configurazioni.

```bash
./build.sh              # solo i binari
./build.sh --app        # crea theLAAP.app qui accanto
./build.sh --install    # la mette in /Applications
./build.sh --desktop    # la mette sulla Scrivania
```

Aprendola compare l'icona nella barra in alto, col numero di GB che i modelli
stanno occupando — misurato sui processi, lo stesso numero che legge il pannello.
Dal menu: cosa c'è in memoria, accendi e spegni i programmi, attiva il modello
grande, cerca aggiornamenti, e apri il pannello completo.

Il pannello si apre in una **finestra vera** dell'app: `NSWindow` con una
`WKWebView` dentro, semafori di sistema e la barra dei menu in alto. Nessun
browser di mezzo, nessuna barra degli indirizzi. Finché la finestra è aperta
l'app compare anche nel Dock; chiudendola torna a vivere solo nella barra di
stato, e il server continua a girare.

## Due modalità

L'interruttore in alto a destra commuta fra:

- **guidato** — ogni cosa spiegata in italiano, senza gergo: a cosa serve un modello, quando usarlo, cosa fa un pulsante e quanto ci mette.
- **esperto** — in più: identificativi completi, programma e porta, dimensione del contesto e le righe di comando degli strumenti.

La scelta resta memorizzata.

## Come è fatta la pagina

Tre colonne, e ognuna risponde a una domanda diversa: a sinistra **dove sono**, in
mezzo **cosa succede**, a destra **cosa faccio con questo**. In cima, un campo
`⌘K` che accetta un comando o una frase; in fondo, i numeri della macchina, il
tema e la lingua.

**Una schermata, una cosa.** La memoria e l'elenco dei modelli sono due
schermate separate, e non è un dettaglio di gusto: quando erano la stessa,
cambiare filtro cambiava solo la tabella in fondo, e per vedere gli «esclusivi»
bisognava scorrere oltre il titolo, la barra della RAM, le righe dei processi, la
lettura della macchina e la scheda della regola. Misurati: 1036 px, ogni volta,
per arrivare a una lista che a volte è vuota.

| schermata | cosa contiene |
|---|---|
| **Memoria unificata** | barra, processi, lettura della macchina, regola col glossario |
| **Modelli** (Tutti · Conviventi · Esclusivi · Spenti · Remoti) | titolo, filtri, tabella. Sopra, una riga sola coi due numeri che servono a capire i verdetti — ed è il collegamento alla memoria |

Cambiando schermata si torna in cima, e **solo** cambiando: il disegno gira anche
ogni cinque secondi col polling, e riportare su la pagina mentre qualcuno legge
sarebbe peggio del problema che risolve.

**Due assi, non uno.** Ogni modello ha uno *stato* — com'è adesso — e una
*classe* — come può convivere con gli altri:

| | |
|---|---|
| **stato** | `pronto` · `in-memoria` · `spento` · `in-arrivo` · `guasto` · `remoto` |
| **classe** | `convivente` · `esclusivo` · `residente` · `remoto` |

Ogni etichetta è cliccabile e si spiega da sé, con l'elenco di chi ci sta dentro
in quel momento. Le due parole non sono sinonimi: un modello può essere `spento`
e `esclusivo`, cioè non caricato e tale che, caricandolo, pretende la macchina.

**Stato e classe li calcola il server, in `states.go`.** La pagina li legge da
`/api/modelli` e non li rifà: la soglia oltre la quale un modello è `esclusivo` è
la stessa `SogliaGrandeByte` che `budget.go` usa per rifiutare il secondo modello
grande. Con due calcoli separati il pannello finirebbe per scrivere «convivente»
su un modello che l'arbitro rifiuta — e nessuno se ne accorgerebbe.

Per la stessa ragione «se lo carico adesso, ci sta?» non è un conto della
pagina: è il **verdetto** che arriva con la scheda, quello del preflight, con
dentro quanto manca e cosa fermare.

**Tre numeri diversi, detti per quello che sono.** *Occupati* è misurato sui
processi, non sui file (mtplx dichiara 29,3 GB e ne occupa 84,8). *Liberi* è il
resto aritmetico, quello che la barra disegna. *Liberi per un modello nuovo* è
meno: sottrae i 24 GB tenuti da parte per il sistema operativo, e la differenza
è esattamente ciò che ha evitato il secondo kernel panic.

**`⌘K`, senza modello.** Un interprete deterministico di poche righe: capisce
l'azione, la quantità in GB, il modello e il programma, mostra cosa ha capito, e
propone *una* cosa da confermare con invio. Gli id eseguibili non sono scritti
nella pagina: vengono da `/api/comandi`, che li ricava da strumenti, regimi e
programmi dichiarati in configurazione, ognuno con la rotta e il corpo da usare.
È il difetto del 16/08/2026 affrontato alla radice — un elenco scritto a mano
nella pagina è un elenco che prima o poi non combacia col server.

**Lo stesso registro lo legge la voce nella barra dei menu**, che costruisce le
sue voci da `/api/comandi` ed esegue sulla rotta che ogni voce dichiara. Nello
Swift non c'è nessun id e nessuna rotta: prima ce n'erano quattro, tenuti
allineati al sorgente Go da un test statico. Tenere allineati due elenchi è
meglio che non farlo, ma resta un elenco di troppo. Ora
`menubar_contract_test.go` verifica il contrario — che nello Swift non
ricompaia nessun id — e che il registro venga chiesto davvero.

Effetto collaterale utile: i regimi compaiono nel menu della barra, dove non
c'erano mai stati, e le etichette sono quelle della configurazione.

**Niente `confirm()`.** Le azioni distruttive si armano al primo clic e dicono
cosa faranno — quali programmi fermano, quanta memoria liberano — e il secondo
clic esegue.

**Si ferma quando non la guardi.** L'aggiornamento ogni cinque secondi è sospeso
mentre la finestra non è visibile, e riprende su `visibilitychange`: un pannello
aperto e dimenticato non deve tenere sveglia la macchina.

**Italiano e inglese.** La lingua parte da quella del browser e si cambia in
fondo a destra. Nota onesta: `cosa`, `nota`, gli avvisi e le schede del manuale
arrivano dal server e dalla configurazione, e sono scritti in italiano — in
inglese la schermata resta in parte italiana finché quelle stringhe non
diventano chiavi da tradurre.

## Cosa c'è dentro

**La memoria.** Una barra che rappresenta la memoria del Mac, con un blocco colorato per ogni programma che ne tiene: passandoci sopra vedi nome, GB occupati e — se differiscono — quanto pesano i file rispetto a quanto tiene il processo. Sui Mac Apple Silicon RAM e VRAM sono la stessa memoria, quindi questa barra è tutto quello che serve sapere. La riga tratteggiata è il tetto per un solo modello, e si disegna **solo** se il programma lo dichiara: zero vuol dire «non lo so», non «nessun limite».

**L'elenco dei modelli.** Una riga per ciascuno: stato, nome, classe, peso, velocità. Il nome dice **cosa fa e quando usarlo** in italiano — non l'identificativo tecnico. Un peso a zero si scrive «—», non «0,0 GB»: non è zero, è che non lo sappiamo, e sarebbe l'unica cifra falsa della tabella.

Cliccando una riga, la colonna di destra: la descrizione, la tabella delle caratteristiche, il verdetto «se lo carichi adesso», e le azioni. **Cronometra** manda una domanda vera al modello e misura, con un contatore nel pulsante; alla fine rileva da solo se il modello ragiona prima di rispondere. **Nomi e client** cambia nome e identificativo e sceglie separatamente se mostrarlo in Pi e OpenCode. **Archivia** lo toglie dal disco conservando percorso e configurazione: **Ripristina** rimette tutto a posto, e la cancellazione definitiva è disponibile soltanto dall'archivio e richiede due conferme.

**Editor JSON/YAML.** Il file principale di theLAAP e tutti i file dei client dichiarati in configurazione sono modificabili direttamente nel pannello. Ogni file si può vedere e modificare sia come JSON sia come YAML, pur venendo salvato nel formato originale. L'editor valida sintassi e struttura, formatta il JSON, inserisce gli spazi col tasto Tab, salva con `⌘S`/`Ctrl-S`, crea un backup e rileva se un altro programma ha modificato il file nel frattempo.

**Aggiungi un modello.** Ricerca diretta su HuggingFace: scrivi "qwen coder" e vedi cosa c'è, con dimensione reale, formato e quanti l'hanno scaricato. Vengono mostrati solo i formati MLX che questo Mac sa eseguire, con in testa quelli a 8 bit. Il download va in sottofondo.

**L'aiuto, in un pannello laterale.** Il modellino locale che gira su questo Mac, con davanti un piccolo RAG: **sedici** schede di manuale scritte a mano più **la fotografia dello stato reale, rigenerata a ogni domanda**. Chiedigli "cosa c'è in memoria adesso" e risponde coi numeri veri. Sotto la casella ci sono suggerimenti pescati da **28 domande**.

**Quale sia il modellino lo decide il peso, non il nome.** Lo dice `/api/aiuto`, e il pannello lo nomina in fondo alla colonna di sinistra col suo peso e il suo stato. La regola: fra i modelli serviti, il più piccolo che sappia conversare, sotto gli 8 miliardi di parametri.

I parametri si leggono dal nome, e sono quelli **totali** a contare. In `gemma-4-26b-a4b` la sigla «a4b» sono i parametri *attivi* di un modello a esperti: dicono la velocità, non il peso — in memoria ce ne stanno 26. Ignorata anche la quantizzazione, perché `-8bit` non sono 8 miliardi. Un nome che dichiara solo gli attivi vale zero e non è eleggibile: contarlo vorrebbe dire chiamare 3B un modello che può pesarne trenta.

Chi non sa conversare è escluso — OCR, embedding, trascrizione, diffusione — e a dirlo è `indizi()` in `profiles.go`, la stessa tabella che l'interfaccia mostra accanto al modello: un secondo elenco di parole chiave qui divergerebbe dal primo.

Se fra i modelli serviti non c'è niente di piccolo si usa quello che c'è — senza aiuto il pannello perde le descrizioni e la chat — ma **la barra laterale lo scrive**, invece di mostrare un 26B come se fosse normale. Per non lasciarlo decidere alla macchina: `"modelloAiuto": "id-del-modello"` in configurazione, che ha la precedenza.

**Le descrizioni si scrivono una volta.** Il riquadro «descritto dal modellino» legge il campo `note` di `profili.json`; il pulsante ↻ lo rigenera per quel modello, *Fai descrivere tutto* riempie i buchi. Sono frasi che non cambiano: generate una volta, il modellino può restare scaricato e la RAM torna ai modelli che lavorano.

**Due modelli non possono chiamarsi allo stesso modo.** È capitato: tre modelli diversi tutti «Analisi testi lunghi», perché al modellino non era stato detto quali nomi fossero già presi. Ora il prompt li elenca, e se torna un doppione si richiede — tre volte, poi il nome non si scrive: senza etichetta la pagina mostra l'identificativo, che almeno è unico. Il confronto ignora maiuscole, accenti, trattini e spazi doppi.

Chiederlo nelle regole non basta, e si è visto: sono uscite «Esperti analisi token lunghe» e «Analisi del contesto e delle regole» — sei parole, e il gergo dei fatti che gli avevamo passato. Ora è verificato in codice: massimo cinque parole, e nessuna delle radici `token`, `contest`, `parametr`, `esperti`, `miliard`, `quantizz`. Chi non passa viene richiesto come un doppione.

**Programmi.** Acceso/spento e riavvio dei programmi che eseguono i modelli. I pulsanti che si vedono sono quelli che funzionano: `avvia`, `ferma` e `riavvia` li decide `comandoServizio`, la stessa funzione che poi li esegue, e le righe di shell non escono verso il browser.

**Manutenzione.** Gli strumenti dichiarati in configurazione — ognuno con scritto cosa fa e quanto ci mette, dall'occhiata di 2 secondi al controllo completo che misura ogni modello. Fra questi *Installa aggiornamenti*, che aggiorna i programmi e li riavvia (i modelli no: quelli pesano decine di GB e li scarichi tu). L'output arriva in diretta e si legge dall'alto in basso come in un terminale, ripulito dalle sequenze di colore ANSI — che altrimenti uscirebbero come `[92m` in mezzo alle frasi.

**Regimi.** In alto, accanto a «Ferma tutto», i pulsanti che accendono e spengono una configurazione di macchina *tutta insieme*. Il caso per cui sono nati: un modello da 89 GB su un Mac da 128 ci sta comodamente da solo, ma non insieme a un secondo server da 79 GB — e anche da solo veniva rifiutato, non per mancanza di memoria ma per i margini che il programma si impone. Il regime ferma gli altri programmi **e poi** allarga quei margini: l'ordine non è estetico, perché allargarli con un altro modello ancora residente è precisamente la configurazione che fa bloccare la macchina. Passandoci sopra col mouse vedi in anticipo cosa verrà fermato, e il primo clic te lo dice invece di eseguire.

I regimi si dichiarano in `~/.config/thelaap/configurazione.json` — il programma non ne conosce nessuno:

```json
{
  "regimi": [
    { "chiave": "esclusivo", "nome": "Un modello solo",
      "cosa": "Ferma gli altri programmi e allarga i margini di memoria",
      "runtimeAttivo": "omlx",
      "attiva": "/percorso/profilo.sh margini-larghi",
      "disattiva": "/percorso/profilo.sh margini-prudenti",
      "segno": "~/.omlx/.esclusivo" }
  ]
}
```

`runtimeAttivo` è l'unico che resta acceso; gli altri li ferma il pannello coi comandi che già conosce. `attiva`/`disattiva` sono facoltativi: senza, il regime si limita a fare spazio. `segno` è il file la cui esistenza dice che il regime è attivo — senza, il pannello non può distinguere «spento» da «non lo so», e lo dichiara spento.

**Aspetto.** Segue il tema del Mac — chiesto a `/api/tema`, perché un browser aperto in modalità applicazione riporta `prefers-color-scheme: light` anche col sistema in scuro — oppure lo forzi chiaro o scuro. La scelta resta memorizzata.

**Stati vuoti che dicono cosa fare.** Macchina senza modelli, senza strumenti di manutenzione, archivio vuoto, un filtro senza risultati, server non raggiungibile: ognuno spiega il perché e la mossa successiva, invece di mostrare una tabella vuota.

## La finestra è una finestra vera

Non è un browser travestito: `NSWindow` con una `WKWebView` dentro, semafori di
sistema, e la barra dei menu in alto — che per come era fatta prima non c'era
(vedi nota 2).

    theLAAP    Informazioni · Nascondi · Nascondi gli altri · Mostra tutti · Esci
    Modifica   Annulla · Ripristina · Taglia · Copia · Incolla · Seleziona tutto
    Pannello   Chiedi o comanda… ⌘K · Aiuto ⌘/
               Memoria ⌘1 · Programmi ⌘2 · Manutenzione ⌘3 · Configurazioni ⌘4
               Ricarica ⌘R
    Vista      Ingrandisci ⌘+ · Riduci ⌘− · Dimensione reale ⌘0 · Schermo intero ⌃⌘F
    Finestra   Riduci a icona ⌘M · Zoom · Chiudi ⌘W

Le voci di **Pannello** non duplicano logica in Swift: chiamano le funzioni della
pagina dentro la webview. Servono a rendere di sistema — visibili, con la
scorciatoia scritta accanto — cose che altrimenti si scoprono per caso.

Il titolo della finestra lo dice la **pagina**, non il sorgente Swift: cablato,
continuava a raccontare la versione di prima, e così segue anche la lingua scelta
dentro il pannello. Due dichiarazioni nell'`Info.plist` (`CFBundleDevelopmentRegion`
e `CFBundleLocalizations`) servono perché le voci che AppKit aggiunge da sé escano
in italiano e non in inglese in mezzo a un menu italiano; e
`allowsAutomaticWindowTabbing = false` perché macOS non offra «Mostra la barra dei
pannelli» a una finestra sola.

Chiudendo la finestra l'app resta nella barra di stato e il server continua a
girare. Riaprendola, riparte da dov'era.

## 📦 Installazione

```bash
git clone https://github.com/ludovicoloreti/theLAAP.git
cd theLAAP
./build.sh --install
```

## Portarlo su un altro computer

Il programma non sa niente della macchina su cui gira: tutto quello che è
specifico sta in **`~/.config/thelaap/configurazione.json`**. Al primo avvio, se
il file non c'è, viene scritto guardando cosa è installato.

```bash
git clone <questo repo> theLAAP && cd theLAAP
./build.sh --install       # macOS: crea l'app
go build -o thelaap ./cmd/thelaap   # Linux e Windows: basta il binario
./thelaap                  # poi apri http://127.0.0.1:7070
```

**Cosa rileva da solo**: Ollama, LM Studio, oMLX, MTPLX, llama.cpp, vLLM — cercandone
l'eseguibile nei posti soliti e, se non lo trova, provando se qualcosa risponde
sulla loro porta. Poi le configurazioni di Pi e OpenCode, se esistono, e gli script
di manutenzione se sono in una cartella nota.

**Cosa NON serve fare**: modificare il codice. Se hai un programma che qui non è
previsto, aggiungi una voce a `runtime` nel file di configurazione: servono nome,
porta e il percorso che elenca i modelli. I comandi per accenderlo e spegnerlo sono
facoltativi — senza, il pannello ne mostra solo lo stato.

```json
{
  "runtime": [
    { "nome": "Il mio server", "chiave": "mio", "porta": 9000,
      "elencoModelli": "/v1/models",
      "avvia": "systemctl --user start mioserver",
      "ferma": "systemctl --user stop mioserver",
      "modelliCaricati": "mio-cli ps" }
  ],
  "modelloAiuto": "gemma-4-E2B-it-MLX-8bit",
  "riservaSistemaGB": 24,
  "sogliaModelloGrandeGB": 40
}
```

`modelloAiuto` impone quale modellino scrive le etichette e risponde alle domande:
vuoto lo sceglie il programma, prendendo il più piccolo che sappia conversare.
`riservaSistemaGB` è quanto si tiene da parte per il sistema operativo, e
`sogliaModelloGrandeGB` è la soglia oltre la quale un modello è «esclusivo» —
la stessa che l'arbitro usa per rifiutare il secondo modello grande, perché due
numeri diversi vorrebbero dire un pannello che promette ciò che il server nega.

### Cosa cambia fra i sistemi

| | macOS | Linux | Windows |
|---|---|---|---|
| memoria | `sysctl` + `vm_stat` (unificata) | `/proc/meminfo` | CIM via PowerShell |
| memoria grafica | `iogpu.wired_limit_mb` | `nvidia-smi` o `rocm-smi` | `Win32_VideoController` |
| tema chiaro/scuro | `AppleInterfaceStyle` | GNOME o KDE | registro di sistema |
| servizi | `launchctl` | `systemctl --user` | comandi diretti |
| voce nella barra | sì (Swift) | no, solo il pannello | no, solo il pannello |

Sono file separati con un vincolo di compilazione (`system_darwin.go`,
`system_linux.go`, `system_windows.go`): il resto del programma non sa su cosa
sta girando. Verificato con la compilazione incrociata per macOS arm64, Linux
amd64 e arm64, Windows amd64.

**Il pannello regge una macchina spoglia**: provato con un solo programma, nessun
client e nessuno strumento — nessun errore, e al posto delle sezioni vuote un
messaggio che dice cosa fare.

## Sicurezza

- Ascolta **solo** su `127.0.0.1` e rifiuta le richieste che non arrivano da localhost: da qui si riavviano servizi.
- **L'intestazione `Host` è controllata su ogni richiesta, letture comprese.** Guardare l'indirizzo di partenza non basta: è il browser dell'utente a mandare la richiesta, quindi resta `127.0.0.1` qualunque pagina l'abbia chiesta. Un dominio che risolve a `127.0.0.1` diventerebbe altrimenti same-origin col pannello e potrebbe leggerne le risposte.
- Tutto ciò che muta stato pretende `Origin` più un token, rigenerato a ogni avvio e mai scritto su disco.
- **Le rotte che restituiscono i file dei client chiedono il token anche in lettura**: è lì che il pannello scrive le chiavi dei provider.
- I comandi eseguibili sono una lista chiusa, risolta per id dalla configurazione locale; nessun testo della richiesta finisce in una shell.
- I nomi dei modelli arrivano da repository di terzi. Dentro un `onclick` sono sfuggiti per **entrambi** i parser, l'attributo HTML e la stringa JavaScript: sfuggirli per uno solo è ciò che ha lasciato dieci pulsanti morti, in silenzio, fino a che qualcuno non ha provato a cliccarli.
- La cancellazione dall'archivio risolve il percorso dentro la radice dell'archivio, rifiutando `..`, i percorsi assoluti e un symlink in qualunque componente.
- Prima di scrivere le configurazioni fa una copia di sicurezza, scrive su file temporaneo e poi rinomina. Rifiuta JSON/YAML non valido e non sovrascrive in silenzio modifiche esterne. Una lista modelli vuota è consentita: serve per poter rimuovere anche l'ultimo modello.
- Lo stato operativo resta nei file di Pi e OpenCode; theLAAP conserva soltanto etichette/misure e i manifesti necessari a ripristinare i modelli archiviati.

## Note tecniche imparate a caro prezzo

Dieci note, e la numero 3 è la correzione di quello che questa stessa sezione affermava.
Lasciarla riscritta invece di cancellarla è il punto: la diagnosi sbagliata era plausibile,
ed è per questo che è costata mesi.

**0. Barra e server sono due programmi separati, e niente li teneva allineati.**
Scoperto il 16/08/2026: le voci del menu nella barra **non facevano niente da tempo**, e
fallivano in silenzio. Quattro cause sovrapposte, tutte legittime prese una per una:

- chiamavano `/api/esegui` in **GET**, ma la rotta accetta solo POST da quando è stata
  chiusa la falla CSRF (`<img src="...?cmd=ferma-tutto">` da qualunque pagina) → **405**;
- mandavano `cmd=laguna-on`, mentre gli strumenti registrati si chiamano
  `modello-grande-on`; e `cmd=stoppa-tutto`, mentre `comandoAmmesso` conosce
  `ferma-tutto`; e `cmd=aiupdate`, mentre l'id vero è `aggiornamenti` → **403**;
- non mandavano né `Origin` né `X-theLAAP-Token`, che `guardia()` pretende su tutto ciò
  che muta stato → «richiesta non partita dal pannello».

Il token vive **solo in memoria**, rigenerato a ogni avvio e mai scritto su disco: si
recupera con una GET su `/` leggendo `<meta name="thelaap-token">`, che la guardia lascia
passare. È quello che fa ora `eseguiComando()`. Il **pannello web non era interessato**:
usa gli id dinamici e fa POST col token.

**Lezione**: due programmi che si parlano via HTTP hanno un contratto, e un contratto non
verificato marcisce. Ora c'è `menubar_contract_test.go`, che legge il sorgente Swift e
controlla metodo, id e etichette. Validato reintroducendo i cinque difetti uno per uno —
la prima versione era da buttare perché chiamava `comandoAmmesso`, che nei test legge una
`cfg()` vuota: falliva sempre, quindi non distingueva il codice giusto da quello rotto.

**Corollario**: dopo aver modificato `~/.config/thelaap/configurazione.json`, il server va
**riavviato** — tiene la configurazione in cache e continua a servire quella vecchia. Basta
chiudere e riaprire l'app: da quando i dati non stanno più sul Desktop (nota 3) `open -a`
funziona, e il server riparte da sé in un secondo.

**1. L'eseguibile del bundle dev'essere il programma, non uno script.**
L'`Info.plist` aveva `CFBundleExecutable = avvia`, uno script shell che poi faceva `exec theLAAP`. Con quella catena la voce nella barra dei menu **nasceva alta 0 pixel e restava invisibile**, pur risultando presente a tutti i controlli: `isVisible = true`, immagine impostata, pulsante 30×27. Lo stesso identico binario lanciato da terminale finiva a `(1109, 1084)` e si vedeva; lanciato tramite lo script, a `(0, 0, 46, 0)`.
Ho dato la colpa alla barra dei menu piena e ho detto che non era risolvibile: era falso. A smascherarlo è stata un'app di prova di venti righe che faceva solo lo status item — quella si vedeva, e il confronto ha isolato la differenza.
**Lezione**: quando una cosa "non si può fare", costruire il caso minimo che funziona e confrontarlo.

**2. La politica di attivazione: `.accessory` per nascere, `.regular` per avere i menu.**
Partendo `.regular` (icona nel Dock) il processo espone due menu bar e lo status item finisce nella seconda, a `-1`: la voce nella barra non si vede. Con `.accessory` ce n'è una sola e la voce sta a `(1116, 4)`.

Ma un'app `.accessory` **non disegna la barra dei menu in alto**, e il `mainMenu` serve soltanto a instradare le scorciatoie. Per anni il pannello non ha avuto un menu.

La via d'uscita è che le due cose non avvengono nello stesso momento: lo status item si crea in `applicationDidFinishLaunching` mentre la politica è `.accessory` — e a quel punto **cambiarla non lo rimuove**. Quindi `.regular` quando la finestra si apre, `.accessory` quando si chiude. Menu in alto, icona nella barra, e nessuna icona nel Dock quando non c'è niente da guardare.

**3. Il server non partiva dall'app, e la firma ad-hoc non era la ragione.**
Questa nota diceva: «è firmato solo ad-hoc, macOS 26 lo esegue solo con un terminale fra gli antenati». Era **sbagliata**, e ha mandato fuori strada per settimane — provati e scartati figlio dell'app, ambiente ripulito, `launchctl submit`, LaunchAgent, `bash -l` dentro l'app, tutti col processo che nasce e non apre mai la porta.

La causa vera, trovata col campionatore: il thread principale fermo dentro `open()`, il 100% dei campioni per quindici secondi e oltre.

```
2365 Thread  DispatchQueue_1: com.apple.main-thread  (serial)
+ 2365 runtime.asmcgocall.abi0
+   2365 runtime.syscallN_trampoline.abi0
+     2365 open  (in libsystem_kernel.dylib)
```

`caricaProfili()` apriva `~/Desktop/AI/theLAAP/profili.json`. **Il Desktop è protetto da TCC**: un figlio di una `.app` che ci apre un file resta appeso ad aspettare un permesso che in quel contesto non compare mai. Da un terminale non si vede, perché il Terminale quel permesso ce l'ha già — ed è per questo che ogni prova col server avviato a mano sembrava perfetta.

Verificato invece di dedotto: `git archive HEAD` in una cartella a parte, compilato il binario di prima, messo quello nel bundle. Identico blocco, log vuoto, porta chiusa. Il difetto era vecchio, e restava coperto da un server acceso una volta e mai riavviato.

Spostati `profili.json` e `backup-config/` in `~/.config/thelaap/`, l'app avvia il proprio server **in un secondo**. Niente firma, niente notarizzazione, niente Terminale.

**Lezione**: «non si può fare» va misurato, non concluso. E una nota tecnica sbagliata costa più di una nota assente, perché la si crede.

**4. Mai far leggere alla app la cartella Scrivania — e vale anche per i dati.**
Cercare lì il binario faceva comparire il dialogo dei permessi di macOS, e finché non si rispondeva **l'app restava congelata**: nessuna finestra, nessun errore, niente. Sembrava rotta. Ora legge solo dentro il proprio bundle.

Questa nota c'era già, e la trappola è tornata comunque da un'altra porta: non l'eseguibile, ma un file di **dati** — `profili.json`, che sta nella cartella del progetto, che sta sul Desktop. Vedi la nota 3. La regola giusta è più larga di come era scritta: **niente di quello che l'app apre all'avvio può stare sotto `~/Desktop`, `~/Documents` o `~/Downloads`.** Per questo nel codice non c'è un ripiego che guardi i percorsi vecchi: basterebbe quello a rimettere il blocco.

**5. Attenzione ai processi fantasma durante lo sviluppo.**
Un `./aipanel` lanciato a mano e mai ucciso ha tenuto la porta 7070 per un'ora mentre le build nuove morivano in silenzio: ore a inseguire un favicon 404 e rotte "inesistenti" che erano solo codice vecchio in esecuzione. Prima di dare la colpa al codice: `lsof -nP -iTCP:7070 -sTCP:LISTEN` e guarda l'ora di avvio.

**6. Una navigazione annullata non è un errore.**
Chiudendo e riaprendo la finestra il pannello diceva «Il pannello non è acceso» con il
server che rispondeva 200. `mostra()` ricarica, e `apriPannello` ricarica di nuovo appena
il server risponde: il secondo caricamento annulla il primo, e `WKWebView` riporta
l'annullamento a `didFailProvisionalNavigation` esattamente come un guasto. Ora
`NSURLErrorCancelled` si ignora. Fra le stesse righe: `isReleasedWhenClosed = false`,
perché l'app sopravvive alla chiusura della finestra e quella finestra va riusata.

**7. Un ripiego senza freno sommerge lo schermo.**
`avviaViaTerminale()` apriva una finestra di Terminale a ogni tentativo, e i tentativi
arrivano da tre punti diversi (l'avvio, l'apertura del pannello, le voci di menu). Viste
otto finestre sovrapposte, tutte con lo stesso errore, mentre si cercava di capire cosa non
andasse. Se un ripiego non funziona al primo colpo non funziona al settimo: ora c'è un
freno di due minuti.

**8. `lms ps` costa 4 secondi** — è Node e riparte ogni volta. Con la pagina che si aggiorna ogni 5 secondi teneva la richiesta sempre in attesa. Ora un lavoratore in sottofondo tiene pronta la fotografia: **da 4100 ms a 0 ms**. E ogni comando esterno ha un tetto di tempo, perché uno che si impianta non deve bloccare il resto.

**9. Sfuggire il testo per un parser solo lascia i pulsanti morti in silenzio.**
La pagina costruisce i gestori come testo HTML, quindi un argomento che finisce
dentro `onclick="..."` deve sopravvivere prima al parser degli attributi e poi a
quello di JavaScript. `JSON.stringify` emette virgolette **doppie**, e
`onclick="scegli('+JSON.stringify(m.id)+')"` viene reso come:

```
onclick="scegli("coder")"
```

Il browser chiude l'attributo alla seconda virgoletta: il gestore diventa
`scegli(` e accanto compare un attributo parassita chiamato `coder")"`. Non è un
errore, non finisce in console, non si vede nel sorgente: è solo un elemento che
al clic non risponde. Dieci punti, fra cui **ogni riga della tabella dei
modelli** e le domande suggerite nel pannello di aiuto.

Non l'avevo visto perché nelle verifiche chiamavo le funzioni da JavaScript
invece di cliccare: `scegli('coder')` funziona benissimo: è l'attributo che non
arriva mai a chiamarla. **Lezione**: una interfaccia si prova cliccandola. Il
resto verifica il modello, non la pagina.

Ora c'è `arg()`, che mette gli apici singoli e sfugge per entrambi i contesti, e
due test che leggono la pagina incorporata. Validati rimettendo il difetto in un
punto solo: falliscono entrambi.

**Corollario, e ricaduta della nota 0.** Cercando i gestori rotti è venuta fuori
un'altra voce che non faceva niente: «Riavvia» nel menu della barra costruiva la
sua `curl` a mano, senza `Origin` e senza token. Il pannello rispondeva **403**,
l'output finiva in `/dev/null`, e la notifica diceva comunque «Riavvio in corso,
ci vuole qualche secondo». `menubar_contract_test.go` non l'ha presa perché
controllava che nello Swift non ricomparissero **id di comando**, e quella riga
non ne cablava uno: cablava una rotta e un corpo. Ora il riavvio viene dal
registro come tutto il resto, cercato per servizio, e se il registro non ce l'ha
la voce resta inerte invece di promettere.

## I file

```
cmd/thelaap/      il programma: server, rotte, pagina incorporata
internal/budget/  l'arbitro della memoria, senza I/O
menubar/          la voce nella barra dei menu e la finestra (Swift)
examples/         uno script di regime, di esempio e adattabile
```

`internal/budget` è l'unico pezzo estratto, e non per simmetria: `budget.go`
importa soltanto `fmt` e `sort` e non usa niente dal resto: il suo commento
promette «qui non si esegue niente e non si legge niente dal sistema», e in un
package quella promessa la impone il compilatore invece del commento.

Il resto sta in un `package main` piatto, e non per pigrizia: **128 dei 228
simboli di pacchetto sono usati fuori dal file che li definisce** — `scriviJSON`
da 17 file, `cfg()` da 13, `errJSON` da 11. Spezzarlo vorrebbe dire esportarne
un centinaio o creare un package-sacco `util`, che è peggio del piatto. È debito
riconosciuto, non una scelta di stile: si paga ridisegnando i confini, non
spostando i file. E `cmd/`/`pkg/` non è uno standard Go ufficiale — il layout
che gira col nome di «standard» dichiara esso stesso di non essere affiliato al
team Go.

| File | Cosa fa |
|---|---|
| `main.go` | server, tabella delle rotte (`rotte()`, quella vera anche per i test), pagina incorporata |
| `discovery.go` | interroga i quattro programmi in parallelo |
| `config.go` | legge e scrive le configurazioni di Pi e OpenCode, traducendo fra i due formati |
| `editor.go` | editor sicuro JSON/YAML, conversione, validazione, backup e controllo dei conflitti |
| `probe.go` | prova un modello: velocità e rilevamento del ragionamento |
| `memory.go` | memoria da `vm_stat`, tetto da oMLX, modelli caricati; con cache e monitor in sottofondo |
| `services.go` | accensione dei servizi e comandi in lista chiusa, con output in diretta |
| `regimes.go` | configurazioni di macchina che si accendono e spengono tutte insieme |
| `internal/budget/budget.go` | l'arbitro: decide se un modello ci sta, prima di caricarlo. Nessun I/O, imposto dal compilatore |
| `states.go` | stato e classe di ogni modello, e il registro dei comandi eseguibili: `/api/modelli` e `/api/comandi` |
| `footprint*.go` | quanto occupa davvero un processo, per sistema operativo |
| `runtimes.go` | cosa sa fare ogni programma: scarico per modello, o solo stop |
| `security.go` | guardia delle rotte: localhost, `Host`, Origin e token |
| `hf.go` | ricerca e scaricamento da HuggingFace |
| `models.go` | esame, archiviazione, ripristino ed eliminazione sicura dei modelli sul disco |
| `rag.go` | il manuale dell'app + la fotografia dello stato, per il modello che risponde |
| `explain.go` | quale modellino risponde (il più piccolo che conversa) e le regole di risposta |
| `labels.go` | nomi e descrizioni scritti dal modellino, distinti fra loro e senza gergo |
| `menubar/theLAAP.swift` | la voce nella barra di stato, la finestra vera e il menu in alto |
| `ui.html` | tutta l'interfaccia, incorporata nel binario |
| `build.sh` | compila, firma, e crea l'app per macOS |
| `start-server.command` | avvio del server da terminale: da quando i dati non stanno sul Desktop è solo un ripiego (nota 3) |

I test dicono cosa non deve tornare a rompersi, e ognuno è stato verificato
rompendo la regola prima di scriverla:

| Test | Cosa difende |
|---|---|
| `budget_test.go` | lo scenario del kernel panic del 27/07/2026 |
| `states_test.go` | stato, classe e registro dei comandi: una fonte sola |
| `helper_test.go` | la taglia si legge dai parametri totali; i nomi sono distinti e non gergo |
| `menubar_contract_test.go` | la barra dei menu non cabla id, legge il registro, e dice lo stesso numero del pannello |
| `security_test.go` | localhost, `Host`, Origin, token, solo POST; e le rotte di configurazione chiuse anche in lettura |
| `editor_test.go` `config_test.go` `models_test.go` `regimes_test.go` `measure_test.go` `server_test.go` | editor, configurazioni, disco, regimi, misure, rotte |

## Cosa non entra nel repository

`.gitignore` esclude i binari compilati (`aipanel`, `menubar/theLAAP`, il bundle `.app`) perché si rifanno con `./build.sh`.

Lo stato locale della macchina non sta più nella cartella del progetto: vive in `~/.config/thelaap/`, accanto alla configurazione.

| Dove | Cosa |
|---|---|
| `~/.config/thelaap/configurazione.json` | la macchina: programmi, percorsi, regimi, limiti di memoria |
| `~/.config/thelaap/profili.json` | le velocità misurate qui, le etichette e le descrizioni scritte dal modellino |
| `~/.config/thelaap/backup-config/` | le copie delle configurazioni dei client prima di ogni scrittura |

**Non è una questione di ordine, è l'unico posto da cui il server può leggere.**
Quando la cartella del progetto sta sul Desktop — e su questa macchina ci sta —
ogni file dentro di essa passa da TCC. Un `aipanel` avviato dalla voce nella
barra dei menu, aprendo `~/Desktop/.../profili.json`, si fermava **dentro la
syscall `open()`**: nessuna riga di log, porta mai aperta, e lo Swift che lo
rilanciava all'infinito credendolo morto. Dal Terminale non si vedeva, perché il
Terminale il permesso sul Desktop ce l'ha già. Il campionatore lo diceva chiaro:
100% dei campioni in `open`, sia col binario di adesso sia con quello di prima.

Per questo nel codice **non** c'è un ripiego che guarda i vecchi percorsi sul
Desktop: rimetterlo rimetterebbe il blocco.

---

## 📄 Licenza

MIT. Vedi [LICENSE](LICENSE).
