package main

import (
	"fmt"
	"sort"
)

// L'arbitro della memoria.
//
// Perché serve: il 27/07/2026 questa macchina è andata in kernel panic con due
// server di inferenza residenti insieme, ~154 GB su 128 GB. Nessuno dei due
// sapeva dell'altro. Il memory guard di oMLX, interrogato, ha risposto che
// andava bene e l'ha scritto nel proprio log:
//
//	Admitting 'Laguna-S-2.1-oQ6e' above the admission soft target
//	with no idle model left to evict (90.82GB > 86.70GB, ceiling 102.00GB)
//
// Ogni server conosce solo sé stesso. La decisione di ammissione non si può
// delegare a chi non vede la macchina intera: serve un arbitro sopra tutti, e
// il pannello è l'unico che sta in quella posizione.
//
// Qui dentro non si esegue niente e non si legge niente dal sistema: solo
// aritmetica su numeri che arrivano da fuori. Così si può provare lo scenario
// del 27/07 senza rimetterci la macchina.

// OccupazioneRuntime: quanto pesa un programma e cosa ci si può fare.
type OccupazioneRuntime struct {
	Chiave string `json:"chiave"`
	Nome   string `json:"nome"`
	// PesoByte è il picco: è su quello che si decide. CorrenteByte è quanto
	// occupa adesso, ed è il numero da mostrare nella barra — mostrare il
	// picco farebbe sembrare la macchina più piena di quanto sia.
	PesoByte     uint64   `json:"pesoByte"`
	CorrenteByte uint64   `json:"correnteByte"`
	Stimato      bool     `json:"stimato,omitempty"` // misura di ripiego, meno precisa
	Liberabile   bool     `json:"liberabile"`        // false = non si può scaricare senza fermarlo
	Modelli      []string `json:"modelli,omitempty"`
}

func (o OccupazioneRuntime) PesoGB() float64 { return float64(o.PesoByte) / 1e9 }

// Budget: la fotografia su cui si decide.
type Budget struct {
	TotaleByte    uint64
	RiservaSOByte uint64
	Occupato      []OccupazioneRuntime
}

// Verdetto: la risposta, con dentro cosa fare se è no.
type Verdetto struct {
	Ammesso         bool     `json:"ammesso"`
	RichiestoByte   uint64   `json:"richiestoByte"`
	DisponibileByte uint64   `json:"disponibileByte"`
	MancanoByte     uint64   `json:"mancanoByte,omitempty"`
	DaLiberare      []string `json:"daLiberare,omitempty"` // chiavi dei runtime da fermare
	Motivo          string   `json:"motivo"`
}

// Politica: le regole sopra all'aritmetica.
type Politica struct {
	// UnModelloGrandeAllaVolta: sopra SogliaGrandeByte ne resta uno solo.
	// È la regola che l'utente ha enunciato dopo il panic — «Laguna va da dio,
	// solo non ci deve essere altra roba» — e che l'aritmetica da sola non
	// esprime: due modelli da 60 GB entrerebbero in 128, ma lascerebbero il
	// sistema senza margine per crescere.
	UnModelloGrandeAllaVolta bool
	SogliaGrandeByte         uint64
}

// occupatoByte: quanto è già impegnato.
func (b Budget) occupatoByte() uint64 {
	var s uint64
	for _, o := range b.Occupato {
		s += o.PesoByte
	}
	return s
}

// DisponibileByte: quanto resta per un modello nuovo, tolta la riserva del
// sistema operativo. Mai negativo: se si è già oltre, è zero.
func (b Budget) DisponibileByte() uint64 {
	usato := b.occupatoByte() + b.RiservaSOByte
	if usato >= b.TotaleByte {
		return 0
	}
	return b.TotaleByte - usato
}

// Ammette risponde alla domanda che il pannello non sapeva porsi: se carico
// questo, ci sta?
func (b Budget) Ammette(richiestoByte uint64, p Politica) Verdetto {
	v := Verdetto{
		RichiestoByte:   richiestoByte,
		DisponibileByte: b.DisponibileByte(),
	}
	if richiestoByte == 0 {
		v.Ammesso = false
		v.Motivo = "non so quanto occupa questo modello: non posso dire se ci sta"
		return v
	}

	// Regola: un modello grande alla volta. Si applica prima dell'aritmetica,
	// perché anche quando i conti tornerebbero due modelli grossi insieme non
	// lasciano margine a nessuno dei due per crescere durante l'uso.
	if p.UnModelloGrandeAllaVolta && richiestoByte >= p.SogliaGrandeByte {
		var grossi []OccupazioneRuntime
		for _, o := range b.Occupato {
			if o.PesoByte >= p.SogliaGrandeByte {
				grossi = append(grossi, o)
			}
		}
		if len(grossi) > 0 {
			v.Ammesso = false
			v.DaLiberare = chiaviDi(grossi)
			v.Motivo = fmt.Sprintf(
				"c'è già un modello grande in memoria (%s, %.0f GB). Su questa macchina se ne tiene "+
					"uno alla volta: libera quello e poi carica questo (%.0f GB).",
				nomiDi(grossi), sommaGB(grossi), float64(richiestoByte)/1e9)
			return v
		}
	}

	if richiestoByte <= v.DisponibileByte {
		v.Ammesso = true
		v.Motivo = fmt.Sprintf("ci sta: servono %.0f GB e ce ne sono %.0f liberi, "+
			"tenendo %.0f GB da parte per il sistema",
			float64(richiestoByte)/1e9, float64(v.DisponibileByte)/1e9,
			float64(b.RiservaSOByte)/1e9)
		return v
	}

	v.Ammesso = false
	v.MancanoByte = richiestoByte - v.DisponibileByte
	scelti := sceltiPerLiberare(b.Occupato, v.MancanoByte)
	v.DaLiberare = chiaviDi(scelti)

	if len(scelti) == 0 {
		v.Motivo = fmt.Sprintf(
			"non ci sta: servono %.0f GB e ce ne sono %.0f. Non c'è niente da liberare: "+
				"questo modello è troppo grande per questa macchina.",
			float64(richiestoByte)/1e9, float64(v.DisponibileByte)/1e9)
		return v
	}
	v.Motivo = fmt.Sprintf(
		"non ci sta: servono %.0f GB e ce ne sono %.0f, ne mancano %.0f. "+
			"Liberando %s si recuperano %.0f GB e allora entra.",
		float64(richiestoByte)/1e9, float64(v.DisponibileByte)/1e9,
		float64(v.MancanoByte)/1e9, nomiDi(scelti), sommaGB(scelti))
	return v
}

// sceltiPerLiberare: quali runtime fermare per recuperare almeno `manca`.
// Si parte dal più pesante, così si propone il minor numero di operazioni.
// Restituisce nil se anche liberando tutto non basta: meglio dirlo che
// suggerire di spegnere mezza macchina per niente.
func sceltiPerLiberare(occ []OccupazioneRuntime, manca uint64) []OccupazioneRuntime {
	ordinati := append([]OccupazioneRuntime{}, occ...)
	sort.SliceStable(ordinati, func(i, j int) bool {
		return ordinati[i].PesoByte > ordinati[j].PesoByte
	})
	var scelti []OccupazioneRuntime
	var recuperato uint64
	for _, o := range ordinati {
		if o.PesoByte == 0 {
			continue
		}
		scelti = append(scelti, o)
		recuperato += o.PesoByte
		if recuperato >= manca {
			return scelti
		}
	}
	return nil
}

func chiaviDi(oo []OccupazioneRuntime) []string {
	var s []string
	for _, o := range oo {
		s = append(s, o.Chiave)
	}
	return s
}

func nomiDi(oo []OccupazioneRuntime) string {
	var s []string
	for _, o := range oo {
		s = append(s, o.Nome)
	}
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	}
	return fmt.Sprintf("%s e %s", joinVirgole(s[:len(s)-1]), s[len(s)-1])
}

func joinVirgole(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ", "
		}
		out += x
	}
	return out
}

func sommaGB(oo []OccupazioneRuntime) float64 {
	var s uint64
	for _, o := range oo {
		s += o.PesoByte
	}
	return float64(s) / 1e9
}
