package main

// helper_test.go — le due regole che decidono chi scrive le etichette e come si
// chiamano i modelli. Sono le parti che si possono provare senza far girare il
// modellino: la scelta della taglia e il rifiuto dei doppioni.
//
// I nomi qui sotto sono quelli veri serviti da questa macchina, copiati da
// /api/runtime. Inventarli avrebbe reso il test una conferma di sé stesso.

import "testing"

// La taglia si legge dal nome, e sono i parametri TOTALI a contare: «a4b» sono i
// parametri ATTIVI di un modello a esperti, e dicono la velocità, non il peso.
//
// Il caso che rende la regola necessaria è l'ultimo: un nome che dichiara SOLO
// gli attivi. Leggendoli come taglia, un 30B-A3B chiamato «qwen3-a3b» passerebbe
// per un 3B e finirebbe a fare il modellino.
func TestTagliaDalNomeContaITotaliNonGliAttivi(t *testing.T) {
	casi := []struct {
		id     string
		atteso float64
		perche string
	}{
		{"gemma-4-26b-a4b-it-mlx", 26, "a4b sono gli attivi, il peso è 26"},
		{"lmstudio-community--gemma-4-E2B-it-MLX-8bit", 2, "E2B è la taglia, 8bit la quantizzazione"},
		{"ailexleon--gemma-4-26B-A4B-it-qat-uncensored-heretic-mlx-lm-4Bit", 26, "4Bit non sono 4 miliardi"},
		{"Basher17--Qwen3.6-27B-heretic-v2-oQ8e-mtp", 27, "27B, e il 3.6 della versione non è una taglia"},
		{"qwen3.8-27b-mtp", 27, "idem"},
		{"gemma-4-31b-it-mlx", 31, ""},
		{"mlx-community/Qwen2.5-0.5B-Instruct-4bit", 0.5, "le taglie con la virgola si leggono"},
		{"gemma-3n-2b", 2, ""},
		{"nomic-embed-text", 0, "nessuna taglia nel nome"},
		{"diffusiongemma-26B-MLX-8bit", 26, ""},
		// Solo gli attivi nel nome: non sappiamo quanto pesa, quindi zero — e chi
		// vale zero non è eleggibile. Contarli sarebbe dire «3 miliardi» di un
		// modello che in memoria può starne trenta.
		{"qwen3-a3b-instruct", 0, "dichiara solo i parametri attivi"},
	}
	for _, c := range casi {
		if got := parametriMiliardi(c.id); got != c.atteso {
			t.Errorf("%s → %.1f, atteso %.1f  (%s)", c.id, got, c.atteso, c.perche)
		}
	}
}

// Un modello che non conversa non può fare l'aiuto: proporglielo è farlo fallire.
func TestChiNonPuoFareLAiuto(t *testing.T) {
	fuori := []string{
		"GLM-OCR-8bit", "PaddleOCR-VL-8bit", "dots.mocr-8bit",
		"text-embedding-nomic-embed-text-v1.5", "nomic-embed-text:latest",
		"bge-m3:latest", "mlx-community--Whisper-Large-v3", "diffusiongemma-26b-mlx",
	}
	for _, id := range fuori {
		if !nonPuoFareAiuto(id) {
			t.Errorf("%s verrebbe proposto come aiuto, ma non conversa", id)
		}
	}
	dentro := []string{
		"lmstudio-community--gemma-4-E2B-it-MLX-8bit", "gemma-4-26b-a4b-it-mlx",
		"qwen3.8-27b-mtp", "Basher17--Qwen3.6-27B-heretic-v2-oQ8e-mtp",
	}
	for _, id := range dentro {
		if nonPuoFareAiuto(id) {
			t.Errorf("%s è un modello di chat e verrebbe scartato", id)
		}
	}
}

// Sopra il tetto non è un modellino: la regola esiste perché il ripiego sia
// riconoscibile, non perché sia comodo.
func TestIlTettoDelModellinoScartaIGrossi(t *testing.T) {
	if parametriMiliardi("gemma-4-26b-a4b-it-mlx") <= tettoModellinoB {
		t.Error("un 26B passerebbe per modellino")
	}
	if parametriMiliardi("lmstudio-community--gemma-4-E2B-it-MLX-8bit") > tettoModellinoB {
		t.Error("un 2B non passa per modellino")
	}
}

// La scelta, sui modelli veri che questa macchina serve.
func TestSceglieIlPiuPiccoloCheConversa(t *testing.T) {
	lmstudio := []string{
		"gemma-4-26b-a4b-it-mlx", "gemma-4-31b-it-mlx", "diffusiongemma-26b-mlx",
		"text-embedding-nomic-embed-text-v1.5",
	}
	omlx := []string{
		"Basher17--Qwen3.6-27B-heretic-v2-oQ8e-mtp", "GLM-OCR-8bit", "PaddleOCR-VL-8bit",
		"lmstudio-community--gemma-4-E2B-it-MLX-8bit", "dots.mocr-8bit",
	}

	t.Run("col 2B in elenco sceglie quello", func(t *testing.T) {
		scelto, ripiego := scegliModellino(append(append([]string{}, lmstudio...), omlx...))
		if scelto != "lmstudio-community--gemma-4-E2B-it-MLX-8bit" {
			t.Errorf("scelto %q: doveva essere il 2B", scelto)
		}
		if ripiego {
			t.Error("segnalato come ripiego, ma il 2B c'era")
		}
	})

	// È il caso visto sullo schermo. Il vecchio codice cercava indizi di taglia
	// come sottostringhe («e2b», «-2b», «-4b»): su «gemma-4-26b-a4b-it-mlx»
	// nessuno corrispondeva — «-4b» non c'è, c'è «-a4b» — quindi scattava il
	// ripiego `candidati[0]`, che prendeva il primo modello servito. In silenzio,
	// e messo in cache per tutta la vita del processo. Riprodotto compilando il
	// vecchio codice a parte: rispondeva «gemma-4-26b-a4b-it-mlx».
	t.Run("senza modelli piccoli è un ripiego, e lo dice", func(t *testing.T) {
		scelto, ripiego := scegliModellino(lmstudio)
		if scelto == "" {
			t.Fatal("nessuna scelta: senza aiuto il pannello perde descrizioni e chat")
		}
		if !ripiego {
			t.Errorf("scelto %q senza dire che è un ripiego: pesa %.0f miliardi",
				scelto, parametriMiliardi(scelto))
		}
		if nonPuoFareAiuto(scelto) {
			t.Errorf("anche di ripiego ha scelto %q, che non conversa", scelto)
		}
	})

	t.Run("solo modelli che non conversano: nessuna scelta", func(t *testing.T) {
		scelto, _ := scegliModellino([]string{"GLM-OCR-8bit", "bge-m3:latest", "diffusiongemma-26b-mlx"})
		if scelto != "" {
			t.Errorf("scelto %q, che non sa rispondere a una domanda", scelto)
		}
	})
}

// Due modelli non possono chiamarsi allo stesso modo: il nome serve a
// distinguere, e tre righe uguali sono peggio di tre identificativi grezzi.
// Visto sullo schermo: «Analisi testi lunghi» su ds4, lmstudio e mtplx insieme.
func TestNomiUgualiRifiutati(t *testing.T) {
	presi := map[string]string{
		chiaveEtichetta("Analisi testi lunghi"): "Analisi testi lunghi",
		chiaveEtichetta("Chat veloce"):          "Chat veloce",
	}
	casi := []struct {
		proposta string
		libera   bool
		perche   string
	}{
		{"Scrivere codice", true, ""},
		{"Analisi testi lunghi", false, "identico"},
		{"analisi TESTI lunghi", false, "le maiuscole non sono una differenza"},
		{"  Analisi   testi   lunghi  ", false, "gli spazi doppi non sono una differenza"},
		{"Chat-Veloce", false, "il trattino non è una differenza"},
		{"Chat velocità", true, "questo è un altro nome"},
		{"", false, "un nome vuoto non è un nome"},
		{"   ", false, "nemmeno solo spazi"},
	}
	for _, c := range casi {
		if got := etichettaLibera(c.proposta, presi); got != c.libera {
			t.Errorf("%q → libera=%v, atteso %v  (%s)", c.proposta, got, c.libera, c.perche)
		}
	}
}

// Un nome deve essere un nome: non una frase, e non il gergo che gli abbiamo
// passato noi. Il modellino lo ignora se glielo si chiede soltanto — questi sono
// i suoi scarti veri, presi da due giri su questa macchina.
func TestUnNomeNonEUnaFrase(t *testing.T) {
	buoni := []string{"Chat veloce", "Scrivere codice", "Chat senza filtri",
		"Trascrizione audio", "Elaborazione testi lunga", "Lavori lunghi sul codice"}
	for _, n := range buoni {
		if err := etichettaSensata(n); err != nil {
			t.Errorf("%q rifiutato: %v", n, err)
		}
	}
	cattivi := []struct{ nome, perche string }{
		{"Analisi del contesto e delle regole", "sei parole, ed è una frase"},
		{"Esperti analisi token lunghe", "«token» e «esperti» sono il gergo dei fatti"},
		{"Modello per contesti molto lunghi", "«contesto»"},
		{"Modello da 26 miliardi di parametri", "descrive com'è fatto, non a cosa serve"},
		{"", "vuoto"},
		{"   ", "solo spazi"},
	}
	for _, c := range cattivi {
		if err := etichettaSensata(c.nome); err == nil {
			t.Errorf("%q accettato, ma %s", c.nome, c.perche)
		}
	}
}

// Il nome che un modello ha già non deve impedirgli di riconfermarlo: rifacendo
// le etichette, il suo stesso nome non è un doppione.
func TestIlProprioNomeNonEUnDoppione(t *testing.T) {
	ss := []Scheda{
		{Modello: Modello{Runtime: "mtplx", ID: "qwen"}, Etichetta: "Scrivere codice"},
		{Modello: Modello{Runtime: "lmstudio", ID: "gemma"}, Etichetta: "Chat veloce"},
		{Modello: Modello{Runtime: "omlx", ID: "senzanome"}},
	}
	presi := etichetteInUso(ss, "mtplx", "qwen")
	if !etichettaLibera("Scrivere codice", presi) {
		t.Error("il modello non può riconfermare il proprio nome")
	}
	if etichettaLibera("Chat veloce", presi) {
		t.Error("il nome di un altro modello viene accettato")
	}
	if len(presi) != 1 {
		t.Errorf("nomi presi %v: l'etichetta vuota non deve entrare", presi)
	}
}
