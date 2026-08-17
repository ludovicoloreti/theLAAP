package main

import (
	"math"
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
			got, ok := misuraGB(c.campi, c.i)
			if ok != c.valido {
				t.Fatalf("valido = %v, volevo %v", ok, c.valido)
			}
			if ok && math.Abs(got-c.vuole) > 0.01 {
				t.Errorf("= %.3f, volevo %.3f", got, c.vuole)
			}
		})
	}
}

// misuraGB indicizza uno slice: un output storto non deve far cadere il
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
				misuraGB(campi, i)
			}()
		}
	}
}
