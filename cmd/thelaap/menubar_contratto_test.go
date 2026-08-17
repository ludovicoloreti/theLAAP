package main

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Il menu della barra (Swift) e il server (Go) sono due programmi separati che si
// parlano via HTTP, e niente li teneva allineati. Il 16/08/2026 abbiamo scoperto
// che erano scollegati da tempo, in tre modi diversi e tutti silenziosi:
//
//   1. lo Swift chiamava /api/esegui in GET, ma la rotta accetta solo POST da
//      quando è stata chiusa la falla CSRF (vedi services.go e sicurezza_test.go);
//   2. mandava cmd=laguna-on / laguna-off, mentre gli strumenti registrati si
//      chiamano modello-grande-on / modello-grande-off;
//   3. mandava cmd=stoppa-tutto, mentre comandoAmmesso conosce ferma-tutto.
//
// Nessuno dei tre dava errore visibile: la voce di menu semplicemente non faceva
// niente. Questi test leggono il sorgente Swift e verificano il contratto.

const sorgenteSwift = "../../menubar/theLAAP.swift"

func leggiSwift(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(sorgenteSwift)
	if err != nil {
		t.Skipf("sorgente Swift non leggibile (%v): test saltato", err)
	}
	return string(b)
}

// TestMenubarNonUsaGetSuEsegui: /api/esegui rifiuta le GET. Se lo Swift torna a
// costruire un URL con ?cmd=..., la voce di menu smette di funzionare in silenzio.
func TestMenubarNonUsaGetSuEsegui(t *testing.T) {
	src := leggiSwift(t)
	if strings.Contains(src, "/api/esegui?cmd=") {
		t.Errorf("%s chiama /api/esegui con una GET (?cmd=...), ma la rotta accetta solo POST.\n"+
			"Usa eseguiComando(\"...\"), che manda POST con {\"cmd\":\"...\"} nel corpo.", sorgenteSwift)
	}
}

// TestMenubarNonCablaNessunComando: il menu non deve contenere id di comandi.
//
// Il confronto statico fra i due sorgenti serviva finché lo Swift li scriveva a
// mano: se non combaciavano col server, la voce rispondeva 403 e non faceva
// niente. Ora li chiede a /api/comandi, che li ricava dalla configurazione, e il
// difetto non è più «tenuti allineati» ma «impossibile da riprodurre».
//
// Quello che va difeso è quindi il contrario di prima: che nello Swift non
// ricompaia nessun id.
func TestMenubarNonCablaNessunComando(t *testing.T) {
	src := leggiSwift(t)

	// Gli id che il server sa risolvere, estratti dal sorgente Go. Servono per
	// riconoscerli se qualcuno li riscrive nello Swift.
	ammessi := map[string]bool{}
	for _, b := range mustRead(t, "services.go") {
		for _, m := range regexp.MustCompile(`case "([a-z0-9-]+)":`).FindAllStringSubmatch(b, -1) {
			ammessi[m[1]] = true
		}
	}
	for _, b := range mustRead(t, "configurazione.go") {
		for _, m := range regexp.MustCompile(`ID:\s*"([a-z0-9-]+)"`).FindAllStringSubmatch(b, -1) {
			ammessi[m[1]] = true
		}
		for _, m := range regexp.MustCompile(`\{"[a-z0-9_.-]+\.py",\s*"([a-z0-9-]+)"`).FindAllStringSubmatch(b, -1) {
			ammessi[m[1]] = true
		}
	}
	if len(ammessi) < 3 {
		t.Fatalf("estratti solo %d id dal sorgente Go: il test non sta guardando niente", len(ammessi))
	}

	for id := range ammessi {
		if strings.Contains(src, `"`+id+`"`) {
			t.Errorf("%s cabla l'id %q. Gli id vengono da /api/comandi: scriverli qui "+
				"rimette in piedi due elenchi da tenere allineati a mano.", sorgenteSwift, id)
		}
	}
}

// TestMenubarLeggeIlRegistro: e deve leggerlo davvero, altrimenti il menu resta
// senza comandi e il test sopra passerebbe per il motivo sbagliato.
func TestMenubarLeggeIlRegistro(t *testing.T) {
	src := leggiSwift(t)
	// La CHIAMATA, non la stringa: `/api/comandi` compare anche nei commenti, e
	// cercarla lì fa passare il test anche se il menu ha smesso di chiedere il
	// registro. Verificato togliendo la chiamata: prima non fallivo.
	for _, atteso := range []string{`chiedi("/api/comandi"`, "struct Comando", "c.rotta"} {
		if !strings.Contains(src, atteso) {
			t.Errorf("%s non contiene %q: il menu non sta usando il registro", sorgenteSwift, atteso)
		}
	}
	// La rotta la dice il registro, non lo Swift: nessuna rotta di comando scritta a mano.
	for _, rotta := range []string{`"/api/esegui"`, `"/api/regime"`, `"/api/servizio"`} {
		if strings.Contains(src, rotta) {
			t.Errorf("%s cabla la rotta %s invece di usare quella del registro", sorgenteSwift, rotta)
		}
	}
}

func mustRead(t *testing.T, nome string) []string {
	t.Helper()
	b, err := os.ReadFile(nome)
	if err != nil {
		t.Fatalf("non leggo %s: %v", nome, err)
	}
	return []string{string(b)}
}

func chiavi(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestMenubarUsaLaStessaMisuraDelPannello: alla domanda «quanti GB stanno
// occupando i modelli» la barra e il pannello devono rispondere lo stesso
// numero.
//
// Sono due misure diverse e entrambe presenti in /api/memoria:
// `processi[].correnteByte` è quanto tiene davvero il processo, `caricati[].gb`
// è quanto pesano i file dei modelli. Differiscono parecchio — mtplx dichiara
// 29,3 GB e ne occupa 33,3 — ed è la ragione per cui budget.go decide sui
// processi. Se la barra somma i file e il pannello i processi, le due superfici
// si contraddicono sullo stesso schermo, senza che niente dia errore.
func TestMenubarUsaLaStessaMisuraDelPannello(t *testing.T) {
	src := leggiSwift(t)
	if strings.Contains(src, "caricati.reduce") {
		t.Errorf("%s somma caricati[].gb (i pesi dei FILE) per i GB occupati.\n"+
			"Il pannello somma processi[].correnteByte: i due numeri divergono. Usa m.occupatiGB.",
			sorgenteSwift)
	}
	if !strings.Contains(src, "correnteByte") {
		t.Errorf("%s non legge processi[].correnteByte: non può dare lo stesso numero del pannello",
			sorgenteSwift)
	}
	// E la pagina deve stare sulla stessa misura, o il test guarda solo metà del contratto.
	pagina := mustRead(t, "ui.html")[0]
	if !strings.Contains(pagina, "correnteByte") {
		t.Error("ui.html non somma processi[].correnteByte: il confronto qui sopra non vale niente")
	}
}

// TestMenubarNonNominaLaguna: Laguna è stato dismesso il 16/08/2026. Le etichette
// che l'utente legge non devono più nominarlo, né citarne la taglia (90 GB).
func TestMenubarNonNominaLaguna(t *testing.T) {
	src := leggiSwift(t)
	for _, brutto := range []string{"Laguna", "90 GB"} {
		if strings.Contains(src, brutto) {
			t.Errorf("%s contiene ancora %q: il modello grande è DeepSeek V4 Flash (81 GB) dal 16/08/2026",
				sorgenteSwift, brutto)
		}
	}
}
