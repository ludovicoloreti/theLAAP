package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidaJSONEYAML(t *testing.T) {
	jsonDoc := ConfigDocument{ID: "client-0", Formato: "json"}
	yamlDoc := ConfigDocument{ID: "client-0", Formato: "yaml"}
	if err := validateDocument(jsonDoc, `{"modelli":[{"id":"uno"}]}`); err != nil {
		t.Fatalf("JSON valido rifiutato: %v", err)
	}
	if err := validateDocument(jsonDoc, `{"modelli":`); err == nil {
		t.Fatal("JSON rotto accettato")
	}
	if err := validateDocument(yamlDoc, "modelli:\n  - id: uno\n"); err != nil {
		t.Fatalf("YAML valido rifiutato: %v", err)
	}
	if err := validateDocument(yamlDoc, "modelli:\n  - id: [\n"); err == nil {
		t.Fatal("YAML rotto accettato")
	}
	if err := validateDocument(yamlDoc, "uno: 1\n---\ndue: 2\n"); err == nil {
		t.Fatal("file YAML con due documenti accettato")
	}
}

func TestConversioneJSONYAMLSenzaPerdereLaStruttura(t *testing.T) {
	originale := `{"runtime":[{"chiave":"omlx","porta":8000}],"attivo":true,"limite":24,"interoGrande":9007199254740993}`
	y, err := convertDocument(originale, "json", "yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(y, "runtime:") || !strings.Contains(y, "chiave: omlx") {
		t.Fatalf("YAML inatteso:\n%s", y)
	}
	j, err := convertDocument(y, "yaml", "json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(j, "9007199254740993") {
		t.Fatalf("la conversione ha perso precisione nell'intero grande:\n%s", j)
	}
	var prima, dopo any
	if err := json.Unmarshal([]byte(originale), &prima); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(j), &dopo); err != nil {
		t.Fatal(err)
	}
	if !sameJSON(prima, dopo) {
		t.Fatalf("andata e ritorno hanno cambiato i dati:\n%s", j)
	}
}

func sameJSON(a, b any) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}

func TestEditorNonSovrascriveUnaModificaEsterna(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "configurazione.json")
	backupPath := filepath.Join(dir, "backup")
	iniziale := `{"porta":7070,"runtime":[{"chiave":"x","nome":"X","porta":8000,"elencoModelli":"/v1/models"}]}`
	if err := os.WriteFile(configPath, []byte(iniziale), 0o640); err != nil {
		t.Fatal(err)
	}

	vecchioFile, vecchioBackup := CFGFILE, BACKUP
	vecchiaCfg := cfg()
	CFGFILE, BACKUP = configPath, backupPath
	cfgMu.Lock()
	CFG = Config{Porta: 7070, Runtime: []RuntimeCfg{{Chiave: "x", Nome: "X", Porta: 8000, Elenco: "/v1/models"}}}
	cfgMu.Unlock()
	t.Cleanup(func() {
		CFGFILE, BACKUP = vecchioFile, vecchioBackup
		cfgMu.Lock()
		CFG = vecchiaCfg
		cfgMu.Unlock()
	})

	get := httptest.NewRequest(http.MethodGet, "/api/documento?id=thelaap", nil)
	wget := httptest.NewRecorder()
	apiDocument(wget, get)
	if wget.Code != http.StatusOK {
		t.Fatalf("lettura: codice %d, %s", wget.Code, wget.Body.String())
	}
	var letto documentRead
	if err := json.Unmarshal(wget.Body.Bytes(), &letto); err != nil {
		t.Fatal(err)
	}

	esterno := strings.Replace(iniziale, `"porta":7070`, `"porta":9090`, 1)
	if err := os.WriteFile(configPath, []byte(esterno), 0o640); err != nil {
		t.Fatal(err)
	}
	corpo, _ := json.Marshal(map[string]any{
		"contenuto": iniziale, "formato": "json", "revisione": letto.Revisione,
	})
	post := httptest.NewRequest(http.MethodPost, "/api/documento?id=thelaap", bytes.NewReader(corpo))
	wpost := httptest.NewRecorder()
	apiDocument(wpost, post)
	if wpost.Code != http.StatusConflict {
		t.Fatalf("modifica esterna sovrascritta: codice %d, %s", wpost.Code, wpost.Body.String())
	}
	b, _ := os.ReadFile(configPath)
	if string(b) != esterno {
		t.Fatal("il file cambiato esternamente e' stato toccato")
	}
}

func TestEditorEsponeSoloFileDichiarati(t *testing.T) {
	if _, ok := findDocument("../../etc/passwd"); ok {
		t.Fatal("un percorso arbitrario e' entrato nella lista bianca")
	}
}
