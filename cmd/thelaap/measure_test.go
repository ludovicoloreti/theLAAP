package main

import (
	"math"
	"strings"
	"testing"
)

// La soglia del tetto grafico era codificata a 124518 MB — il valore
// raccomandato da oMLX, e precisamente quello con cui questa macchina è andata
// in kernel panic il 27/07/2026. Avvisando solo *sopra*, a 124518 esatti
// taceva. Questo test fissa il comportamento nuovo sui numeri veri.
func TestAvvisoTettoGrafico(t *testing.T) {
	const totale = 137.4 // 128 GiB in GB decimali

	casi := []struct {
		nome   string
		mib    float64
		avvisa bool
	}{
		{"il valore del panic (124518 MiB)", 124518, true},
		{"il vecchio 122 GiB", 124928, true},
		{"impostazione attuale (114688 MiB)", 114688, false},
		{"prudente (106496 MiB)", 106496, false},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			tetto := c.mib * 1048576 / 1e9
			sotto := totale - tetto
			avvisa := sotto < minimoSottoIlTettoGBDefault
			if avvisa != c.avvisa {
				t.Errorf("tetto %.0f MiB = %.1f GB, sotto il tetto %.1f GB: avvisa=%v, volevo %v",
					c.mib, tetto, sotto, avvisa, c.avvisa)
			}
		})
	}
}

// Il sysctl è in MiB; il resto del pannello lavora in GB decimali. Prima la
// conversione dava GiB e il numero finiva nella stessa barra dei GB decimali.
func TestConversioneTettoGrafico(t *testing.T) {
	const mib = 114688.0
	got := mib * 1048576 / 1e9
	const vuole = 120.259084288
	if math.Abs(got-vuole) > 0.001 {
		t.Errorf("%.0f MiB convertiti = %.6f GB, volevo %.6f", mib, got, vuole)
	}
	// Il vecchio calcolo sbagliava di quasi 8 GB: vale la pena fissarlo.
	vecchio := mib / 1024
	if math.Abs(got-vecchio) < 8 {
		t.Errorf("la differenza col vecchio calcolo dovrebbe essere ~8 GB, è %.1f", got-vecchio)
	}
}

func TestMisuraGB(t *testing.T) {
	casi := []struct {
		nome   string
		campi  []string
		i      int
		vuole  float64
		valido bool
	}{
		{"GB attaccato", []string{"modello", "29.3GB"}, 1, 29.3, true},
		{"GB staccato", []string{"modello", "29.3", "GB"}, 1, 29.3, true},
		{"MB convertiti", []string{"modello", "30012", "MB"}, 1, 30012.0 / 1024, true},
		{"GiB", []string{"modello", "12GiB"}, 1, 12, true},
		{"senza unità", []string{"modello", "42"}, 1, 0, false},
		{"non numerico", []string{"modello", "abc"}, 1, 0, false},
		{"zero", []string{"modello", "0GB"}, 1, 0, false},
	}
	for _, c := range casi {
		t.Run(c.nome, func(t *testing.T) {
			got, ok := measureGB(c.campi, c.i)
			if ok != c.valido {
				t.Fatalf("valido = %v, volevo %v", ok, c.valido)
			}
			if ok && math.Abs(got-c.vuole) > 0.01 {
				t.Errorf("= %.3f, volevo %.3f", got, c.vuole)
			}
		})
	}
}

// measureGB indicizza uno slice: un output storto non deve far cadere il
// processo, perché gira dentro il monitor in sottofondo.
func TestMisuraGBNonVaInPanico(t *testing.T) {
	casi := [][]string{
		{},
		{"solo"},
		{"a", "b"},
	}
	for _, campi := range casi {
		for i := range append([]string{}, campi...) {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("panico con campi=%v i=%d: %v", campi, i, r)
					}
				}()
				measureGB(campi, i)
			}()
		}
	}
}

func TestStatoRuntimeDiceQualeModelloEAttivo(t *testing.T) {
	b := []byte(`{"final_ceiling":120000000000,"models":[
  {"id":"qwen-attivo","loaded":true,"actual_size":30400000000,"estimated_size":31500000000,"engine_type":"vlm"},
  {"id":"altro","loaded":false,"estimated_size":1000000000,"engine_type":"llm"},
  {"id":"MarkItDown","loaded":true,"actual_size":0,"engine_type":"markitdown"}
]}`)
	got, tetto := loadedModelsStatus(b, "oMLX")
	if len(got) != 1 || got[0].Nome != "qwen-attivo" || math.Abs(got[0].GB-30.4) > 0.01 {
		t.Fatalf("modelli attivi inattesi: %+v", got)
	}
	if math.Abs(tetto-120) > 0.01 {
		t.Fatalf("tetto = %.1f, atteso 120", tetto)
	}
}

// Il tetto per singolo modello lo dichiara un programma (oggi oMLX, il suo
// «final_ceiling», che cambia con la memoria libera) e vale solo per i modelli
// che carica lui. Il 4/10/2026 l'avviso diceva «qwen3.8-27b-mtp occupa 30 GB su
// un tetto di 33»: il 27B gira su MTPLX, a cui quel tetto non si applica.
func TestLAvvisoDelTettoRiguardaSoloIlProgrammaCheLoDichiara(t *testing.T) {
	m := MemState{CeilingGB: 33, CeilingRuntime: "oMLX",
		Caricati: []ModelInRAM{{Nome: "qwen3.8-27b-mtp", Runtime: "MTPLX", GB: 30}}}
	if a := avvisoTetto(m); a != "" {
		t.Fatalf("avviso per un modello di un altro programma: %q", a)
	}
}

func TestLAvvisoDelTettoNominaIlProgrammaEIlModello(t *testing.T) {
	m := MemState{CeilingGB: 33, CeilingRuntime: "oMLX", Caricati: []ModelInRAM{
		{Nome: "qwen3.8-27b-mtp", Runtime: "MTPLX", GB: 30},
		{Nome: "gemma-31b", Runtime: "oMLX", GB: 31}}}
	a := avvisoTetto(m)
	for _, atteso := range []string{"gemma-31b", "oMLX", "33 GB"} {
		if !strings.Contains(a, atteso) {
			t.Errorf("avviso %q: manca %q", a, atteso)
		}
	}
}

func TestLontanoDalTettoNessunAvviso(t *testing.T) {
	m := MemState{CeilingGB: 63, CeilingRuntime: "oMLX",
		Caricati: []ModelInRAM{{Nome: "gemma-31b", Runtime: "oMLX", GB: 31}}}
	if a := avvisoTetto(m); a != "" {
		t.Fatalf("avviso con metà del tetto libero: %q", a)
	}
}

// Senza sapere di chi è il tetto non si avvisa: meglio tacere che attribuirlo
// al programma sbagliato.
func TestUnTettoSenzaProgrammaNonProduceAvvisi(t *testing.T) {
	m := MemState{CeilingGB: 33, Caricati: []ModelInRAM{{Nome: "x", Runtime: "oMLX", GB: 32}}}
	if a := avvisoTetto(m); a != "" {
		t.Fatalf("avviso senza sapere di chi è il tetto: %q", a)
	}
}

// Il tetto si legge insieme al nome di chi lo dichiara.
func TestIlTettoArrivaConIlNomeDelProgramma(t *testing.T) {
	b := []byte(`{"final_ceiling":62659642064,"models":[{"id":"m","loaded":true,"estimated_size":3000000000}]}`)
	caricati, tetto := loadedFromHTTP(RuntimeCfg{Chiave: "omlx", Nome: "oMLX"}, b, nil)
	if len(caricati) != 1 || math.Abs(tetto-62.66) > 0.01 || caricati[0].Runtime != "oMLX" {
		t.Fatalf("caricati=%+v tetto=%.2f", caricati, tetto)
	}
}
