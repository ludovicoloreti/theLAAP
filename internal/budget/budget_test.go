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
		TotalBytes:     gb(137.4),
		OSReserveBytes: gb(24),
		Used: []RuntimeUsage{
			{Key: "mtplx", Name: "MTPLX", PeakBytes: gb(79), Freeable: false,
				Models: []string{"qwen3.6-27b-mtp"}},
		},
	}
	p := Policy{OneLargeModelAtATime: true, LargeThresholdBytes: gb(40)}

	v := b.Admits(gb(92.4), p)

	if v.Allowed {
		t.Fatalf("AMMESSO il caricamento che ha ucciso la macchina.\n"+
			"disponibile=%.1f GB richiesto=%.1f GB motivo=%q",
			float64(v.AvailableBytes)/GB, float64(v.RequestedBytes)/GB, v.Reason)
	}
	if len(v.ToFree) == 0 {
		t.Error("rifiuto senza dire cosa liberare: inutile per chi legge")
	}
	if len(v.ToFree) > 0 && v.ToFree[0] != "mtplx" {
		t.Errorf("propone di liberare %v, mi aspettavo mtplx", v.ToFree)
	}
	if !strings.Contains(v.Reason, "MTPLX") {
		t.Errorf("il motivo non nomina cosa liberare: %q", v.Reason)
	}
	t.Logf("verdetto: %s", v.Reason)
}

// Con mtplx fermo, Laguna da solo ci sta: 92,4 + 24 di riserva = 116,4 su
// 137,4. È esattamente quello che l'utente sostiene, e ha ragione.
func TestLagunaDaSolaCiSta(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24)}
	p := Policy{OneLargeModelAtATime: true, LargeThresholdBytes: gb(40)}

	v := b.Admits(gb(92.4), p)
	if !v.Allowed {
		t.Errorf("rifiutato Laguna da solo, che invece ci sta: %s", v.Reason)
	}
	t.Logf("verdetto: %s", v.Reason)
}

func TestUnModelloGrandeAllaVolta(t *testing.T) {
	p := Policy{OneLargeModelAtATime: true, LargeThresholdBytes: gb(40)}

	t.Run("due grandi che ci starebbero comunque", func(t *testing.T) {
		// 45 + 45 + 24 = 114 su 137,4: l'aritmetica direbbe di sì, la regola no.
		b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24),
			Used: []RuntimeUsage{{Key: "a", Name: "A", PeakBytes: gb(45)}}}
		v := b.Admits(gb(45), p)
		if v.Allowed {
			t.Errorf("due modelli grandi ammessi insieme: %s", v.Reason)
		}
	})

	t.Run("un piccolo accanto a un grande passa", func(t *testing.T) {
		b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24),
			Used: []RuntimeUsage{{Key: "a", Name: "A", PeakBytes: gb(60)}}}
		v := b.Admits(gb(8), p)
		if !v.Allowed {
			t.Errorf("un modello piccolo dovrebbe entrare: %s", v.Reason)
		}
	})

	t.Run("senza la regola decide solo l'aritmetica", func(t *testing.T) {
		b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24),
			Used: []RuntimeUsage{{Key: "a", Name: "A", PeakBytes: gb(45)}}}
		v := b.Admits(gb(45), Policy{})
		if !v.Allowed {
			t.Errorf("senza la regola i conti tornano, doveva passare: %s", v.Reason)
		}
	})
}

func TestTroppoGrandePerLaMacchina(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24)}
	v := b.Admits(gb(200), Policy{})
	if v.Allowed {
		t.Fatal("ammesso un modello da 200 GB su una macchina da 137")
	}
	if len(v.ToFree) != 0 {
		t.Errorf("non c'è niente da liberare, invece propone %v", v.ToFree)
	}
	if !strings.Contains(v.Reason, "too large") {
		t.Errorf("the reason should say it is too large: %q", v.Reason)
	}
}

func TestPesoSconosciutoNonPassa(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24)}
	v := b.Admits(0, Policy{})
	if v.Allowed {
		t.Error("ammesso un modello di peso ignoto: nel dubbio si dice no")
	}
}

// Già oltre il budget: DisponibileByte non deve andare sottozero e diventare
// un numero enorme (sono uint64: un sottrarre di troppo e tutto passa).
func TestGiaOltreIlBudgetNonVaSottozero(t *testing.T) {
	b := Budget{
		TotalBytes:     gb(137.4),
		OSReserveBytes: gb(24),
		Used: []RuntimeUsage{
			{Key: "a", Name: "A", PeakBytes: gb(100)},
			{Key: "b", Name: "B", PeakBytes: gb(50)},
		},
	}
	if d := b.AvailableBytes(); d != 0 {
		t.Errorf("disponibile = %d byte, volevo 0", d)
	}
	if v := b.Admits(gb(1), Policy{}); v.Allowed {
		t.Error("ammesso con la macchina già oltre il budget")
	}
}

func TestSceglieIlPiuPesantePerPrimo(t *testing.T) {
	occ := []RuntimeUsage{
		{Key: "piccolo", Name: "Piccolo", PeakBytes: gb(5)},
		{Key: "grosso", Name: "Grosso", PeakBytes: gb(60)},
		{Key: "medio", Name: "Medio", PeakBytes: gb(20)},
	}
	scelti := chosenToFree(occ, gb(50))
	if len(scelti) != 1 || scelti[0].Key != "grosso" {
		t.Errorf("per recuperare 50 GB bastava il più grosso, ha scelto %v", keysOf(scelti))
	}
	scelti = chosenToFree(occ, gb(70))
	if len(scelti) != 2 {
		t.Errorf("per 70 GB servono due runtime, ha scelto %v", keysOf(scelti))
	}
}
