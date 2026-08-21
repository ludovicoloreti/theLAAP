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

// Gli identificativi dei modelli arrivano da servizi di terzi. Senza questo
// controllo, un nome costruito ad arte trasformerebbe una rimozione in una
// cancellazione di cartelle arbitrarie.
func TestNomeSicuroRifiutaLaRisalita(t *testing.T) {
	cattivi := []string{
		"../../../etc",
		"pippo/../../../../Users",
		"/etc/passwd",
		"",
		"   ",
		"nome\x00nascosto",
		"con\nritorno",
	}
	for _, c := range cattivi {
		if safeName(c) {
			t.Errorf("accettato un identificativo pericoloso: %q", c)
		}
	}
	buoni := []string{
		"lmstudio-community--gemma-4-26B-A4B-it-MLX-8bit",
		"zecanard/gemma-4-31b-it-uncensored-heretic-ara-MLX-8bit",
		// Laguna è stato dismesso il 16/08/2026 (slot CODE-AGENT → DeepSeek V4 Flash
		// su ds4, che non passa da oMLX). I due nomi restano qui come casi di prova
		// del parser: hanno un punto nella versione e un suffisso di quantizzazione,
		// che è la forma che deve continuare a essere accettata.
		"mlx-community--Laguna-S-2.1-oQ6e",
		"mlx-community--Laguna-S-2.1-oQ5e",
		"mlx-community--Qwen3.8-27B-8bit", // il punto separa la versione
		"mlx-community--Qwen3.5-122B-A10B-4bit",
	}
	for _, b := range buoni {
		if !safeName(b) {
			t.Errorf("rifiutato un identificativo legittimo: %q", b)
		}
	}
}

func TestArchivioNuovoSiPuoRipristinare(t *testing.T) {
	base := t.TempDir()
	radice := filepath.Join(base, "modelli")
	deposito := filepath.Join(base, "archivio")
	if err := os.MkdirAll(radice, 0o755); err != nil {
		t.Fatal(err)
	}

	vecchieRadici, vecchioDeposito := ModelRoots, ModelStore
	ModelRoots, ModelStore = []string{radice}, deposito
	t.Cleanup(func() { ModelRoots, ModelStore = vecchieRadici, vecchioDeposito })

	originale := filepath.Join(radice, "models--tizio--prova")
	if err := os.MkdirAll(originale, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(originale, "peso.bin"), []byte("pesi"), 0o644); err != nil {
		t.Fatal(err)
	}
	posto := examineFolder(originale)
	configurata := Model{Runtime: "omlx", ID: "tizio--prova", Nome: "Prova", InPi: true, InOC: true}
	voce, err := archivia(ModelExam{ID: configurata.ID, Posti: []DiskSpace{posto}, GBTotali: posto.GB},
		configurata.Runtime, []Model{configurata})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(originale); !os.IsNotExist(err) {
		t.Fatal("il modello e' rimasto nella radice dopo l'archiviazione")
	}
	if voci := archiveList(); len(voci) != 1 || !voci[0].Ripristinabile {
		t.Fatalf("archivio inatteso: %+v", voci)
	}

	m, err := restoreArchived(voce.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(originale, "peso.bin")); err != nil {
		t.Fatalf("file non ripristinato: %v", err)
	}
	if len(m.Configurazioni) != 1 || m.Configurazioni[0].ID != configurata.ID {
		t.Fatalf("configurazione non conservata: %+v", m.Configurazioni)
	}
	if len(archiveList()) != 0 {
		t.Fatal("la voce e' rimasta nell'archivio dopo il ripristino")
	}
}

func TestAliasLMStudioUsaLaMappaUfficiale(t *testing.T) {
	b := []byte(`[
  {"modelKey":"gemma-4-31b-it-mlx","path":"lmstudio-community/gemma-4-31B-it-MLX-8bit","indexedModelIdentifier":"lmstudio-community/gemma-4-31B-it-MLX-8bit"},
  {"modelKey":"un-altro","path":"editore/un-altro"}
]`)
	got := lmStudioAliasesJSON("GEMMA-4-31B-IT-MLX", b)
	if len(got) != 1 || got[0] != "lmstudio-community/gemma-4-31B-it-MLX-8bit" {
		t.Fatalf("alias inattesi: %#v", got)
	}
}

func TestArchivioToglieERipristinoRimetteIClient(t *testing.T) {
	a := Model{Runtime: "omlx", ID: "modello-a", Nome: "A", InPi: true, InOC: false}
	b := Model{Runtime: "lmstudio", ID: "modello-b", Nome: "B", InPi: true, InOC: true}
	restanti, associate := withoutConfiguredModel([]Model{a, b}, "omlx", "modello-a")
	if len(restanti) != 1 || restanti[0].ID != b.ID || len(associate) != 1 || associate[0].ID != a.ID {
		t.Fatalf("separazione errata: restanti=%+v associate=%+v", restanti, associate)
	}
	merged := mergeConfiguredModels(restanti, associate)
	if len(merged) != 2 {
		t.Fatalf("ripristino configurazione errato: %+v", merged)
	}
	for _, m := range merged {
		if m.ID == a.ID && (!m.InPi || m.InOC) {
			t.Fatalf("client non conservati: %+v", m)
		}
	}
}

func TestEliminazioneDefinitivaRestaDentroIlDeposito(t *testing.T) {
	deposito := t.TempDir()
	vecchio := ModelStore
	ModelStore = deposito
	t.Cleanup(func() { ModelStore = vecchio })

	bersaglio := filepath.Join(deposito, "2026-08-15", "modello-a")
	fratello := filepath.Join(deposito, "2026-08-15", "modello-b")
	for _, p := range []string{bersaglio, fratello} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "peso.bin"), []byte("pesi"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id := "2026-08-15/modello-a"
	b, _ := json.Marshal(map[string]string{"id": id, "conferma": "ELIMINA " + id})
	r := httptest.NewRequest(http.MethodPost, "/api/modello/elimina", bytes.NewReader(b))
	w := httptest.NewRecorder()
	apiDeleteArchived(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("codice %d: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(bersaglio); !os.IsNotExist(err) {
		t.Fatal("la voce confermata non e' stata eliminata")
	}
	if _, err := os.Stat(filepath.Join(fratello, "peso.bin")); err != nil {
		t.Fatal("l'eliminazione ha toccato una voce vicina")
	}

	for _, idCattivo := range []string{"../fuori", "2026-08-15/../../fuori", "/tmp/fuori"} {
		if _, err := archivePath(idCattivo); err == nil {
			t.Errorf("percorso pericoloso accettato: %q", idCattivo)
		}
	}
	// Anche un componente intermedio simbolico deve essere rifiutato: il
	// percorso sembra interno, ma la cancellazione finirebbe fuori dal deposito.
	fuori := t.TempDir()
	if err := os.Mkdir(filepath.Join(fuori, "modello"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fuori, filepath.Join(deposito, "collegamento")); err == nil {
		if _, err := archivePath("collegamento/modello"); err == nil {
			t.Fatal("componente simbolico intermedio accettato")
		}
	}
}

// La cache HuggingFace scrive "models--publisher--nome", LM Studio
// "publisher/nome". Sono lo stesso modello e vanno riconosciuti come tale,
// altrimenti si toglie una copia e si lascia l'altra a puntare nel vuoto.
func TestRiconosceLeDueFormeDelNome(t *testing.T) {
	radice := t.TempDir()
	vecchie := ModelRoots
	ModelRoots = []string{radice}
	t.Cleanup(func() { ModelRoots = vecchie })

	// forma cache HF
	hf := filepath.Join(radice, "models--lmstudio-community--gemma-4-26B-A4B-it-MLX-8bit")
	os.MkdirAll(hf, 0o755)
	os.WriteFile(filepath.Join(hf, "peso.bin"), make([]byte, 2048), 0o644)

	// forma LM Studio
	lm := filepath.Join(radice, "lmstudio-community", "gemma-4-26B-A4B-it-MLX-8bit")
	os.MkdirAll(lm, 0o755)
	os.WriteFile(filepath.Join(lm, "peso.bin"), make([]byte, 1024), 0o644)

	posti := findOnDisk("lmstudio-community--gemma-4-26B-A4B-it-MLX-8bit")
	if len(posti) != 2 {
		var p []string
		for _, x := range posti {
			p = append(p, x.Percorso)
		}
		t.Fatalf("trovati %d posti, ne volevo 2: %v", len(posti), p)
	}
}

// Una cartella fatta di soli collegamenti non occupa spazio: dirlo evita di
// promettere GB che non si liberano.
func TestDistingueCollegamentiDaFileVeri(t *testing.T) {
	d := t.TempDir()

	veri := filepath.Join(d, "veri")
	os.MkdirAll(veri, 0o755)
	os.WriteFile(filepath.Join(veri, "peso.bin"), make([]byte, 4096), 0o644)

	link := filepath.Join(d, "link")
	os.MkdirAll(link, 0o755)
	os.Symlink(filepath.Join(veri, "peso.bin"), filepath.Join(link, "peso.bin"))

	if p := examineFolder(veri); p.Collegamenti {
		t.Error("una cartella di file veri è stata presa per collegamenti")
	}
	p := examineFolder(link)
	if !p.Collegamenti {
		t.Error("una cartella di soli collegamenti non è stata riconosciuta")
	}
	if p.GB != 0 {
		t.Errorf("i collegamenti non occupano spazio, misurati %.6f GB", p.GB)
	}
}

// Il caso concreto del 28/07: stavo per togliere il modello che il pannello
// usa per il proprio riquadro di aiuto, convinto che non lo usasse nessuno.
func TestNonRimuoveIlModelloDellAiuto(t *testing.T) {
	radice := t.TempDir()
	vecchie := ModelRoots
	ModelRoots = []string{radice}
	t.Cleanup(func() { ModelRoots = vecchie })

	id := "lmstudio-community--gemma-4-E2B-it-MLX-8bit"
	p := filepath.Join(radice, "models--"+id)
	os.MkdirAll(p, 0o755)
	os.WriteFile(filepath.Join(p, "peso.bin"), make([]byte, 1024), 0o644)

	vecchioFisso := EXPLAIN_MODEL_PINNED
	EXPLAIN_MODEL_PINNED = id
	t.Cleanup(func() { EXPLAIN_MODEL_PINNED = vecchioFisso })

	e := esamina(id)
	if e.Rimovibile {
		t.Error("si è dichiarato rimovibile il modello del riquadro di aiuto")
	}
	trovata := false
	for _, d := range e.Dipendenze {
		if d.Grave {
			trovata = true
		}
	}
	if !trovata {
		t.Errorf("nessuna dipendenza grave segnalata: %+v", e.Dipendenze)
	}
}

// Un modello che non sta su questo disco (per esempio su un server aziendale)
// non è un errore: va detto, non si può togliere di qui.
func TestModelloRemotoNonEUnErrore(t *testing.T) {
	radice := t.TempDir()
	vecchie := ModelRoots
	ModelRoots = []string{radice}
	t.Cleanup(func() { ModelRoots = vecchie })

	e := esamina("qwen-aziendale-su-server-remoto")
	if len(e.Posti) != 0 {
		t.Error("ha trovato posti su disco per un modello che non c'è")
	}
	if e.Nota == "" {
		t.Error("non spiega perché non si può togliere")
	}
	if e.Rimovibile {
		t.Error("dichiarato rimovibile un modello che non è su questo disco")
	}
}

// Il caso reale del 29/07: LM Studio crea un COLLEGAMENTO alla cartella dello
// snapshot nella cache HuggingFace, e lo chiama a modo suo —
// "gemma-4-26B-A4B-MLX-8bit" per "gemma-4-26B-A4B-it-MLX-8bit".
//
// Il pannello non prova a spostarlo (indovinare tutte le copie camminando nel
// filesystem sbagliava sempre un caso, arrivando a proporre la cartella di un
// publisher, cioè anche i modelli da tenere). Lo SEGNALA: chi legge decide.
func TestAvvisaChiPuntaAlModello(t *testing.T) {
	radice := t.TempDir()
	vecchie := ModelRoots
	ModelRoots = []string{radice}
	t.Cleanup(func() { ModelRoots = vecchie })

	hf := filepath.Join(radice, "models--tizio--modello-it-8bit", "snapshots", "abc")
	os.MkdirAll(hf, 0o755)
	os.WriteFile(filepath.Join(hf, "peso.bin"), make([]byte, 4096), 0o644)

	// un altro programma che ci punta, con un nome diverso
	pub := filepath.Join(radice, "altroprogramma")
	os.MkdirAll(pub, 0o755)
	os.Symlink(hf, filepath.Join(pub, "modello-8bit"))

	e := esamina("tizio--modello-it-8bit")
	if len(e.Posti) != 1 {
		t.Errorf("posti trovati %d, ne volevo 1 (la cartella vera)", len(e.Posti))
	}
	trovato := false
	for _, d := range e.Dipendenze {
		if strings.Contains(d.Cosa, "puntano") {
			trovato = true
		}
	}
	if !trovato {
		t.Errorf("non avvisa che un altro programma ci punta: %+v", e.Dipendenze)
	}
}

// Le cartelle di servizio della cache (.locks) non sono modelli: comparivano
// come "posti" e facevano dire "2 posti" per un modello solo.
func TestIgnoraLeCartelleDiServizio(t *testing.T) {
	radice := t.TempDir()
	vecchie := ModelRoots
	ModelRoots = []string{radice}
	t.Cleanup(func() { ModelRoots = vecchie })

	for _, d := range []string{"models--tizio--m", ".locks/models--tizio--m"} {
		p := filepath.Join(radice, d)
		os.MkdirAll(p, 0o755)
		os.WriteFile(filepath.Join(p, "x.bin"), make([]byte, 512), 0o644)
	}
	for _, p := range findOnDisk("tizio--m") {
		if strings.Contains(p.Percorso, ".locks") {
			t.Errorf("ha incluso una cartella di servizio: %s", p.Percorso)
		}
	}
}
