# theLAAP — governo della memoria e ciclo di vita dei modelli

Data: 2026-07-27
Stato: implementato

## Perché

Il 27/07/2026 alle 18:42 il Mac è andato in kernel panic
(`watchdog timeout: no checkins from watchdogd in 93 seconds`). Causa: due server di
inferenza residenti insieme — mtplx con Qwen3.6-27B e oMLX con Laguna-S-2.1-oQ6e — per un
totale di ~154 GB su una macchina da 128 GB. Il sistema è finito in swap thrashing, l'I/O
si è saturato, `watchdogd` non è più riuscito a fare checkin e il kernel ha panicato.

Il pannello era acceso e non ha avvisato di nulla. Questa spec descrive cosa cambiare
perché non possa succedere di nuovo, e perché quando succede qualcosa di simile il
pannello lo dica invece di tacere.

## Vincoli verificati sul campo

Tutto quanto segue è misurato su questa macchina, non dedotto.

### 1. La dimensione su disco non predice l'occupazione in memoria

| Modello | Su disco (`du`) | Residente misurato |
|---|---|---|
| Laguna Q6 (oMLX) | 86 GB | 92,4 GB (+7%) |
| Qwen3.6-27B (mtplx) | ~30 GB | 59 GB corrente, **79 GB di picco** (+163%) |

Oggi `memory.go` misura i modelli con `du` sulla cartella. Un preflight costruito su quel
numero avrebbe concluso «86 + 30 = 116 GB, ci stanno nei 128» e avrebbe autorizzato
esattamente la combinazione che ha ucciso la macchina. **La dimensione su disco non è
utilizzabile per decidere l'ammissione.**

### 2. `ps`/RSS sottostima la memoria Metal

Per mtplx: RSS 38,7 GB contro 59 GB di `phys_footprint` reale (−34%). Su Apple Silicon la
memoria IOAccelerator non compare in RSS. La metrica autorevole — quella che guarda jetsam
— è `phys_footprint`.

### 3. Come leggere `phys_footprint` da Go senza cgo

| Metodo | Costo | Precisione | Esito |
|---|---|---|---|
| syscall `proc_info` diretta | — | — | **non praticabile**: su darwin/arm64 Go instrada `syscall.Syscall` nella `syscall()` variadica di libSystem e gli argomenti variadici su arm64 passano dallo stack. Restituisce ENOMEM. |
| `vmmap --summary <pid>` | 2,6 s | 0,1 GB | troppo lento per un ciclo da 4 s |
| `top -l 1 -pid N -stats mem` | 354 ms | 1 GB | praticabile, ma niente picco |
| **`/usr/bin/footprint <pid>`** | **109 ms** | 1 GB | **scelto**: dà corrente *e* picco storico |

`footprint` espone `phys_footprint` e `phys_footprint_peak`. Il picco è ciò che serve per
il modello di crescita: un modello che oggi occupa 59 GB ma ha toccato 79 GB va ammesso
sulla base di 79, non di 59.

Ripiego dichiarato: se `/usr/bin/footprint` non esiste, si usa `ps -o rss` marcando il dato
come **stima** nell'interfaccia. Mai far passare una stima per una misura.

### 4. Lo scarico del singolo modello non è uniforme

| Runtime | Scarico per modello | Meccanismo |
|---|---|---|
| Ollama | sì | `ollama stop <modello>` |
| LM Studio | sì | `lms unload <modello>` |
| oMLX | sì, con sessione admin | `POST /admin/api/login` → cookie → `POST /admin/api/models/{id}/unload` |
| mtplx | **no** | il modello *è* il demone: solo `mtplx stop` sulla porta |

Il pannello deve mostrare la granularità reale di ciascun runtime. Un pulsante unico che
sotto fa cose diverse è una bugia che costa cara.

### 5. Ogni server conosce solo sé stesso

Il memory guard di oMLX con `memory_guard_tier: custom` restituisce un tetto fisso e non
guarda mai la memoria di sistema (`process_memory_enforcer.py:587-588`): era cieco ai 62 GB
di mtplx e ha ammesso Laguna scrivendolo nel proprio log
(`Admitting 'Laguna-S-2.1-oQ6e' above the admission soft target ... (90.82GB > 86.70GB)`).

**Corollario architetturale**: non si può delegare ai runtime la decisione di ammissione.
Serve un arbitro che veda l'intera macchina. È il pannello.

## Architettura

Due pezzi, sovrapposti.

**A — Il pannello misura i processi.** Smette di fidarsi del disco e dei numeri che ogni
server dà di sé. Per ogni runtime risolve il pid, ne legge `phys_footprint` corrente e di
picco, e tiene un budget unico della macchina.

**C — Un modello grande alla volta.** Politica predefinita sopra ad A: un solo modello
sopra una soglia (default 40 GB) residente. Caricarne un altro richiede di scaricare il
precedente, e il pannello lo dice prima, non dopo.

C senza A non è applicabile: per sapere cos'è «grande» serve la misura.

## Componenti

### `footprint_darwin.go` / `footprint_linux.go` / `footprint_windows.go`

Un'unica funzione esportata verso il resto del programma:

```go
// Occupazione di un processo. Stimato=true quando il dato viene da un ripiego
// meno preciso e non deve essere presentato come misura.
type Occupazione struct {
    CorrenteByte uint64
    PiccoByte    uint64
    Stimato      bool
}

func occupazioneProcesso(pid int) (Occupazione, error)
func pidInAscoltoSuPorta(porta int) (int, error)
```

Segue il modello dei `sistema_*.go` già presenti: il resto del programma non sa su cosa
gira. Su macOS `footprint(1)` con ripiego `ps`; su Linux `/proc/<pid>/status`; su Windows
il ripiego.

### `budget.go`

L'arbitro. Non esegue nulla, calcola soltanto — quindi è testabile senza macchina.

```go
type Budget struct {
    TotaleByte    uint64
    RiservaSOByte uint64   // quanto lasciare al sistema operativo
    Occupato      []OccupazioneRuntime
}

type Verdetto struct {
    Ammesso        bool
    DisponibileByte uint64
    RichiestoByte   uint64
    DaScaricare    []string  // cosa liberare per farlo entrare
    Motivo         string    // in italiano, per l'interfaccia
}

func (b Budget) Ammette(richiestoByte uint64, politica Politica) Verdetto
```

La riserva per il sistema operativo è **24 GB di default**, non un numero tirato a caso:
il panic è avvenuto con 6,4 GB lasciati al sistema, e oMLX raccomanda il 5% della RAM
(6,4 GB su 128) — che è precisamente il valore letale. 24 GB è quasi quattro volte tanto.
Configurabile in `configurazione.json`.

### `runtimes.go`

Un adattatore per runtime, che **dichiara** cosa sa fare:

```go
type CapacitaRuntime struct {
    ScaricaSingoloModello bool
    RichiedeAutenticazione bool
    Nota                  string  // mostrata all'utente quando la capacita' manca
}
```

Per mtplx: `ScaricaSingoloModello: false`, `Nota: "il modello e' il servizio: scaricarlo significa fermarlo"`.
L'interfaccia mostra la nota accanto al pulsante, che diventa «Ferma il servizio».

### Preflight

Prima di ogni caricamento il pannello chiama `Ammette()` e, se il verdetto è negativo,
mostra **cosa scaricare** per far entrare il modello, con i GB che ciascuna operazione
libera. Non un rifiuto secco: una proposta eseguibile.

## Gestione degli errori — «ogni eccezione», reso concreto

La richiesta «gestisca ogni eccezione» diventa questi impegni verificabili:

1. **Nessuna goroutine senza `recover()`.** Oggi `avviaMonitorMemoria` (`memory.go:217`) è
   una goroutine nuda: in Go un panic in una goroutine non-HTTP **termina l'intero
   processo**. E `leggeMemoria` fa parsing di output esterni, cioè proprio il codice che
   può andare fuori indice. Ogni goroutine di lungo corso viene avvolta, il panic viene
   registrato, e il ciclo riparte con backoff.

2. **Comando fallito ≠ output vuoto.** Oggi `cmdT` (`memory.go:43`) fa
   `if err != nil { return "" }`: un comando che fallisce è indistinguibile da uno che non
   ha stampato niente, e non c'è log. Diventa `(output, error)` con l'errore registrato e,
   dove conta, mostrato.

3. **L'età del dato è visibile.** Oggi `Aggiornato` è la stringa `"15:04:05"`: l'interfaccia
   non può sapere se la foto è di 4 secondi o di 40 minuti fa. Diventa un timestamp; la
   pagina mostra un avviso quando il dato supera i 30 secondi, così un monitor morto si
   vede invece di sembrare uno stato stabile.

4. **Raccolta in parallelo e con tetto complessivo.** Oggi `leggeMemoria` è seriale: per
   ogni runtime `sh(...)` con timeout 12 s, più `httpGet` 3 s, più `du`. Con quattro
   runtime lenti sono ~60 s per giro contro uno `Sleep(4s)`: il monitor resta
   indietro. Passa a raccolta parallela (come fa già `discovery.go`) con tetto complessivo.

5. **`apiEsegui` con tetto di tempo e disconnessione gestita.** Oggi (`services.go:34-66`)
   non ha timeout — nonostante il README affermi che ogni comando esterno ce l'ha — non
   guarda `r.Context().Done()`, e scarta l'errore di `cmd.Wait()`. Un comando impiantato
   tiene la connessione aperta per sempre e lascia il processo orfano; un fallimento appare
   identico a un successo. Diventa: contesto legato alla richiesta, kill del gruppo di
   processi alla disconnessione, exit code trasmesso alla pagina.

6. **Un solo posto che decide se un errore si mostra.** Gli errori attesi (servizio spento)
   non sono guasti e non devono allarmare; quelli inattesi non devono essere ingoiati.

## Sicurezza — precede tutto il resto

`soloLocale` (`main.go:45`) controlla `RemoteAddr`, che per una richiesta partita dal
browser dell'utente **è sempre 127.0.0.1**. Non c'è controllo di `Origin`/`Referer` né
token. Conseguenze verificate leggendo le rotte:

- `/api/esegui` è una **GET con parametro**: qualsiasi pagina web visitata può fare
  `<img src="http://127.0.0.1:7070/api/esegui?cmd=ferma-tutto">` e spegnere lo stack.
- `/api/servizio` è POST JSON, raggiungibile con un form `enctype="text/plain"` che non
  fa scattare il preflight CORS.

Interventi:

1. Controllo di `Origin`/`Referer` su tutto ciò che muta stato: assente o diverso da
   `http://127.0.0.1:<porta>` → rifiuto.
2. `/api/esegui` passa a **POST**.
3. Token per sessione, generato all'avvio, incorporato nella pagina e richiesto nelle
   chiamate mutanti. Difende anche dal caso in cui un browser non mandi `Origin`.

Questo va fatto **prima** di introdurre il login admin verso oMLX: aggiungere una
credenziale custodita e azioni privilegiate a un pannello con la CSRF aperta peggiorerebbe
la situazione invece di migliorarla.

## Correttezza di misura

`memoriaSistema()` lavora in **GB decimali** (`hw.memsize / 1e9`), ma `tettoGraficaGB()`
restituisce **GiB binari** (`sysctl / 1024`). I due numeri finiscono nella stessa barra: il
tetto viene disegnato ~8 GB più corto del vero. Si standardizza su GB decimali —
l'unità che usa anche Monitoraggio Attività — e `tettoGraficaGB` diventa
`MiB * 1048576 / 1e9`.

La soglia di allarme hardcoded `124518` (`memory.go:175`) va tolta: è il valore che oMLX
raccomanda ed è esattamente quello che ha causato il panic; il pannello avvisava solo
*sopra*, quindi a 124518 esatti taceva. Sostituita da una soglia **derivata**: allarme
quando il tetto grafico lascia al sistema meno della riserva configurata.

## Testing

Il progetto oggi non ha test. Non si introduce un framework: bastano i test standard di Go.

- `budget_test.go` — l'arbitro è pura aritmetica: si testa senza macchina. Casi
  obbligatori: lo scenario reale del 27/07 (mtplx 79 GB di picco + Laguna 92 GB su 128 GB)
  **deve** produrre un verdetto negativo con l'indicazione di scaricare mtplx.
- `measure_test.go` — parsing di `footprint`, `vm_stat`, `lms ps`, `ollama ps` su output
  registrati, inclusi output malformati e vuoti.
- `security_test.go` — le rotte mutanti rifiutano richieste senza `Origin` valido e senza
  token; le rotte in sola lettura restano raggiungibili.

## Cosa non entra (YAGNI)

- Nessuna cronologia storica né grafici nel tempo: la barra attuale basta.
- Nessuna gestione multi-utente o multi-macchina.
- Nessuna coda di caricamento: il preflight dice sì o no, non mette in fila.
- Nessuna riscrittura dell'interfaccia: si aggiungono solo gli elementi necessari.

## Ordine di lavoro

1. Sicurezza (CSRF, GET→POST) — piccolo, e abilita il resto in sicurezza
2. Correttezza di misura (unità, soglia derivata) — piccolo, alto valore
3. Robustezza (recover, parallelo, età del dato, errori, timeout)
4. Governo della memoria (footprint, budget, preflight, scarico per modello)
