# theLAAP — un interruttore per modello, e una pagina a dati

Segue [governo della memoria e ciclo di vita](2026-07-27-governo-memoria-e-ciclo-di-vita-design.md),
di cui usa l'arbitro, la misura sui processi e la tabella delle capacità.

## Perché

Due richieste dello stesso giorno (03/10/2026):

1. **Il programma segue il modello.** Accendere un modello deve avviare da solo il suo
   programma (oMLX, MTPLX, LM Studio, vLLM…); spegnere l'ultimo modello rimasto in un
   programma deve fermare anche il programma. Prima erano due gesti separati, in due schede
   diverse, e toccava alla persona metterli in fila e ricordarsi di fermare il programma
   rimasto acceso a vuoto.
2. **Semplificare.** Troppe sezioni, troppo testo. Dati strutturati → schema → tabelle e
   grafici. Il testo libero solo da Gellow (il modello piccolo), dove serve. La struttura
   grafica a tre colonne, con Gellow che si apre a destra, resta: cambiano sezioni e contenuti.

## Il ciclo di vita (`lifecycle.go`)

Un'unica coppia di rotte, `POST /api/modello/accendi` e `POST /api/modello/spegni`, corpo
`{"runtime","modello"}`. `/api/attiva` è ora un alias di accendi, così chi la chiamava ancora
ottiene anche l'avvio del programma.

**Accendi**

1. Ammissione: se il peso è noto e il modello non è già in memoria, l'arbitro risponde prima
   di avviare qualunque cosa. No → `409` con il verdetto a campi (`codice: non-ci-sta`).
   Vedi «La memoria libera vera» sotto: l'aritmetica sui picchi da sola non basta.
2. Se la porta del programma non risponde: comando `avvia` dalla configurazione, poi attesa
   della porta fino a 2 minuti.
3. Warm-up del modello. Se il modello non parte e il programma l'ha acceso *questa* richiesta,
   e dentro non c'è altro, il programma si rispegne; uno già acceso per conto suo non si tocca.

**Spegni**

1. Programma già spento → niente da fare, non è un errore.
2. Con `scaricaModello` (LM Studio, Ollama): scarica quel modello, usando il nome con cui il
   programma lo tiene (può essere un alias). Poi, se una lettura **fresca** dei modelli
   caricati dice che non resta altro, `ferma` il programma.
3. Senza `scaricaModello` (oMLX, MTPLX, qwen-next): il modello *è* il programma. Si ferma solo
   se dentro non c'è altro; con altri modelli caricati la richiesta è rifiutata
   (`altri-modelli`) e dice quali, perché fermarlo li spegnerebbe tutti.

**Decisioni**

| Decisione | Perché |
|---|---|
| La lettura che decide se fermare è fresca (`loadedIn`), non la fotografia del monitor | La fotografia ha secondi di età: in quel tempo un client può aver caricato un altro modello, e fermare il programma glielo toglierebbe da sotto |
| «Non so cosa c'è dentro» non è «dentro non c'è niente» | Se l'elenco non si legge, il programma resta acceso; se serve fermarlo per spegnere, si rifiuta |
| Registro dei modelli *in caricamento* (`markLoading`) | Caricare un modello grande dura minuti e per tutto quel tempo il programma non lo elenca: senza registro, spegnere un altro modello lo troverebbe «vuoto» e lo fermerebbe sotto quello che sta entrando |
| Uno stack di comandi alla volta (`stackMu`) | Il controllore dello stack ha un lock non bloccante e rifiuta subito la seconda chiamata. Vale anche per start/stop/restart manuali |
| Si riprova (fino a 3 volte, a 3 s) solo se il rifiuto è immediato **e** il testo dice che il lock è occupato | Il lock lo tiene spesso il guardiano, che gira ogni due minuti. Ma un rifiuto immediato non è per forza quello: `stackctl mode qwen-next` rifiuta subito per memoria insufficiente, e riprovarlo non cambia la risposta. Un comando che fallisce dopo aver lavorato non si riprova: costerebbe altri minuti |
| Il motivo del controllore arriva così com'è | Se l'output è `{"ok": false, "error": "…"}` si mostra il campo `error`, senza la busta |
| Lo stato acceso/spento si legge dalle **porte**, mai da uno stato salvato | `launchctl bootout` vale fino al prossimo login: dopo un riavvio launchd rialza i servizi con `RunAtLoad` anche se l'ultimo gesto era spegnerli |
| I comandi sono sempre quelli in `configurazione.json` | Il pannello non lancia processi per conto suo. Qui passano tutti da `stackctl`, che tiene lock e stato desiderato e il guardiano rispetta gli spegnimenti volontari |
| Dopo il gesto si aspetta una fotografia della memoria cominciata *dopo* (`freshMemory`) | Altrimenti la pagina rilegge subito e mostra ancora acceso ciò che è appena stato spento |

**L'interruttore lo calcola il server.** `/api/modelli` porta con ogni scheda un oggetto
`interruttore` (`switchOf`): `puo`, `avviaProgramma`, `fermaProgramma` e, se non si può,
`blocco`, una **chiave** (`remoto`, `in-arrivo`, `guasto`, `senza-avvio`, `senza-arresto`,
`altri-modelli`). La pagina disegna e traduce la chiave; non decide cosa si può fare a un
programma. Stessa regola già applicata a `puoAvviare`/`puoFermare`.

## La memoria libera vera

Il 03/10/2026 alle 11:21 la macchina è andata in kernel panic, di nuovo, con un'aritmetica che
diceva sì: totale 137 GB, riserva 24, nessun picco da sommare → ~113 GB «disponibili» per un
modello da ~90. Ma il desktop (app, OrbStack, cache: ~40 GiB) lasciava **1,85 GiB liberi** dopo
il carico. Misure fatte da localstack, a 1 Hz fino a 5 s dal panic:

- il segnale affidabile è **Pages free + speculative + purgeable**. La cache dei file non è
  disponibile: la guardia di mtplx contava 21,9 GiB «disponibili» (3,9 liberi + 18 di file) e
  la libera vera era 0,07 GiB;
- swap, memoria compressa e `kern.memorystatus_vm_pressure_level` sono rimasti «normali» fino al
  crollo: **non sono un semaforo**;
- Flash-Next Optimized Speed: 77,3 GiB di pesi, impronta reale 79–90 GiB più le pagine calde
  della tabella n-gram.

Conseguenze, tutte nel codice:

| Dove | Cosa |
|---|---|
| `system_darwin.go` | `libera` = free + speculative + purgeable. Prima includeva `inactive`, cioè cache di file: lo stesso errore della guardia di mtplx |
| `budget.Budget.FreeBytes` | La libera vera letta **adesso** (un `vm_stat` a ogni decisione, non la fotografia del monitor). Zero = lettura mancante, e decide l'aritmetica di prima: un `vm_stat` rotto non deve rifiutare tutto in silenzio |
| `budget.Policy.MinFreeBytes` | Dopo il carico devono restare almeno `margineLiberaGB` (default **16**) di libera vera. Se `peso + margine > libera` → rifiuto, con quanto manca |
| `Verdict.FreeBytes` | Il verdetto riporta la libera vera, così «liberi 80, mancano 14» non sembra incoerente |

Regola dell'utente: «che ciò non accada più». Nel dubbio, si rifiuta: un rifiuto costa un clic,
un panic costa la sessione. Sulla macchina vera, in sola lettura, con 70,2 GB di libera vera
l'arbitro ammette i modelli da 6–35 GB e rifiuta Flash-Next (115 GB, ne mancano 60,9).

`stackctl` fa un controllo suo, a monte: `mode qwen-next` rifiuta prima di fermare qualunque
servizio se non ci sono 122 GiB liberi. I due controlli non si sostituiscono: il pannello
rifiuta anche ciò che non passa da `stackctl`.

## La pagina

Tre colonne come prima: navigazione, pagina, ispettore (scheda del modello o Gellow) che si
apre a destra. Cambiano le sezioni e cosa c'è dentro.

| Prima | Ora |
|---|---|
| Panoramica, Modelli, Configurazioni, Controlla + «Altro»: Programmi, Memoria unificata, Scarica, Archivio, File | **Panoramica**, **Client**, **Controlli** + «Altro»: **Scarica modelli**, **Archivio**, **File** |
| Modelli, Memoria e Programmi in tre schermate | Una: quattro numeri, la memoria per programma, e la tabella programmi → modelli con un interruttore per riga |
| Guidato / Esperto | Tolto: due modi duplicavano ogni testo |
| Frasi sulla macchina, glossario, sottotitoli, note | Tolti. Etichette, numeri e tabelle; le spiegazioni vanno a Gellow con lo stato vero davanti |
| «Libera RAM modelli», «Disattiva modello», «Attiva» | L'interruttore. Resta «Ferma tutto», con conferma |

**Memoria.** Barra impilata, un segmento per programma, misure sui processi come nella barra
dei menu. Il colore segue il programma (posizione in configurazione), non la posizione fra gli
accesi. Le otto tinte categoriali sono in ordine fisso, con valori separati per chiaro e scuro,
validate con lo script della skill `dataviz` (CVD ΔE 9,1 chiaro / 8,4 scuro; contrasto sotto
3:1 su tre tinte in chiaro, compensato da legenda con i valori e tooltip). Oltre otto programmi:
grigio «altro», non una tinta inventata. Un solo tooltip per la pagina, con `textContent`: i
nomi dei modelli arrivano da servizi di terzi.

**Conferme.** Spegnere tutto, spegnere a mano un programma con modelli dentro, entrare in un
regime: la pagina mostra a etichette cosa verrà spento e chiede conferma.

**Gellow.** Il pallino accanto al pulsante dice se il suo programma risponde. Se è spento la
risposta è un pulsante «Accendi Gellow», che usa la stessa rotta dell'interruttore e rifà la
domanda. Per saperlo anche a programma spento, `/api/aiuto` ora dice su quale programma vive il
modello (`helperRuntimeFrom`: chi lo serve adesso, altrimenti il catalogo, saltando gli alias).
Il manuale (`rag.go`) e le regole (`explain.go`) nominano solo sezioni che esistono.

## Contratti che si rompono in silenzio

La pagina non ha un compilatore, quindi i contratti sono test sul sorgente:

- `TestMenubarChiamaSoloCioCheLaPaginaHa`: le voci ⌘1–⌘4 del menu Swift eseguono JavaScript
  nella pagina (`vai('…')`). Togliere una sezione avrebbe lasciato voci che non portano da
  nessuna parte. In più `vai()` ripiega sulla Panoramica per una sezione sconosciuta.
- `TestOgniBloccoDellInterruttoreHaIlSuoTesto`: ogni chiave `Blocco` di `lifecycle.go` ha il
  suo testo nella pagina, o l'interruttore è spento e muto.
- `TestAccendereESpegnereEUnGestoSolo`: la pagina non richiama le due strade di prima.
- `TestLaPaginaNonScriveProsa`.

I test di `lifecycle_test.go` usano un programma finto (un server HTTP che risponde solo se
esiste un file-bandierina, con avvio e arresto che lo creano e lo tolgono): si prova il
comportamento vero senza che un test possa spegnere il modello con cui qualcuno sta lavorando.

## Cosa non entra

- Nessuna coda di caricamento, come prima: l'ammissione dice sì o no.
- Nessun uso di swap, compressione o pressione del kernel come semaforo di ammissione.
- Nessuna chiamata diretta a `launchctl`: ci pensano i comandi in configurazione.
- Nessun login admin verso oMLX: oMLX continua a non scaricare il singolo modello, e il
  pannello lo tratta come «il modello è il programma».
- Le etichette «Pi / OC / DSH» nella tabella sono sola lettura; si cambiano da **Client**.

## Da verificare sulla macchina vera

I collaudi di avvio e arresto sono stati fatti su programmi finti. Il primo spegnimento di un
modello reale va guardato: il programma deve fermarsi davvero, e il guardiano non deve
riaccenderlo (`desired.<chiave>=false` lo garantisce a chiamate via `stackctl service … stop`).
