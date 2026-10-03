package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Optional installations have an explicit purpose; absence from a coding client
// does not make an embedding model, helper or second serving alias a fault.
type CatalogEntry struct {
	Runtime        string `json:"runtime"`
	ID             string `json:"id"`
	Uso            string `json:"uso"`
	Motivo         string `json:"motivo,omitempty"`
	AliasDi        string `json:"aliasDi,omitempty"`
	Ricevuta       string `json:"ricevuta,omitempty"`
	Quantizzazione string `json:"quantizzazione,omitempty"`
}

type catalogInfo struct {
	Bytes   float64
	Context int
}

var catalogCache = struct {
	sync.Mutex
	at   time.Time
	data map[string]catalogInfo
}{}

func parseCatalogInfo(kind string, body []byte) map[string]catalogInfo {
	out := map[string]catalogInfo{}
	var data struct {
		Models []struct {
			ID        string  `json:"id"`
			Key       string  `json:"key"`
			Name      string  `json:"name"`
			Size      float64 `json:"size"`
			Bytes     float64 `json:"size_bytes"`
			Estimated float64 `json:"estimated_size"`
			Context   int     `json:"max_context_length"`
			Window    int     `json:"max_context_window"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &data) != nil {
		return out
	}
	for _, m := range data.Models {
		id, size, ctx := m.ID, m.Estimated, m.Window
		if kind == "lmstudio" {
			id, size, ctx = m.Key, m.Bytes, m.Context
		}
		if kind == "ollama" {
			id, size, ctx = m.Name, m.Size, 0
		}
		if id != "" {
			out[id] = catalogInfo{Bytes: size, Context: ctx}
		}
	}
	return out
}

func catalogMetadata() map[string]catalogInfo {
	catalogCache.Lock()
	defer catalogCache.Unlock()
	if catalogCache.data != nil && time.Since(catalogCache.at) < 15*time.Second {
		return catalogCache.data
	}
	out := map[string]catalogInfo{}
	for _, r := range configuredRuntimes() {
		path := ""
		switch r.Chiave {
		case "omlx":
			path = "/v1/models/status"
		case "lmstudio":
			path = "/api/v1/models"
		case "ollama":
			path = "/api/tags"
		}
		if path == "" {
			continue
		}
		for id, info := range parseCatalogInfo(r.Chiave, httpGet(fmt.Sprintf("http://127.0.0.1:%d%s", r.Porta, path), 2*time.Second)) {
			out[r.Chiave+"|"+id] = info
		}
	}
	catalogCache.at, catalogCache.data = time.Now(), out
	return out
}

func catalogAnnotation(runtime, id string) CatalogEntry {
	for _, entry := range cfg().CatalogoModelli {
		if entry.Runtime == runtime && entry.ID == id {
			return entry
		}
	}
	return CatalogEntry{Uso: "disponibile"}
}

func installationPending(entry CatalogEntry) bool {
	if entry.Ricevuta == "" {
		return false
	}
	_, err := os.Stat(espandi(entry.Ricevuta))
	return os.IsNotExist(err)
}

// Keep one card per declared model identity. Aliases remain attached so a loaded
// alternative and client registrations are still visible to the model inspector.
func mergeCatalogAliases(cards []Card) []Card {
	index := map[string]int{}
	for i, c := range cards {
		index[c.Runtime+"|"+c.ID] = i
	}
	hidden := map[int]bool{}
	for i, c := range cards {
		j, ok := index[c.AliasDi]
		if !ok || i == j || cards[j].AliasDi != "" || c.Runtime != cards[j].Runtime {
			continue
		}
		if c.InPi || c.InOC || c.InDSH {
			continue
		} // preserve client-specific choices
		cards[j].Alias = append(cards[j].Alias, c.Model)
		hidden[i] = true
	}
	out := make([]Card, 0, len(cards))
	for i, c := range cards {
		if !hidden[i] {
			out = append(out, c)
		}
	}
	return out
}
