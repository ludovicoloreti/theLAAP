package main

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestScriviChiaveDeepSeekHarnessNelCredentialStore(t *testing.T) {
	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "settings.yaml")
	settings := `llm-pi-ai:
  providers:
    esempio-hub:
      apiKeyEnv: CURTI_AIHUB_API_KEY
      models:
        - id: agentic
`
	if err := os.WriteFile(settingsPath, []byte(settings), 0o600); err != nil {
		t.Fatal(err)
	}
	credsPath := filepath.Join(dir, ".credentials.yaml")
	creds := `version: 1
refs:
  CURTI_AIHUB_API_KEY: vecchia
`
	if err := os.WriteFile(credsPath, []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeDSHKey(settingsPath, "esempio-hub", "nuova-di-prova"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(credsPath)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	refs := got["refs"].(map[string]any)
	if refs["CURTI_AIHUB_API_KEY"] != "nuova-di-prova" {
		t.Fatalf("chiave DSH non aggiornata: %+v", refs)
	}
	if originale, err := os.ReadFile(settingsPath); err != nil || string(originale) != settings {
		t.Fatalf("settings.yaml è stato alterato: err=%v contenuto=%q", err, originale)
	}
	st, err := os.Stat(credsPath)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("permessi credential store: mode=%v", st.Mode().Perm())
	}
	backup, err := filepath.Glob(credsPath + ".bak-*")
	if err != nil || len(backup) != 1 {
		t.Fatalf("backup credential store: err=%v file=%v", err, backup)
	}
}
