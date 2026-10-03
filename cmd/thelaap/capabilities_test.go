package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Che cosa accetta un modello (testo, immagini…) lo dichiarano i client nei
// loro file: Pi con `input`, OpenCode con `modalities.input`. Il pannello lo
// legge da lì invece di dedurlo dal nome, e lo passa alla pagina.
func TestIlPannelloLeggeDaiClientCosaAccettaUnModello(t *testing.T) {
	dir := t.TempDir()
	piPath, ocPath := filepath.Join(dir, "models.json"), filepath.Join(dir, "opencode.json")
	pi := `{"providers":{"x":{"models":[{"id":"vede","input":["text","image"]},{"id":"solo-testo","input":["text"]},{"id":"muto"}]}}}`
	oc := `{"provider":{"x":{"models":{"vede":{},"solo-oc":{"modalities":{"input":["text","image"]}}}}}}`
	if os.WriteFile(piPath, []byte(pi), 0o644) != nil || os.WriteFile(ocPath, []byte(oc), 0o644) != nil {
		t.Fatal("non riesco a scrivere i file dei client")
	}
	withConfig(t, Config{
		Runtime: []RuntimeCfg{{Chiave: "x", ChiaveOC: "x", Nome: "X", Porta: 8000}},
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"}, {Nome: "OpenCode", File: ocPath, Formato: "opencode"}},
	})
	modelli, _ := configState()
	per := map[string]Model{}
	for _, m := range modelli {
		per[m.ID] = m
	}
	vede := func(id string) bool {
		for _, v := range per[id].Ingressi {
			if v == "image" {
				return true
			}
		}
		return false
	}
	if !vede("vede") || !vede("solo-oc") {
		t.Fatalf("immagini dichiarate dai client non lette: %+v %+v", per["vede"].Ingressi, per["solo-oc"].Ingressi)
	}
	if vede("solo-testo") || len(per["solo-testo"].Ingressi) != 1 {
		t.Fatalf("un modello di solo testo risulta %+v", per["solo-testo"].Ingressi)
	}
	if per["muto"].Ingressi != nil {
		t.Fatalf("chi non dichiara niente non deve risultare capace di niente: %+v", per["muto"].Ingressi)
	}
	b, _ := json.Marshal(per["vede"])
	if !strings.Contains(string(b), `"ingressi":["text","image"]`) {
		t.Fatalf("la pagina non riceve gli ingressi: %s", b)
	}
}

// «Recenti» ha bisogno di sapere quando un modello è stato usato. Il pannello
// lo vede in memoria: lo segna, ma non riscrive il file a ogni giro.
func TestUnModelloVistoInMemoriaRisultaUsatoSenzaRiscrivereAOgniGiro(t *testing.T) {
	profiliDiProva(t)

	t0 := time.Date(2026, 10, 3, 21, 0, 0, 0, time.UTC)
	if !segnaUso("mtplx", "m", t0) {
		t.Fatal("il primo avvistamento non è stato segnato")
	}
	if segnaUso("mtplx", "m", t0.Add(5*time.Minute)) {
		t.Fatal("cinque minuti dopo ha riscritto il file")
	}
	if !segnaUso("mtplx", "m", t0.Add(11*time.Minute)) {
		t.Fatal("undici minuti dopo non ha aggiornato")
	}
	if p := readProfile("mtplx", "m"); p == nil || !p.UltimoUso.Equal(t0.Add(11*time.Minute)) {
		t.Fatalf("ultimo uso non conservato: %+v", p)
	}
	if b, err := os.ReadFile(PROFILI); err != nil || !strings.Contains(string(b), `"ultimoUso"`) {
		t.Fatalf("ultimo uso non scritto su disco: %v", err)
	}
}

// La vista iniziale è «Recenti», non «Tutti»: chi apre il pannello vede i
// modelli che usa. La scelta fatta a mano resta.
func TestLaVistaInizialeEQuellaDeiModelliRecenti(t *testing.T) {
	mustContain(t, "vista iniziale", "filtro:localStorage.getItem('filtro-modelli')||'recenti'",
		"function recente(m)", "localStorage.setItem('filtro-modelli'")
	corpo := corpoFunzione(t, "vistaModelli", "mostraTip")
	for _, atteso := range []string{"recenti:[", "m=>recente(m)", "!S.modelli.some(recente)"} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("vistaModelli: manca %q", atteso)
		}
	}
}

// Contesto e capacità si leggono sulla riga, senza aprire il dettaglio.
// Un'etichetta compare solo se il modello ha quella capacità.
func TestLaRigaMostraContestoECheCosaAccettaIlModello(t *testing.T) {
	riga := corpoFunzione(t, "rigaModello", "vistaModelli")
	for _, atteso := range []string{"contestoDi(m)", "accetta(m,'image')", "m.reasoning"} {
		if !strings.Contains(riga, atteso) {
			t.Errorf("rigaModello: manca %q", atteso)
		}
	}
	tabella := corpoFunzione(t, "vistaModelli", "mostraTip")
	if !strings.Contains(tabella, "T().colContext") {
		t.Error("la tabella non ha la colonna del contesto")
	}
	mustContain(t, "aiuti della riga", "function contestoDi(m)", "function accetta(m,cosa)", `colContext:"Contesto"`, `colContext:"Context"`)
}

// Per cosa usare un modello si legge sulla riga: è la frase del catalogo, e chi
// non l'ha scritta continua a vedere l'identificativo.
func TestLaRigaDiceInBrevePerCosaUsareIlModello(t *testing.T) {
	riga := corpoFunzione(t, "rigaModello", "vistaModelli")
	if !strings.Contains(riga, "m.motivoCatalogo") {
		t.Error("la riga non mostra la descrizione breve del catalogo")
	}
}

// Un modello dichiarato «chat» nel catalogo è una chat anche se il nome farebbe
// pensare a uno strumento (DiffusionGemma è a diffusione, ma si usa per chattare).
func TestUnModelloDichiaratoChatNonFinisceTraGliStrumenti(t *testing.T) {
	mustContain(t, "uso dichiarato", `chat:'Chat'`, "if(['chat','coding'].includes(m.uso)) return false;")
}

// Nella riga la descrizione è una frase da leggere, non un identificativo: va
// a capo su due righe invece di troncarsi dopo quattro parole.
func TestLaDescrizioneNellaRigaSiLeggePerIntero(t *testing.T) {
	riga := corpoFunzione(t, "rigaModello", "vistaModelli")
	if !strings.Contains(riga, "secondaria'+(m.motivoCatalogo?' per':'')") {
		t.Error("la descrizione del catalogo ha lo stile degli identificativi")
	}
	mustContain(t, "stile della descrizione", ".secondaria.per{", "-webkit-line-clamp:2")
}

// Aprendo un modello, per cosa usarlo si legge per intero sotto il titolo.
func TestIlDettaglioDicePerCosaUsareIlModello(t *testing.T) {
	dettaglio := corpoFunzione(t, "inspModello", "formNomi")
	if !strings.Contains(dettaglio, "m.motivoCatalogo") {
		t.Error("il dettaglio non mostra la descrizione del catalogo")
	}
}

// «daily — Tutti i giorni: legge tantissimo…»: quando prima del trattino c'è
// l'identificativo, il nome è quello e la frase è una descrizione. Senza, nella
// tabella il modello si chiamava «Tutti i giorni: legge tantissimo».
func TestUnNomeFattoDiIdentificativoEDescrizioneMostraLIdentificativo(t *testing.T) {
	nome := corpoFunzione(t, "nomeReale", "recente")
	if !strings.Contains(nome, "toLowerCase()===id.toLowerCase()") {
		t.Error("nomeReale prende la descrizione per nome")
	}
}

// Chi usa un modello da Pi, a pannello chiuso, deve ritrovarlo tra i recenti:
// l'uso lo segna il monitor della memoria, che gira sempre, e non la pagina
// quando qualcuno la tiene aperta.
func TestIModelliInMemoriaRisultanoUsatiAncheSenzaLaPaginaAperta(t *testing.T) {
	profiliDiProva(t)
	t0 := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	schede := []Card{
		{Model: Model{Runtime: "omlx", ID: "editore--Modello-8bit"}},
		{Model: Model{Runtime: "omlx", ID: "spento"}},
	}
	// i programmi scrivono i nomi come vogliono: le maiuscole non combaciano mai
	m := MemState{Caricati: []ModelInRAM{{Nome: "EDITORE--modello-8BIT", Runtime: "omlx"}}}

	segnaCaricati(schede, m, t0)

	if p := readProfile("omlx", "editore--Modello-8bit"); p == nil || !p.UltimoUso.Equal(t0) {
		t.Fatalf("il modello in memoria non risulta usato: %+v", p)
	}
	if p := readProfile("omlx", "spento"); p != nil && !p.UltimoUso.IsZero() {
		t.Fatalf("un modello spento risulta usato: %+v", p)
	}
}

// Il monitor gira ogni quattro secondi: le schede (che costano la lettura dei
// file dei client) si mettono insieme al più una volta al minuto, e mai se in
// memoria non c'è niente.
func TestIlRegistroDellUsoNonRileggeIClientAOgniGiro(t *testing.T) {
	profiliDiProva(t)
	piPath := filepath.Join(t.TempDir(), "models.json")
	if os.WriteFile(piPath, []byte(`{"providers":{"x":{"models":[{"id":"primo"},{"id":"secondo"}]}}}`), 0o644) != nil {
		t.Fatal("non riesco a scrivere il file del client")
	}
	withConfig(t, Config{
		Runtime: []RuntimeCfg{{Chiave: "x", ChiaveOC: "x", Nome: "X", Porta: 8000}},
		Clienti: []ClientCfg{{Nome: "Pi", File: piPath, Formato: "pi"}},
	})
	vecchio := ultimoRegistroUso
	ultimoRegistroUso = time.Time{}
	t.Cleanup(func() { ultimoRegistroUso = vecchio })
	usato := func(id string) bool { p := readProfile("x", id); return p != nil && !p.UltimoUso.IsZero() }
	t0 := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

	registraUso(MemState{}, t0)
	if usato("primo") || !ultimoRegistroUso.IsZero() {
		t.Fatal("con la memoria vuota non c'è niente da registrare")
	}
	registraUso(MemState{Caricati: []ModelInRAM{{Nome: "primo", Runtime: "x"}}}, t0)
	if !usato("primo") {
		t.Fatal("il modello in memoria non è stato registrato")
	}
	registraUso(MemState{Caricati: []ModelInRAM{{Nome: "secondo", Runtime: "x"}}}, t0.Add(30*time.Second))
	if usato("secondo") {
		t.Fatal("trenta secondi dopo ha riletto le schede")
	}
	registraUso(MemState{Caricati: []ModelInRAM{{Nome: "secondo", Runtime: "x"}}}, t0.Add(61*time.Second))
	if !usato("secondo") {
		t.Fatal("un minuto dopo non ha registrato il nuovo modello")
	}
}
