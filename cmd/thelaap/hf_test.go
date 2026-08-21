package main

import "testing"

func TestRicercaHuggingFaceChiedeDirettamenteMLX(t *testing.T) {
	if got := hfSearchTerms("qwen"); got != "qwen MLX" {
		t.Fatalf("ricerca generica = %q, atteso qwen MLX", got)
	}
	if got := hfSearchTerms("gemma mlx"); got != "gemma mlx" {
		t.Fatalf("MLX duplicato: %q", got)
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
