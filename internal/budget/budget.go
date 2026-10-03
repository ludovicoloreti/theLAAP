package budget

import (
	"fmt"
	"sort"
)

// The memory arbiter.
//
// Why it exists: on 27 July 2026 this machine hit a kernel panic with two
// inference servers resident at once, ~154 GB on 128 GB. Neither knew about the
// other. Asked whether it could admit the model, the memory guard of one of them
// said yes, and wrote it in its own log:
//
//	Admitting 'Laguna-S-2.1-oQ6e' above the admission soft target
//	with no idle model left to evict (90.82GB > 86.70GB, ceiling 102.00GB)
//
// Every server knows only itself. An admission decision cannot be delegated to
// something that cannot see the whole machine: it needs an arbiter above all of
// them, and the panel is the only thing sitting in that position.
//
// Nothing is executed here and nothing is read from the system: only arithmetic
// on numbers that arrive from outside. That is what makes the 27 July scenario
// reproducible in a test instead of on the machine.

// RuntimeUsage: how much a program weighs and what can be done about it.
//
// The json tags stay in Italian: they are the wire contract with the page and
// with the menu bar app, and renaming them would buy nothing while touching
// three programs at once.
type RuntimeUsage struct {
	Key  string `json:"chiave"`
	Name string `json:"nome"`
	// PeakBytes is the peak, and the peak is what decisions are made on.
	// CurrentBytes is what it holds right now, and that is the number to draw in
	// the bar: showing the peak would make the machine look fuller than it is.
	PeakBytes    uint64   `json:"pesoByte"`
	CurrentBytes uint64   `json:"correnteByte"`
	Estimated    bool     `json:"stimato,omitempty"` // fallback measure, less precise
	Freeable     bool     `json:"liberabile"`        // false = cannot be unloaded without stopping it
	Models       []string `json:"modelli,omitempty"`
}

func (o RuntimeUsage) PeakGB() float64 { return float64(o.PeakBytes) / 1e9 }

// Budget: the snapshot a decision is made on.
type Budget struct {
	TotalBytes     uint64
	OSReserveBytes uint64
	Used           []RuntimeUsage
	// FreeBytes: la memoria che si può ancora impegnare adesso — RAM meno
	// memoria anonima, wired, compressore e il pavimento di cache dei file che
	// il sistema non restituisce. Non le pagine «libere», che stanno al fondo
	// anche in condizioni sane. Zero = lettura non disponibile, e allora decide
	// solo l'aritmetica sui picchi.
	//
	// Serve perché il totale meno i picchi non vede il resto della macchina: il
	// 03/10/2026 il desktop (app, OrbStack, cache: ~40 GiB) lasciava 1,85 GiB
	// liberi dopo un carico che l'aritmetica dava per ammesso con ~23 GB di
	// margine, e la macchina è andata in kernel panic.
	FreeBytes uint64
}

// Verdict: the answer, carrying what to do when it is no.
type Verdict struct {
	Allowed        bool     `json:"ammesso"`
	RequestedBytes uint64   `json:"richiestoByte"`
	AvailableBytes uint64   `json:"disponibileByte"`
	FreeBytes      uint64   `json:"liberaByte,omitempty"` // la libera vera letta adesso, se nota
	MissingBytes   uint64   `json:"mancanoByte,omitempty"`
	ToFree         []string `json:"daLiberare,omitempty"` // keys of the runtimes to stop
	Reason         string   `json:"motivo"`
}

// Policy: the rules that sit above the arithmetic.
type Policy struct {
	// OneLargeModelAtATime: above LargeThresholdBytes only one stays.
	//
	// It is the rule the owner of this machine stated after the panic, and one
	// that arithmetic alone does not express: two 60 GB models would fit in 128,
	// and would leave the system no margin to grow into.
	OneLargeModelAtATime bool
	LargeThresholdBytes  uint64
	// MinFreeBytes: quanta memoria libera vera deve restare DOPO il carico.
	// Zero = nessun controllo. Nel dubbio si rifiuta: un rifiuto costa un
	// clic, un panic costa la sessione.
	MinFreeBytes uint64
}

// usedBytes: how much is already committed.
func (b Budget) usedBytes() uint64 {
	var s uint64
	for _, o := range b.Used {
		s += o.PeakBytes
	}
	return s
}

// AvailableBytes: what is left for a new model, minus the reserve kept for the
// operating system. Never negative: if we are already past it, it is zero.
func (b Budget) AvailableBytes() uint64 {
	used := b.usedBytes() + b.OSReserveBytes
	if used >= b.TotalBytes {
		return 0
	}
	return b.TotalBytes - used
}

// LoadableBytes: the largest model Admits would accept right now (the rule on
// large models aside). It is what the panel shows as «free for a model».
//
// The arithmetic on peaks only sees the runtimes of the stack. The real free
// memory also sees everything else running on the machine, so when it is known
// the smaller of the two wins: the page must not say «81 GB free» right above
// a 35 GB model it refuses.
func (b Budget) LoadableBytes(p Policy) uint64 {
	loadable := b.AvailableBytes()
	if b.FreeBytes > 0 && p.MinFreeBytes > 0 {
		var real uint64
		if b.FreeBytes > p.MinFreeBytes {
			real = b.FreeBytes - p.MinFreeBytes
		}
		if real < loadable {
			loadable = real
		}
	}
	return loadable
}

// Admits answers the question the panel did not know how to ask itself: if I
// load this, does it fit?
func (b Budget) Admits(requestedBytes uint64, p Policy) Verdict {
	// The verdict carries the free figure it decides on: the page prints
	// needed, free and missing one under the other, and they have to add up.
	v := Verdict{
		RequestedBytes: requestedBytes,
		AvailableBytes: b.LoadableBytes(p),
		FreeBytes:      b.FreeBytes,
	}
	if requestedBytes == 0 {
		v.Allowed = false
		v.Reason = "I do not know how much this model takes: I cannot say whether it fits"
		return v
	}

	// The rule: one large model at a time. It applies before the arithmetic,
	// because even when the sums would work out, two large models together leave
	// neither of them room to grow while in use.
	if p.OneLargeModelAtATime && requestedBytes >= p.LargeThresholdBytes {
		var large []RuntimeUsage
		for _, o := range b.Used {
			if o.PeakBytes >= p.LargeThresholdBytes {
				large = append(large, o)
			}
		}
		if len(large) > 0 {
			v.Allowed = false
			v.ToFree = keysOf(large)
			v.Reason = fmt.Sprintf(
				"there is already a large model in memory (%s, %.0f GB). This machine keeps "+
					"one at a time: free that one, then load this (%.0f GB).",
				namesOf(large), sumGB(large), float64(requestedBytes)/1e9)
			return v
		}
	}

	// La memoria libera vera: l'aritmetica sui picchi non la conosce.
	if b.FreeBytes > 0 && p.MinFreeBytes > 0 && requestedBytes+p.MinFreeBytes > b.FreeBytes {
		v.Allowed = false
		// What is missing is measured against the tighter of the two limits.
		v.MissingBytes = requestedBytes - v.AvailableBytes
		v.ToFree = keysOf(chosenToFree(b.Used, v.MissingBytes))
		v.Reason = fmt.Sprintf(
			"not enough memory: %.0f GB needed, and %.0f GB must stay free afterwards, "+
				"but only %.0f GB can still be committed right now (model weights cannot be compressed). %.0f GB missing.",
			float64(requestedBytes)/1e9, float64(p.MinFreeBytes)/1e9,
			float64(b.FreeBytes)/1e9, float64(v.MissingBytes)/1e9)
		return v
	}

	if requestedBytes <= v.AvailableBytes {
		v.Allowed = true
		v.Reason = fmt.Sprintf("it fits: %.0f GB needed and %.0f free, "+
			"keeping %.0f GB aside for the system",
			float64(requestedBytes)/1e9, float64(v.AvailableBytes)/1e9,
			float64(b.OSReserveBytes)/1e9)
		return v
	}

	v.Allowed = false
	v.MissingBytes = requestedBytes - v.AvailableBytes
	chosen := chosenToFree(b.Used, v.MissingBytes)
	v.ToFree = keysOf(chosen)

	if len(chosen) == 0 {
		v.Reason = fmt.Sprintf(
			"it does not fit: %.0f GB needed and %.0f free. There is nothing to free: "+
				"this model is too large for this machine.",
			float64(requestedBytes)/1e9, float64(v.AvailableBytes)/1e9)
		return v
	}
	v.Reason = fmt.Sprintf(
		"it does not fit: %.0f GB needed and %.0f free, %.0f missing. "+
			"Freeing %s recovers %.0f GB and then it fits.",
		float64(requestedBytes)/1e9, float64(v.AvailableBytes)/1e9,
		float64(v.MissingBytes)/1e9, namesOf(chosen), sumGB(chosen))
	return v
}

// chosenToFree: which runtimes to stop to recover at least `missing`.
//
// It starts from the heaviest, so it proposes the smallest number of operations.
// Returns nil when freeing everything would still not be enough: better to say
// so than to suggest shutting down half the machine for nothing.
func chosenToFree(used []RuntimeUsage, missing uint64) []RuntimeUsage {
	sorted := append([]RuntimeUsage{}, used...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].PeakBytes > sorted[j].PeakBytes
	})
	var chosen []RuntimeUsage
	var recovered uint64
	for _, o := range sorted {
		if o.PeakBytes == 0 {
			continue
		}
		chosen = append(chosen, o)
		recovered += o.PeakBytes
		if recovered >= missing {
			return chosen
		}
	}
	return nil
}

func keysOf(oo []RuntimeUsage) []string {
	var s []string
	for _, o := range oo {
		s = append(s, o.Key)
	}
	return s
}

func namesOf(oo []RuntimeUsage) string {
	var s []string
	for _, o := range oo {
		s = append(s, o.Name)
	}
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	}
	return fmt.Sprintf("%s and %s", joinCommas(s[:len(s)-1]), s[len(s)-1])
}

func joinCommas(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

func sumGB(oo []RuntimeUsage) float64 {
	var s uint64
	for _, o := range oo {
		s += o.PeakBytes
	}
	return float64(s) / 1e9
}
