package budget

import (
	"strings"
	"testing"
)

const GB = 1_000_000_000

func gb(n float64) uint64 { return uint64(n * GB) }

// Il test che dà senso a tutto il resto.
//
// Ricostruisce lo stato reale della macchina il 27/07/2026 poco prima del
// kernel panic delle 18:42: mtplx residente col suo Qwen3.6-27B, e la
// richiesta di caricare Laguna Q6 in oMLX. I numeri sono quelli misurati,
// non inventati:
//
//	totale macchina        137,4 GB decimali (128 GiB)
//	mtplx, picco osservato   79 GB
//	Laguna, residente        92,4 GB
//
// Il memory guard di oMLX, che vedeva solo sé stesso, disse di sì.
// L'arbitro deve dire di no, e deve dire cosa fare.
func TestScenarioDelKernelPanic(t *testing.T) {
	b := Budget{
		TotaleByte:    gb(137.4),
		RiservaSOByte: gb(24),
		Occupato: []OccupazioneRuntime{
			{Chiave: "mtplx", Nome: "MTPLX", PesoByte: gb(79), Liberabile: false,
				Modelli: []string{"qwen3.6-27b-mtp"}},
		},
	}
	p := Politica{UnModelloGrandeAllaVolta: true, SogliaGrandeByte: gb(40)}

	v := b.Ammette(gb(92.4), p)

	if v.Ammesso {
		t.Fatalf("AMMESSO il caricamento che ha ucciso la macchina.\n"+
			"disponibile=%.1f GB richiesto=%.1f GB motivo=%q",
			float64(v.DisponibileByte)/GB, float64(v.RichiestoByte)/GB, v.Motivo)
	}
	if len(v.DaLiberare) == 0 {
		t.Error("rifiuto senza dire cosa liberare: inutile per chi legge")
	}
	if len(v.DaLiberare) > 0 && v.DaLiberare[0] != "mtplx" {
		t.Errorf("propone di liberare %v, mi aspettavo mtplx", v.DaLiberare)
	}
	if !strings.Contains(v.Motivo, "MTPLX") {
		t.Errorf("il motivo non nomina cosa liberare: %q", v.Motivo)
	}
	t.Logf("verdetto: %s", v.Motivo)
}

// Con mtplx fermo, Laguna da solo ci sta: 92,4 + 24 di riserva = 116,4 su
// 137,4. È esattamente quello che l'utente sostiene, e ha ragione.
func TestLagunaDaSolaCiSta(t *testing.T) {
	b := Budget{TotaleByte: gb(137.4), RiservaSOByte: gb(24)}
	p := Politica{UnModelloGrandeAllaVolta: true, SogliaGrandeByte: gb(40)}

	v := b.Ammette(gb(92.4), p)
	if !v.Ammesso {
		t.Errorf("rifiutato Laguna da solo, che invece ci sta: %s", v.Motivo)
	}
	t.Logf("verdetto: %s", v.Motivo)
}

func TestUnModelloGrandeAllaVolta(t *testing.T) {
	p := Politica{UnModelloGrandeAllaVolta: true, SogliaGrandeByte: gb(40)}

	t.Run("due grandi che ci starebbero comunque", func(t *testing.T) {
		// 45 + 45 + 24 = 114 su 137,4: l'aritmetica direbbe di sì, la regola no.
		b := Budget{TotaleByte: gb(137.4), RiservaSOByte: gb(24),
			Occupato: []OccupazioneRuntime{{Chiave: "a", Nome: "A", PesoByte: gb(45)}}}
		v := b.Ammette(gb(45), p)
		if v.Ammesso {
			t.Errorf("due modelli grandi ammessi insieme: %s", v.Motivo)
		}
	})

	t.Run("un piccolo accanto a un grande passa", func(t *testing.T) {
		b := Budget{TotaleByte: gb(137.4), RiservaSOByte: gb(24),
			Occupato: []OccupazioneRuntime{{Chiave: "a", Nome: "A", PesoByte: gb(60)}}}
		v := b.Ammette(gb(8), p)
		if !v.Ammesso {
			t.Errorf("un modello piccolo dovrebbe entrare: %s", v.Motivo)
		}
	})

	t.Run("senza la regola decide solo l'aritmetica", func(t *testing.T) {
		b := Budget{TotaleByte: gb(137.4), RiservaSOByte: gb(24),
			Occupato: []OccupazioneRuntime{{Chiave: "a", Nome: "A", PesoByte: gb(45)}}}
		v := b.Ammette(gb(45), Politica{})
		if !v.Ammesso {
			t.Errorf("senza la regola i conti tornano, doveva passare: %s", v.Motivo)
		}
	})
}

func TestTroppoGrandePerLaMacchina(t *testing.T) {
	b := Budget{TotaleByte: gb(137.4), RiservaSOByte: gb(24)}
	v := b.Ammette(gb(200), Politica{})
	if v.Ammesso {
		t.Fatal("ammesso un modello da 200 GB su una macchina da 137")
	}
	if len(v.DaLiberare) != 0 {
		t.Errorf("non c'è niente da liberare, invece propone %v", v.DaLiberare)
	}
	if !strings.Contains(v.Motivo, "troppo grande") {
		t.Errorf("il motivo dovrebbe dire che è troppo grande: %q", v.Motivo)
	}
}

func TestPesoSconosciutoNonPassa(t *testing.T) {
	b := Budget{TotaleByte: gb(137.4), RiservaSOByte: gb(24)}
	v := b.Ammette(0, Politica{})
	if v.Ammesso {
		t.Error("ammesso un modello di peso ignoto: nel dubbio si dice no")
	}
}

// Già oltre il budget: DisponibileByte non deve andare sottozero e diventare
// un numero enorme (sono uint64: un sottrarre di troppo e tutto passa).
func TestGiaOltreIlBudgetNonVaSottozero(t *testing.T) {
	b := Budget{
		TotaleByte:    gb(137.4),
		RiservaSOByte: gb(24),
		Occupato: []OccupazioneRuntime{
			{Chiave: "a", Nome: "A", PesoByte: gb(100)},
			{Chiave: "b", Nome: "B", PesoByte: gb(50)},
		},
	}
	if d := b.DisponibileByte(); d != 0 {
		t.Errorf("disponibile = %d byte, volevo 0", d)
	}
	if v := b.Ammette(gb(1), Politica{}); v.Ammesso {
		t.Error("ammesso con la macchina già oltre il budget")
	}
}

func TestSceglieIlPiuPesantePerPrimo(t *testing.T) {
	occ := []OccupazioneRuntime{
		{Chiave: "piccolo", Nome: "Piccolo", PesoByte: gb(5)},
		{Chiave: "grosso", Nome: "Grosso", PesoByte: gb(60)},
		{Chiave: "medio", Nome: "Medio", PesoByte: gb(20)},
	}
	scelti := sceltiPerLiberare(occ, gb(50))
	if len(scelti) != 1 || scelti[0].Chiave != "grosso" {
		t.Errorf("per recuperare 50 GB bastava il più grosso, ha scelto %v", chiaviDi(scelti))
	}
	scelti = sceltiPerLiberare(occ, gb(70))
	if len(scelti) != 2 {
		t.Errorf("per 70 GB servono due runtime, ha scelto %v", chiaviDi(scelti))
	}
}
