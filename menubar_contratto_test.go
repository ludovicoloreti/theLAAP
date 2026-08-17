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

const sorgenteSwift = "menubar/theLAAP.swift"

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

// TestMenubarUsaSoloComandiAmmessi: ogni cmd citato nello Swift deve esistere fra
// quelli che il server sa risolvere, altrimenti risponde 403 e la voce non fa nulla.
//
// Il confronto è STATICO, fra i due sorgenti, e non passa da comandoAmmesso: quella
// funzione legge cfg(), che nei test è vuota e direbbe di no a tutto — un test che
// fallisce sempre non distingue il codice giusto da quello rotto.
func TestMenubarUsaSoloComandiAmmessi(t *testing.T) {
	src := leggiSwift(t)

	ammessi := map[string]bool{}
	// i due casi cablati in comandoAmmesso (services.go)
	for _, b := range mustRead(t, "services.go") {
		for _, m := range regexp.MustCompile(`case "([a-z0-9-]+)":`).FindAllStringSubmatch(b, -1) {
			ammessi[m[1]] = true
		}
	}
	// gli ID degli strumenti registrati (configurazione.go)
	for _, b := range mustRead(t, "configurazione.go") {
		for _, m := range regexp.MustCompile(`ID:\s*"([a-z0-9-]+)"`).FindAllStringSubmatch(b, -1) {
			ammessi[m[1]] = true
		}
		// e quelli della tabella posizionale: {"aistack.py", "stato", "Stato rapido", ...}
		// dove l'id è il SECONDO campo, non il primo (il primo è il nome del file).
		for _, m := range regexp.MustCompile(`\{"[a-z0-9_.-]+\.py",\s*"([a-z0-9-]+)"`).FindAllStringSubmatch(b, -1) {
			ammessi[m[1]] = true
		}
	}
	if len(ammessi) < 3 {
		t.Fatalf("estratti solo %d id ammessi dal sorgente Go: il test non sta guardando niente", len(ammessi))
	}

	var citati []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`eseguiComando\("([a-z0-9-]+)"`),
		regexp.MustCompile(`representedObject = "([a-z0-9-]+)"`),
	} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			citati = append(citati, m[1])
		}
	}
	if len(citati) == 0 {
		t.Fatal("nessun comando trovato nel sorgente Swift: il test non sta guardando niente")
	}

	for _, id := range citati {
		if !ammessi[id] {
			t.Errorf("il menu della barra manda cmd=%q, che il server non sa risolvere "+
				"→ 403, e la voce non fa nulla.\nId noti: %v", id, chiavi(ammessi))
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
