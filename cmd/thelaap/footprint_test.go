package main

// La misura su cui si decide e il PICCO, non il valore corrente.
//
// Stava in budget_test.go finche budget era nello stesso pacchetto. Occupazione
// vive in footprint.go, che legge dal sistema operativo e resta qui: il test la
// segue, invece di tenere internal/budget legato a un tipo che non gli appartiene.

import "testing"

const GB = 1_000_000_000

func gb(n float64) uint64 { return uint64(n * GB) }

// Il picco conta, non il valore corrente: ammettere sulla base del corrente
// significa autorizzare un sovraccarico che si manifesta dopo.
func TestSiDecideSulPiccoNonSulCorrente(t *testing.T) {
	o := Occupazione{CorrenteByte: gb(59), PiccoByte: gb(79)}
	if o.PesoDaPrevedereByte() != gb(79) {
		t.Errorf("peso da prevedere = %.0f GB, volevo 79 (il picco)",
			float64(o.PesoDaPrevedereByte())/GB)
	}
	// Se il picco non è noto vale il corrente, non zero.
	o2 := Occupazione{CorrenteByte: gb(30)}
	if o2.PesoDaPrevedereByte() != gb(30) {
		t.Errorf("senza picco dovrebbe valere il corrente, ho %.0f GB",
			float64(o2.PesoDaPrevedereByte())/GB)
	}
}
