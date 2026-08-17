package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestScriviConfigRispettaIClientSeparatiEListaVuota(t *testing.T) {
	dir := t.TempDir()
	piPath := filepath.Join(dir, "pi.json")
	ocPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(piPath, []byte(`{"providers":{"x":{"models":[]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ocPath, []byte(`{"provider":{"x":{"models":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	vecchiaCfg, vecchioBackup := cfg(), BACKUP
	BACKUP = filepath.Join(dir, "backup")
	cfgMu.Lock()
	CFG = Config{
		Runtime: []RuntimeCfg{{Chiave: "x", ChiaveOC: "x", Nome: "X", Porta: 8000, Elenco: "/v1/models"}},
		Clienti: []ClienteCfg{{Nome: "Pi", File: piPath, Formato: "pi"},
			{Nome: "OpenCode", File: ocPath, Formato: "opencode"}},
	}
	cfgMu.Unlock()
	t.Cleanup(func() {
		BACKUP = vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	modelli := []Modello{
		{Runtime: "x", ID: "solo-pi", Nome: "Solo Pi", Context: 8192, MaxTokens: 1024, InPi: true},
		{Runtime: "x", ID: "solo-oc", Nome: "Solo OC", Context: 8192, MaxTokens: 1024, InOC: true},
	}
	if err := scriviConfig(modelli); err != nil {
		t.Fatal(err)
	}
	pi := leggiMappaTest(t, piPath)
	piModels := pi["providers"].(map[string]any)["x"].(map[string]any)["models"].([]any)
	if len(piModels) != 1 || piModels[0].(map[string]any)["id"] != "solo-pi" {
		t.Fatalf("Pi ha ricevuto i modelli sbagliati: %+v", piModels)
	}
	oc := leggiMappaTest(t, ocPath)
	ocModels := oc["provider"].(map[string]any)["x"].(map[string]any)["models"].(map[string]any)
	if len(ocModels) != 1 || ocModels["solo-oc"] == nil {
		t.Fatalf("OpenCode ha ricevuto i modelli sbagliati: %+v", ocModels)
	}

	if err := scriviConfig([]Modello{}); err != nil {
		t.Fatalf("non si puo' rimuovere l'ultimo modello: %v", err)
	}
	pi = leggiMappaTest(t, piPath)
	piModels = pi["providers"].(map[string]any)["x"].(map[string]any)["models"].([]any)
	oc = leggiMappaTest(t, ocPath)
	ocModels = oc["provider"].(map[string]any)["x"].(map[string]any)["models"].(map[string]any)
	if len(piModels) != 0 || len(ocModels) != 0 {
		t.Fatal("la lista vuota non ha rimosso tutti i modelli")
	}
}

// Una thinkingLevelMap già scritta non va rigenerata: i livelli ammessi li decide il
// modello, non questo codice. Qwen3.8 accetta solo xhigh/medium/low e risponde 400 su
// "high", che è invece quello che la mappa generica manda. Il 15/08/2026 un giro di
// scrittura ha cancellato una mappa corretta e rotto tutte le richieste al 3.8.
func TestScriviConfigPreservaLaThinkingLevelMap(t *testing.T) {
	dir := t.TempDir()
	piPath := filepath.Join(dir, "pi.json")
	ocPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(ocPath, []byte(`{"provider":{"x":{"models":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// mappa su misura: "high" NON deve mai finire al modello
	if err := os.WriteFile(piPath, []byte(`{"providers":{"x":{"models":[{
		"id":"qwen38","name":"Q","reasoning":true,"contextWindow":8192,"maxTokens":1024,
		"thinkingLevelMap":{"minimal":"low","low":"low","medium":"medium",
		                    "high":"xhigh","xhigh":"xhigh","max":"xhigh"}}]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	vecchiaCfg, vecchioBackup := cfg(), BACKUP
	BACKUP = filepath.Join(dir, "backup")
	cfgMu.Lock()
	CFG = Config{
		Runtime: []RuntimeCfg{{Chiave: "x", ChiaveOC: "x", Nome: "X", Porta: 8000, Elenco: "/v1/models"}},
		Clienti: []ClienteCfg{{Nome: "Pi", File: piPath, Formato: "pi"},
			{Nome: "OpenCode", File: ocPath, Formato: "opencode"}},
	}
	cfgMu.Unlock()
	t.Cleanup(func() {
		BACKUP = vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	modelli, errori := statoConfig()
	if len(errori) > 0 {
		t.Fatalf("lettura della config fallita: %v", errori)
	}
	if err := scriviConfig(modelli); err != nil {
		t.Fatal(err)
	}

	pi := leggiMappaTest(t, piPath)
	voci := pi["providers"].(map[string]any)["x"].(map[string]any)["models"].([]any)
	if len(voci) != 1 {
		t.Fatalf("attesa 1 voce, trovate %d", len(voci))
	}
	tlm, ok := voci[0].(map[string]any)["thinkingLevelMap"].(map[string]any)
	if !ok {
		t.Fatal("thinkingLevelMap sparita dopo la scrittura")
	}
	if tlm["high"] != "xhigh" {
		t.Errorf(`la mappa e' stata rigenerata: "high" vale %v, atteso "xhigh" — `+
			`mandare "high" al modello lo fa fallire con 400`, tlm["high"])
	}
	if tlm["xhigh"] != "xhigh" || tlm["minimal"] != "low" {
		t.Errorf("mappa alterata: %+v", tlm)
	}
}

func leggiMappaTest(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
