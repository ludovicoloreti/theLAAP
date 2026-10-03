package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"},
			{Nome: "OpenCode", File: ocPath, Formato: "opencode"}},
	}
	cfgMu.Unlock()
	t.Cleanup(func() {
		BACKUP = vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	modelli := []Model{
		{Runtime: "x", ID: "solo-pi", Nome: "Solo Pi", Context: 8192, MaxTokens: 1024, InPi: true},
		{Runtime: "x", ID: "solo-oc", Nome: "Solo OC", Context: 8192, MaxTokens: 1024, InOC: true},
	}
	if err := writeConfig(modelli); err != nil {
		t.Fatal(err)
	}
	pi := readTestMap(t, piPath)
	piModels := pi["providers"].(map[string]any)["x"].(map[string]any)["models"].([]any)
	if len(piModels) != 1 || piModels[0].(map[string]any)["id"] != "solo-pi" {
		t.Fatalf("Pi ha ricevuto i modelli sbagliati: %+v", piModels)
	}
	oc := readTestMap(t, ocPath)
	ocModels := oc["provider"].(map[string]any)["x"].(map[string]any)["models"].(map[string]any)
	if len(ocModels) != 1 || ocModels["solo-oc"] == nil {
		t.Fatalf("OpenCode ha ricevuto i modelli sbagliati: %+v", ocModels)
	}

	if err := writeConfig([]Model{}); err != nil {
		t.Fatalf("non si puo' rimuovere l'ultimo modello: %v", err)
	}
	pi = readTestMap(t, piPath)
	piModels = pi["providers"].(map[string]any)["x"].(map[string]any)["models"].([]any)
	oc = readTestMap(t, ocPath)
	ocModels = oc["provider"].(map[string]any)["x"].(map[string]any)["models"].(map[string]any)
	if len(piModels) != 0 || len(ocModels) != 0 {
		t.Fatal("la lista vuota non ha rimosso tutti i modelli")
	}
}

func TestDeepSeekHarnessPartecipaAllaVistaEAlSalvataggio(t *testing.T) {
	dir := t.TempDir()
	piPath := filepath.Join(dir, "pi.json")
	ocPath := filepath.Join(dir, "opencode.json")
	dshPath := filepath.Join(dir, "settings.yaml")
	if err := os.WriteFile(piPath, []byte(`{"providers":{"x":{"models":[]}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ocPath, []byte(`{"provider":{"x":{"models":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	dsh := `# commento da conservare
llm-pi-ai:
  providers:
    x:
      api: openai-completions
      baseURL: http://127.0.0.1:9999/v1
      models:
        - id: solo-dsh
          name: Solo DSH
          contextWindow: 8192
          maxTokens: 1024
          input: [text]
          reasoningEfforts: false
agent-default-model:
  provider: x
  model: solo-dsh
`
	if err := os.WriteFile(dshPath, []byte(dsh), 0o644); err != nil {
		t.Fatal(err)
	}

	vecchiaCfg, vecchioBackup := cfg(), BACKUP
	BACKUP = filepath.Join(dir, "backup")
	cfgMu.Lock()
	CFG = Config{
		Runtime: []RuntimeCfg{{Chiave: "x", ChiaveOC: "x", Nome: "X", Porta: 9999, Elenco: "/v1/models"}},
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"},
			{Nome: "OpenCode", File: ocPath, Formato: "opencode"},
			{Nome: "DeepSeek Harness", File: dshPath, Formato: "dsh"}},
	}
	cfgMu.Unlock()
	t.Cleanup(func() {
		BACKUP = vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	modelli, errori := configState()
	if len(errori) != 0 || len(modelli) != 1 || !modelli[0].InDSH || modelli[0].InPi || modelli[0].InOC {
		t.Fatalf("vista DSH inattesa: modelli=%+v errori=%v", modelli, errori)
	}
	modelli[0].Nome = "Nome aggiornato"
	if err := writeConfig(modelli); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dshPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "# commento da conservare") {
		t.Fatal("il salvataggio DSH ha perso i commenti esterni alla lista modelli")
	}
	got, err := readYAML(dshPath)
	if err != nil {
		t.Fatal(err)
	}
	llm := got["llm-pi-ai"].(map[string]any)
	providers := llm["providers"].(map[string]any)
	models := providers["x"].(map[string]any)["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["name"] != "Nome aggiornato" {
		t.Fatalf("modelli DSH inattesi: %+v", models)
	}
	predefinito := got["agent-default-model"].(map[string]any)
	if predefinito["model"] != "solo-dsh" {
		t.Fatalf("il modello predefinito DSH è stato alterato: %+v", predefinito)
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
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"},
			{Nome: "OpenCode", File: ocPath, Formato: "opencode"}},
	}
	cfgMu.Unlock()
	t.Cleanup(func() {
		BACKUP = vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	modelli, errori := configState()
	if len(errori) > 0 {
		t.Fatalf("lettura della config fallita: %v", errori)
	}
	if err := writeConfig(modelli); err != nil {
		t.Fatal(err)
	}

	pi := readTestMap(t, piPath)
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

func readTestMap(t *testing.T, path string) map[string]any {
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

// Il giro vero: la pagina LEGGE le voci da /api/config, l'utente cambia un nome,
// e la pagina le RIMANDA. In mezzo c'è JSON, e MappaEffort è `json:"-"`: non esce
// e non rientra. Il test sopra non lo vede, perché passa la struct in memoria.
//
// Senza la conservazione lato scrittura, salvare dal pannello sostituisce una
// thinkingLevelMap su misura con quella generica — che manda "high" a un modello
// che accetta solo xhigh/medium/low e fa fallire ogni richiesta con 400. È il
// danno del 15/08/2026, per una strada diversa.
func TestSalvareDalPannelloNonPerdeLaThinkingLevelMap(t *testing.T) {
	dir := t.TempDir()
	piPath := filepath.Join(dir, "pi.json")
	ocPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(ocPath, []byte(`{"provider":{"x":{"models":{}}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
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
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"},
			{Nome: "OpenCode", File: ocPath, Formato: "opencode"}},
	}
	cfgMu.Unlock()
	t.Cleanup(func() {
		BACKUP = vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	modelli, _ := configState()

	// il giro attraverso il browser, senza scorciatoie
	fuori, err := json.Marshal(modelli)
	if err != nil {
		t.Fatal(err)
	}
	var dentro []Model
	if err := json.Unmarshal(fuori, &dentro); err != nil {
		t.Fatal(err)
	}
	for i := range dentro {
		if dentro[i].ID == "qwen38" {
			dentro[i].Nome = "Nome cambiato dall'utente"
		}
	}
	if err := writeConfig(dentro); err != nil {
		t.Fatal(err)
	}

	b, _ := os.ReadFile(piPath)
	var pi map[string]any
	if err := json.Unmarshal(b, &pi); err != nil {
		t.Fatal(err)
	}
	m := pi["providers"].(map[string]any)["x"].(map[string]any)["models"].([]any)[0].(map[string]any)
	tlm, _ := m["thinkingLevelMap"].(map[string]any)
	if tlm["high"] != "xhigh" {
		t.Errorf("thinkingLevelMap[high] = %v, atteso xhigh.\n"+
			"La mappa su misura è stata sostituita da quella generica: "+
			"il modello riceverà \"high\" e risponderà 400.", tlm["high"])
	}
	if m["name"] != "Nome cambiato dall'utente" {
		t.Errorf("il nome non è stato salvato: %v", m["name"])
	}
}
