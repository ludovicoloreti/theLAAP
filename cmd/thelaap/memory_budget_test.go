package main

import "testing"

// Uscita di vm_stat ridotta ai campi che contano, con le pagine da 16 KB di un M5.
func vmStatDiProva(anonime, wired, compressore, file, libere int) string {
	return "Mach Virtual Memory Statistics: (page size of 16384 bytes)\n" +
		"Pages free:                                 " + itoa(libere) + ".\n" +
		"Pages active:                                   99.\n" +
		"Pages inactive:                                 99.\n" +
		"Pages speculative:                               7.\n" +
		"Pages wired down:                           " + itoa(wired) + ".\n" +
		"Pages purgeable:                                 3.\n" +
		"File-backed pages:                          " + itoa(file) + ".\n" +
		"Anonymous pages:                            " + itoa(anonime) + ".\n" +
		"Pages stored in compressor:                 999999.\n" +
		"Pages occupied by compressor:               " + itoa(compressore) + ".\n"
}

const gibDiProva = 1024.0 * 1024 * 1024

// Il bilancio è la RAM meno ciò che non si comprime né si butta: anonima,
// wired, compressore e il pavimento della cache dei file.
func TestBudgetIsRAMMinusWhatCanNeitherBeCompressedNorDropped(t *testing.T) {
	totale := 128 * gibDiProva
	got := budgetFromVMStat(vmStatDiProva(1000, 2000, 300, 50, 10), totale)
	want := totale - 3300*16384 - pavimentoCacheGiB*gibDiProva
	if got != want {
		t.Fatalf("bilancio = %.0f, atteso %.0f", got, want)
	}
}

// 3/10/2026, pomeriggio: 11,6 GiB «liberi» con 79,5 GiB di cache recuperabile.
// Contare le pagine libere faceva rifiutare all'arbitro quasi ogni modello.
func TestReclaimableFileCacheAndFreePagesDoNotChangeTheBudget(t *testing.T) {
	totale := 128 * gibDiProva
	poca := budgetFromVMStat(vmStatDiProva(1000, 2000, 300, 50, 10), totale)
	tanta := budgetFromVMStat(vmStatDiProva(1000, 2000, 300, 5000000, 4000000), totale)
	if poca != tanta {
		t.Fatalf("la cache dei file ha cambiato il bilancio: %.0f contro %.0f", poca, tanta)
	}
}

// Una lettura illeggibile vale zero, cioè «non lo so»: mai memoria disponibile inventata.
func TestUnreadableStatisticsAreNeverReadAsAvailableMemory(t *testing.T) {
	if got := budgetFromVMStat("formato cambiato", 128*gibDiProva); got != 0 {
		t.Fatalf("bilancio da uscita illeggibile = %.0f, atteso 0", got)
	}
}

// Memoria impegnata oltre la RAM: il bilancio è zero, non un numero negativo che in uint64 diventa enorme.
func TestOvercommittedMachineHasZeroBudgetNotAHugeOne(t *testing.T) {
	if got := budgetFromVMStat(vmStatDiProva(9000000, 2000, 300, 50, 10), 128*gibDiProva); got != 0 {
		t.Fatalf("bilancio di una macchina sovraimpegnata = %.0f, atteso 0", got)
	}
}
