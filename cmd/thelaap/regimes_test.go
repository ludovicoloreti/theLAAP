package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withConfig(t *testing.T, c Config) {
	t.Helper()
	cfgMu.Lock()
	vecchia := CFG
	CFG = c
	cfgMu.Unlock()
	t.Cleanup(func() { cfgMu.Lock(); CFG = vecchia; cfgMu.Unlock() })
}

func testConfig() Config {
	return Config{
		Runtime: []RuntimeCfg{
			{Chiave: "omlx", Nome: "oMLX", Porta: 8000, Ferma: "ferma-omlx", Avvia: "avvia-omlx"},
			{Chiave: "mtplx", Nome: "MTPLX", Porta: 8080, Ferma: "ferma-mtplx", Avvia: "avvia-mtplx"},
			{Chiave: "muto", Nome: "Muto", Porta: 9999}, // senza comandi: va ignorato
		},
		Regimi: []RegimeCfg{{
			Chiave: "esclusivo", Nome: "Esclusivo", RuntimeAttivo: "omlx",
			Attiva: "allarga-margini", Disattiva: "stringi-margini",
		}},
	}
}

// L'ordine dei passi non è estetico: allargare i margini di memoria mentre un
// altro modello è ancora residente è precisamente la configurazione che ha
// fatto panicare la macchina il 27/07/2026. Prima si ferma, poi si allarga.
func TestOrdineDeiPassiEntrando(t *testing.T) {
	withConfig(t, testConfig())
	s := composeRegime(cfg().Regimi[0], "on")

	iFerma := strings.Index(s, "ferma-mtplx")
	iAllarga := strings.Index(s, "allarga-margini")
	if iFerma < 0 {
		t.Fatal("non ferma l'altro programma")
	}
	if iAllarga < 0 {
		t.Fatal("non allarga i margini")
	}
	if iFerma > iAllarga {
		t.Error("allarga i margini PRIMA di fermare l'altro programma: " +
			"è la combinazione che ha causato il kernel panic")
	}
	if strings.Contains(s, "ferma-omlx") {
		t.Error("ferma anche il programma che dovrebbe restare acceso")
	}
}

// Uscendo l'ordine si inverte, per lo stesso motivo: prima si stringono i
// margini, poi si riaccende il resto.
func TestOrdineDeiPassiUscendo(t *testing.T) {
	withConfig(t, testConfig())
	s := composeRegime(cfg().Regimi[0], "off")

	iStringi := strings.Index(s, "stringi-margini")
	iAvvia := strings.Index(s, "avvia-mtplx")
	if iStringi < 0 || iAvvia < 0 {
		t.Fatalf("passi mancanti: stringi=%d avvia=%d", iStringi, iAvvia)
	}
	if iStringi > iAvvia {
		t.Error("riaccende gli altri programmi PRIMA di stringere i margini: " +
			"per un istante la macchina è nella configurazione del panic")
	}
	if strings.Contains(s, "avvia-omlx") {
		t.Error("riavvia il programma che non era mai stato fermato")
	}
}

// Un programma senza comandi di governo non può essere fermato: non deve
// finire nella sequenza né far fallire il passaggio.
func TestIgnoraIProgrammiSenzaComandi(t *testing.T) {
	withConfig(t, testConfig())
	s := composeRegime(cfg().Regimi[0], "on")
	if strings.Contains(s, "Muto") {
		t.Error("ha incluso un programma che non sa fermare")
	}
}

// Un regime senza comandi di margine è comunque valido: si limita a fare
// spazio fermando gli altri.
func TestRegimeSenzaComandiDiMargine(t *testing.T) {
	c := testConfig()
	c.Regimi[0].Attiva, c.Regimi[0].Disattiva = "", ""
	withConfig(t, c)
	s := composeRegime(cfg().Regimi[0], "on")
	if !strings.Contains(s, "ferma-mtplx") {
		t.Error("dovrebbe comunque fermare gli altri programmi")
	}
	if strings.TrimSpace(s) == "" {
		t.Error("sequenza vuota")
	}
}

func TestRegimeAttivoDalFileSegno(t *testing.T) {
	d := t.TempDir()
	segno := filepath.Join(d, "segno")

	r := RegimeCfg{Chiave: "x", Nome: "X", Segno: segno}
	if activeRegime(r) {
		t.Error("attivo senza che il file esista")
	}
	if err := os.WriteFile(segno, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !activeRegime(r) {
		t.Error("non attivo pur essendoci il file")
	}

	// Senza file segno il pannello non può sapere lo stato: deve dire "no",
	// non inventare.
	if activeRegime(RegimeCfg{Chiave: "y", Nome: "Y"}) {
		t.Error("senza file segno dovrebbe risultare spento")
	}
}

// I nomi dei programmi finiscono in comandi di shell: devono essere messi fra
// apici, altrimenti un nome con un apostrofo o un punto e virgola diventa
// esecuzione di codice.
func TestNomiCitatiInSicurezza(t *testing.T) {
	c := testConfig()
	c.Runtime[1].Nome = "Cattivo'; rm -rf /tmp/x; echo '"
	withConfig(t, c)
	s := composeRegime(cfg().Regimi[0], "on")
	if strings.Contains(s, "rm -rf /tmp/x; echo") && !strings.Contains(s, `'\''`) {
		t.Error("nome non messo fra apici: iniezione di shell possibile")
	}
}
