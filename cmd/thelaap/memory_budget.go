package main

import (
	"regexp"
	"strconv"
)

// pavimentoCacheGiB: la parte di cache dei file che macOS non restituisce.
//
// Misurata il 03/10/2026 nei secondi prima di un kernel panic: con la memoria
// esaurita la cache dei file non è mai scesa sotto ~14 GiB. Il resto della
// cache si recupera, e infatti la memoria «libera» da sola inganna: lo stesso
// pomeriggio c'erano 11,6 GiB liberi e 79,5 GiB di cache recuperabile.
const pavimentoCacheGiB = 14.0

var (
	rePaginaVM  = regexp.MustCompile(`page size of (\d+) bytes`)
	reImpegnate = []*regexp.Regexp{
		regexp.MustCompile(`Anonymous pages:\s+(\d+)`),
		regexp.MustCompile(`Pages wired down:\s+(\d+)`),
		regexp.MustCompile(`Pages occupied by compressor:\s+(\d+)`),
	}
)

// budgetFromVMStat: quanta memoria un modello può ancora impegnare, in byte.
//
// È la RAM meno ciò che non si comprime né si butta: memoria anonima, memoria
// wired, compressore e il pavimento della cache dei file. Conta l'impegnato e
// non il «libero» per come è fatto il guasto del 03/10/2026: il driver della
// GPU toglie il blocco alla memoria di un modello ogni volta che la GPU resta
// ferma un paio di secondi; da lì i pesi sono memoria qualunque, ma non si
// comprimono. Se modelli e applicazioni insieme superano la RAM il compressore
// si riempie a vuoto e la macchina si ferma.
//
// Zero vuol dire «non lo so» (uscita illeggibile) oppure «niente» (macchina
// già sovraimpegnata): in entrambi i casi nessuna memoria viene promessa.
func budgetFromVMStat(vm string, totaleByte float64) float64 {
	m := rePaginaVM.FindStringSubmatch(vm)
	if len(m) < 2 {
		return 0
	}
	pagina, _ := strconv.ParseFloat(m[1], 64)
	impegnate := 0.0
	for _, re := range reImpegnate {
		c := re.FindStringSubmatch(vm)
		if len(c) < 2 {
			return 0
		}
		n, _ := strconv.ParseFloat(c[1], 64)
		impegnate += n
	}
	disponibile := totaleByte - impegnate*pagina - pavimentoCacheGiB*1024*1024*1024
	if disponibile < 0 {
		return 0
	}
	return disponibile
}
