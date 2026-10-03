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

// Il kernel panic del 03/10/2026, alle 11:21. L'aritmetica sui picchi diceva sì:
// totale 137 GB, nessun programma acceso, riserva 24 GB, quindi ~113 GB
// disponibili per un modello da ~90 GB. Ma col desktop normale (app, OrbStack,
// cache: ~40 GiB) la memoria LIBERA VERA era 1,85 GiB subito dopo il carico. La
// guardia di mtplx contava 21,9 GiB «disponibili» perché sommava 18 GiB di
// cache di file: la memoria libera vera era 0,07 GiB.
//
// Il totale non basta: serve anche quanta memoria è libera adesso.
func TestMemoriaLiberaVeraFermaCioCheLAritmeticaAmmette(t *testing.T) {
	b := Budget{
		TotalBytes:     gb(137.4),
		OSReserveBytes: gb(24),
		FreeBytes:      gb(60), // il desktop si tiene il resto
	}
	p := Policy{MinFreeBytes: gb(17.2)}

	v := b.Admits(gb(90), p) // l'aritmetica sui picchi: 90 <= 113,4 → sì

	if v.Allowed {
		t.Fatalf("ammesso un modello da 90 GB con 60 GB liberi davvero: %q", v.Reason)
	}
	if want := gb(90) + gb(17.2) - gb(60); v.MissingBytes != want {
		t.Errorf("mancano %d byte, atteso %d (peso + margine - libera)", v.MissingBytes, want)
	}
	if !strings.Contains(v.Reason, "free") {
		t.Errorf("il motivo non parla della memoria libera: %q", v.Reason)
	}
}

func TestMargineMinimoDiMemoriaLiberaSiRispetta(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24), FreeBytes: gb(60)}
	p := Policy{MinFreeBytes: gb(16)}

	// 44 GB lasciano esattamente 16 GB liberi: ammesso. Un byte in più, no.
	if v := b.Admits(gb(44), p); !v.Allowed {
		t.Errorf("rifiutato un caricamento che lascia esattamente il margine: %q", v.Reason)
	}
	if v := b.Admits(gb(44)+1, p); v.Allowed {
		t.Error("ammesso un caricamento che scende sotto il margine")
	}
}

// Libera «sconosciuta» (zero) non è libera «zero»: la lettura può mancare, e
// allora decide l'aritmetica di prima. Altrimenti un vm_stat rotto rifiuterebbe
// tutto, in silenzio, senza dire perché.
func TestSenzaLetturaDellaMemoriaLiberaDecideLAritmetica(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24)}
	if v := b.Admits(gb(30), Policy{MinFreeBytes: gb(16)}); !v.Allowed {
		t.Errorf("rifiutato per una lettura che non c'è: %q", v.Reason)
	}
}

// Il numero che il pannello scrive come «libera per un modello» deve essere
// quello che l'arbitro accetta davvero. Il 4/10/2026 la pagina diceva 81 GB
// liberi e, due righe sotto, rifiutava un modello da 35: un'altra applicazione
// teneva 27 GB che l'aritmetica sui programmi dello stack non vede.
func TestLiberaPerUnModelloEQuantoLArbitroAccetta(t *testing.T) {
	b := Budget{
		TotalBytes:     gb(137.4),
		OSReserveBytes: gb(24),
		Used:           []RuntimeUsage{{Key: "mtplx", Name: "MTPLX", PeakBytes: gb(31.3)}},
		FreeBytes:      gb(48.5),
	}
	p := Policy{OneLargeModelAtATime: true, LargeThresholdBytes: gb(40), MinFreeBytes: gb(16)}

	libera := b.LoadableBytes(p)
	if libera != gb(48.5)-gb(16) {
		t.Fatalf("libera per un modello = %.1f GB, attesi 32,5 (48,5 impegnabili meno 16 da lasciare)", float64(libera)/GB)
	}
	if v := b.Admits(libera, p); !v.Allowed {
		t.Fatalf("un modello grande quanto il numero mostrato viene rifiutato: %s", v.Reason)
	}
	if v := b.Admits(libera+gb(1), p); v.Allowed {
		t.Fatal("un modello più grande del numero mostrato viene ammesso")
	}
}

func TestLiberaPerUnModelloSenzaLetturaEQuellaDellAritmetica(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24),
		Used: []RuntimeUsage{{Key: "mtplx", Name: "MTPLX", PeakBytes: gb(31.3)}}}
	p := Policy{MinFreeBytes: gb(16)}
	if got := b.LoadableBytes(p); got != b.AvailableBytes() {
		t.Fatalf("senza lettura della memoria vera: %.1f GB invece di %.1f", float64(got)/GB, float64(b.AvailableBytes())/GB)
	}
}

// Quando l'aritmetica è la più stretta delle due, vince lei.
func TestLiberaPerUnModelloNonSuperaMaiLAritmetica(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24),
		Used: []RuntimeUsage{{Key: "mtplx", Name: "MTPLX", PeakBytes: gb(79)}}, FreeBytes: gb(100)}
	p := Policy{MinFreeBytes: gb(16)}
	if got := b.LoadableBytes(p); got != b.AvailableBytes() {
		t.Fatalf("%.1f GB invece dei %.1f dell'aritmetica", float64(got)/GB, float64(b.AvailableBytes())/GB)
	}
}

// Già sotto il margine: zero, non un numero negativo girato in enorme.
func TestLiberaPerUnModelloNonVaSottozero(t *testing.T) {
	b := Budget{TotalBytes: gb(137.4), OSReserveBytes: gb(24), FreeBytes: gb(9)}
	if got := b.LoadableBytes(Policy{MinFreeBytes: gb(16)}); got != 0 {
		t.Fatalf("con 9 GB impegnabili e 16 da lasciare: %.1f GB invece di 0", float64(got)/GB)
	}
}

// Il verdetto porta con sé il «libero» su cui ha deciso: la pagina scrive
// «serve», «liberi» e «mancano» uno sotto l'altro, e i tre numeri devono
// tornare (serve meno liberi = mancano), non raccontare due memorie diverse.
func TestIlVerdettoPortaIlLiberoSuCuiHaDeciso(t *testing.T) {
	b := Budget{
		TotalBytes:     gb(137.4),
		OSReserveBytes: gb(24),
		Used:           []RuntimeUsage{{Key: "mtplx", Name: "MTPLX", PeakBytes: gb(31.3)}},
		FreeBytes:      gb(49.3),
	}
	p := Policy{MinFreeBytes: gb(16)}

	no := b.Admits(gb(35.5), p)
	if no.Allowed || no.AvailableBytes != gb(49.3)-gb(16) || no.MissingBytes != gb(35.5)-no.AvailableBytes {
		t.Fatalf("rifiuto incoerente: liberi %.1f GB, mancano %.1f GB", float64(no.AvailableBytes)/GB, float64(no.MissingBytes)/GB)
	}
	si := b.Admits(gb(29.4), p)
	if !si.Allowed || si.AvailableBytes != gb(49.3)-gb(16) || !strings.Contains(si.Reason, "33 free") {
		t.Fatalf("ammissione incoerente: liberi %.1f GB, motivo %q", float64(si.AvailableBytes)/GB, si.Reason)
	}
}

// Quando sono strette tutte e due, memoria vera e aritmetica, manca quanto
// serve a soddisfare la più stretta.
func TestMancaQuantoChiedeIlVincoloPiuStretto(t *testing.T) {
	b := Budget{
		TotalBytes:     gb(137.4),
		OSReserveBytes: gb(24),
		Used:           []RuntimeUsage{{Key: "mtplx", Name: "MTPLX", PeakBytes: gb(79), Freeable: true}},
		FreeBytes:      gb(60),
	}
	v := b.Admits(gb(50), Policy{MinFreeBytes: gb(16)}) // vera: 44 caricabili · aritmetica: 34,4
	if v.Allowed || v.AvailableBytes != b.AvailableBytes() || v.MissingBytes != gb(50)-b.AvailableBytes() {
		t.Fatalf("liberi %.1f GB, mancano %.1f GB (attesi 34,4 e 15,6)", float64(v.AvailableBytes)/GB, float64(v.MissingBytes)/GB)
	}
}
