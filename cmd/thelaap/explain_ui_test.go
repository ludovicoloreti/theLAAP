package main

import (
	"strings"
	"testing"
)

func helpTestRuntimes() []RuntimeCfg {
	return []RuntimeCfg{
		{Chiave: "mtplx", Nome: "MTPLX", Ferma: "stop-mtplx"},
		{Chiave: "lmstudio", Nome: "LM Studio", ScaricaModello: "unload {modello}"},
		{Chiave: "readonly", Nome: "Read only"},
	}
}

// Come si spegne un modello lo dice il pannello, non il modellino: è un
// contratto della pagina. Con l'interruttore unico la risposta è una sola —
// spegni l'interruttore — e cambia solo cosa succede al programma.
func TestGellowStopInstructionsMatchRuntimeCapability(t *testing.T) {
	attesi := map[string][]string{
		"it": {"interruttore", "«Panoramica»",
			"LM Studio: esce solo il modello; se era l'ultimo si spegne anche il programma",
			"MTPLX: si spegne insieme al programma, e solo se non tiene altri modelli",
			"non cancella i file"},
		"en": {"switch", "«Overview»",
			"LM Studio: only the model is unloaded; if it was the last one the program stops too",
			"MTPLX: it stops together with its program, and only if no other model is loaded"},
	}
	for _, lang := range []string{"it", "en"} {
		got := aiutoDirettoLingua("how to stop a model?", lang, helpTestRuntimes())
		for _, atteso := range attesi[lang] {
			if !strings.Contains(got, atteso) {
				t.Errorf("%s: manca %q: %s", lang, atteso, got)
			}
		}
		if strings.Contains(got, "Read only") {
			t.Errorf("offered unavailable action: %s", got)
		}
		// Le sezioni di prima non esistono più: nominarle manda la persona a
		// cercare una scheda che non c'è.
		for _, vecchio := range []string{"«Modelli»", "«Models»", "Programmi", "Programs"} {
			if strings.Contains(got, vecchio) {
				t.Errorf("%s: nomina la sezione scomparsa %s: %s", lang, vecchio, got)
			}
		}
	}
}

func TestGellowLasciaAlModelloLeDomandeNonOperative(t *testing.T) {
	if got := aiutoDiretto("quale modello è più veloce?"); got != "" {
		t.Fatal(got)
	}
}

func TestGellowPromptKeepsFactsSeparateAndHonorsLanguage(t *testing.T) {
	for _, lang := range []string{"it", "en"} {
		report := "✓ mtplx risponde\n· glm spento per la modalità corrente"
		messages := explainMessages(reqExplain{Domanda: "Spiegami questo controllo", Contesto: report, Lingua: lang}, "STATO ATTUALE")
		sys := messages[0].(map[string]any)["content"].(string)
		usr := messages[1].(map[string]any)["content"].(string)
		if strings.Contains(sys, report) || strings.Contains(sys, "Sei Gellow") {
			t.Fatal("facts or old echo-prone introduction in system")
		}
		if !strings.Contains(usr, report) || !strings.Contains(usr, "Se non ci sono guasti") {
			t.Fatal("missing report or healthy case")
		}
		language := "italiano"
		if lang == "en" {
			language = "English"
		}
		if !strings.Contains(usr, "Rispondi in "+language) {
			t.Fatal("wrong language")
		}
	}
}

func TestGellowOfflineHasActionableLocalizedResponse(t *testing.T) {
	for _, lang := range []string{"it", "en"} {
		got := helperUnavailable(8000, lang)
		if got["nonDisponibile"] != true {
			t.Fatal("missing recovery flag")
		}
		text := got["risposta"].(string)
		if strings.Contains(text, "dial tcp") || strings.Contains(text, "http://") {
			t.Fatal("raw transport error")
		}
		if lang == "en" && !strings.Contains(text, "«Start Gellow»") {
			t.Fatal(text)
		}
		if lang == "it" && !strings.Contains(text, "«Accendi Gellow»") {
			t.Fatal(text)
		}
		if strings.Contains(text, "Programmi") || strings.Contains(text, "Programs") {
			t.Fatalf("nomina una sezione che non esiste più: %s", text)
		}
	}
}

// Per accendere Gellow il pannello deve sapere quale programma serve il suo
// modello anche quando quel programma è spento — cioè proprio quando serve.
func TestIlProgrammaDiGellowSiTrovaAncheDaSpento(t *testing.T) {
	catalogo := []CatalogEntry{
		{Runtime: "lmstudio", ID: "gemma-e2b", Uso: "alternativo", AliasDi: "omlx|gemma-E2B"},
		{Runtime: "omlx", ID: "gemma-E2B", Uso: "supporto"},
	}
	accesi := []Runtime{{Chiave: "lmstudio", Attivo: true, Modelli: []string{"gemma-E2B"}}, {Chiave: "omlx"}}
	if got := helperRuntimeFrom("gemma-E2B", accesi, catalogo); got != "lmstudio" {
		t.Errorf("chi lo serve adesso vince sul catalogo: ottenuto %q", got)
	}
	spenti := []Runtime{{Chiave: "lmstudio"}, {Chiave: "omlx"}}
	if got := helperRuntimeFrom("gemma-E2B", spenti, catalogo); got != "omlx" {
		t.Errorf("da spento lo dice il catalogo: ottenuto %q", got)
	}
	if got := helperRuntimeFrom("sconosciuto", spenti, catalogo); got != "" {
		t.Errorf("un modello che nessuno conosce non ha un programma: ottenuto %q", got)
	}
}
