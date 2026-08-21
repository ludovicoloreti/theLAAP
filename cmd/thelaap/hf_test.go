package main

import "testing"

func TestRicercaHuggingFaceNonForzaUnFormato(t *testing.T) {
	if got := hfSearchTerms("  qwen  "); got != "qwen" {
		t.Fatalf("ricerca generica trasformata in %q", got)
	}
}

func TestFormatoHuggingFaceDistingueLeFamiglie(t *testing.T) {
	for id, atteso := range map[string]string{
		"org/model-MLX-8bit": "MLX 8-bit",
		"org/model-MLX-bf16": "MLX BF16/FP16",
		"org/model-GGUF":     "GGUF",
		"org/model-ONNX":     "ONNX",
		"org/model-CoreML":   "Core ML",
	} {
		if got, _, _ := formato(id); got != atteso {
			t.Errorf("formato(%q) = %q, atteso %q", id, got, atteso)
		}
	}
}

func TestOrdinamentoHuggingFaceAccettaSoloValoriVisibili(t *testing.T) {
	for _, v := range []string{"trendingScore", "downloads", "likes", "lastModified"} {
		if got := hfSortField(v); got != v {
			t.Errorf("ordinamento %q trasformato in %q", v, got)
		}
	}
	for _, v := range []string{"", "stars", "qualcosa&limit=1000"} {
		if got := hfSortField(v); got != "trendingScore" {
			t.Errorf("ordinamento non valido %q non ripiega su tendenza: %q", v, got)
		}
	}
}
