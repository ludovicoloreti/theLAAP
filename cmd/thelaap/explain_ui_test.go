package main

import (
	"strings"
	"testing"
)

func TestGellowSpiegaComeSpegnereUnModelloSenzaInventareMenu(t *testing.T) {
	got := aiutoDiretto("come si spegne un modello?")
	for _, atteso := range []string{"Modelli", "Disattiva modello", "non cancella i file", "non lo rimuove da Pi o OpenCode"} {
		if !strings.Contains(got, atteso) {
			t.Errorf("risposta diretta senza %q: %s", atteso, got)
		}
	}
	if strings.Contains(got, "Sistema") || strings.Contains(got, "premi Attiva") {
		t.Fatalf("Gellow ha inventato un percorso: %s", got)
	}
}

func TestGellowLasciaAlModelloLeDomandeNonOperative(t *testing.T) {
	if got := aiutoDiretto("quale modello è più veloce?"); got != "" {
		t.Fatalf("risposta diretta fuori tema: %s", got)
	}
}

func TestManualeDiGellowContieneLeAzioniReali(t *testing.T) {
	stato := liveState()
	for _, atteso := range []string{
		"AZIONI REALI DEL PANNELLO", `"Modelli" > clic sulla sua riga > "Disattiva modello"`,
		`"Altro" > "Programmi"`, `Non esiste una sezione chiamata "Sistema"`,
	} {
		if !strings.Contains(stato, atteso) {
			t.Errorf("stato per Gellow senza %q", atteso)
		}
	}
}
