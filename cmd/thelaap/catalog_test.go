package main

import "testing"

func TestRuntimeCatalogUsesDeclaredSizeAndContext(t *testing.T) {
	tests := []struct {
		kind, body, id string
		size           float64
		context        int
	}{
		{"lmstudio", `{"models":[{"key":"local-chat","size_bytes":33795548471,"max_context_length":262144}]}`, "local-chat", 33795548471, 262144},
		{"omlx", `{"models":[{"id":"document-reader","estimated_size":1662974532,"max_context_window":32768}]}`, "document-reader", 1662974532, 32768},
	}
	for _, tt := range tests {
		got := parseCatalogInfo(tt.kind, []byte(tt.body))[tt.id]
		if got.Bytes != tt.size || got.Context != tt.context {
			t.Fatalf("%s: %+v", tt.kind, got)
		}
	}
	if len(parseCatalogInfo("omlx", []byte(`{"error":"unavailable"}`))) != 0 {
		t.Fatal("error payload became a model")
	}
}

func TestOptionalCatalogEntriesKeepTheirPurpose(t *testing.T) {
	withConfig(t, Config{CatalogoModelli: []CatalogEntry{{Runtime: "local", ID: "helper", Uso: "supporto", Motivo: "Usato dal pannello"}, {Runtime: "local", ID: "alias", Uso: "alias", AliasDi: "other|canonical"}}})
	if got := catalogAnnotation("local", "helper"); got.Uso != "supporto" || got.Motivo == "" {
		t.Fatal(got)
	}
	if got := catalogAnnotation("local", "alias"); got.AliasDi != "other|canonical" {
		t.Fatal(got)
	}
	if got := catalogAnnotation("local", "new-model"); got.Uso != "disponibile" {
		t.Fatal(got)
	}
}

func TestAliasesCollapseWithoutHidingSeparateRuntimesOrClientChoices(t *testing.T) {
	cards := []Card{
		{Model: Model{Runtime: "local", ID: "canonical", Servito: true}},
		{Model: Model{Runtime: "local", ID: "short", Servito: true}, AliasDi: "local|canonical"},
		{Model: Model{Runtime: "other", ID: "canonical", Servito: true}, AliasDi: "local|canonical"},
		{Model: Model{Runtime: "local", ID: "selected", InPi: true}, AliasDi: "local|canonical"},
	}
	merged := mergeCatalogAliases(cards)
	if len(merged) != 3 || len(merged[0].Alias) != 1 {
		t.Fatalf("wrong grouping: %+v", merged)
	}
	if !caricato(merged[0], MemState{Caricati: []ModelInRAM{{Nome: "short", Runtime: "local"}}}) {
		t.Fatal("loaded alias became an off model")
	}
}

func TestPendingInstallationPrecedesOfflineState(t *testing.T) {
	card := Card{Model: Model{Runtime: "local", ID: "new"}, InInstallazione: true}
	if got := stateOf(card, MemState{}, nil, map[string]bool{"local": true}); got != StatoInArrivo {
		t.Fatal(got)
	}
}
