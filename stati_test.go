package main

// stati_test.go — la macchina a stati provata sui casi che sono già costati
// qualcosa su questa macchina, senza toccare la macchina.
//
// Il metro l'ha stabilito menubar_contratto_test.go: un test che non distingue
// il codice giusto da quello rotto non serve. Per questo il registro dei comandi
// si prova con una configurazione finta caricata a mano — con cfg() vuota
// comandi() non restituisce niente e il test passerebbe senza guardare nulla.
//
// Lo scenario dei due modelli grandi insieme (27/07/2026) sta già in
// budget_test.go: qui si prova che stati.go usa QUELLA soglia, non una sua.

import (
	"encoding/json"
	"strings"
	"testing"
)

// La classe non è un'opinione: viene dalla soglia, e la soglia dalla macchina.
func TestClasseDipendeDallaSogliaNonDalModello(t *testing.T) {
	conConfig(t, Config{}) // nessun runtime residente dichiarato
	casi := []struct {
		nome   string
		gb     float64
		soglia float64
		atteso string
	}{
		{"sotto soglia convive", 32.4, 64, ClasseConvivente},
		{"sopra soglia pretende la macchina", 89.4, 64, ClasseEsclusivo},
		{"sulla soglia esatta è esclusivo", 64, 64, ClasseEsclusivo},
		{"peso non misurato non è esclusivo", 0, 64, ClasseConvivente},
		// Stesso modello, macchina più grande: cambia la classe, non il codice.
		{"su un Mac da 256 lo stesso modello convive", 89.4, 128, ClasseConvivente},
	}
	for _, c := range casi {
		got := classeDi(Scheda{GB: c.gb}, c.soglia)
		if got != c.atteso {
			t.Errorf("%s: peso %.1f soglia %.0f → %s, atteso %s", c.nome, c.gb, c.soglia, got, c.atteso)
		}
	}
}

// La classe «residente» non si indovina dal nome del programma: la dichiara la
// configurazione, ed è l'unica cosa che la decide.
func TestResidenteVieneDallaConfigurazioneNonDalNome(t *testing.T) {
	conConfig(t, Config{Runtime: []RuntimeCfg{
		{Chiave: "ollama", Nome: "Ollama", ModelloResidente: true},
		{Chiave: "mtplx", Nome: "MTPLX"},
	}})
	if c := classeDi(Scheda{Modello: Modello{Runtime: "ollama"}, GB: 0.3}, 64); c != ClasseResidente {
		t.Errorf("runtime dichiarato residente → %s", c)
	}
	if c := classeDi(Scheda{Modello: Modello{Runtime: "mtplx"}, GB: 0.3}, 64); c != ClasseConvivente {
		t.Errorf("runtime non dichiarato residente → %s, atteso convivente", c)
	}
}

// La soglia della classe DEVE essere quella dell'arbitro, non una copia.
// Se qualcuno ne introduce una seconda, il pannello dice «convivente» su un
// modello che il preflight rifiuta: è il difetto che questo test impedisce.
func TestLaClasseUsaLaSogliaDellArbitro(t *testing.T) {
	conConfig(t, Config{SogliaModelloGrandeGB: 50})
	soglia := float64(politicaCorrente().SogliaGrandeByte) / 1e9
	if soglia != 50 {
		t.Fatalf("la politica dice soglia %.1f GB, la configurazione 50", soglia)
	}
	// 60 GB: sopra la soglia → esclusivo per la classe E rifiutato dall'arbitro
	// quando c'è già un grande in memoria.
	if c := classeDi(Scheda{GB: 60}, soglia); c != ClasseEsclusivo {
		t.Errorf("60 GB con soglia 50 → %s", c)
	}
	b := Budget{TotaleByte: gb(128), RiservaSOByte: gb(24),
		Occupato: []OccupazioneRuntime{{Chiave: "mtplx", Nome: "MTPLX", PesoByte: gb(55)}}}
	if v := b.Ammette(gb(60), politicaCorrente()); v.Ammesso {
		t.Error("l'arbitro ammette due grandi mentre la classe li dice esclusivi: le due soglie sono divergenti")
	}
}

// Un modello pronto e basta, e non può essere il residente della ricerca.
func TestUnSoloProntoEMaiIlResidente(t *testing.T) {
	ss := []SchedaStato{
		{Scheda: Scheda{TokS: 0}, Stato: StatoInMemoria, Classe: ClasseResidente},
		{Scheda: Scheda{TokS: 3.7}, Stato: StatoInMemoria, Classe: ClasseConvivente},
		{Scheda: Scheda{TokS: 61.4}, Stato: StatoInMemoria, Classe: ClasseConvivente},
		{Scheda: Scheda{TokS: 99}, Stato: StatoSpento, Classe: ClasseConvivente},
	}
	if i := scegliPronto(ss); i != 2 {
		t.Fatalf("pronto = indice %d, atteso 2 (il più veloce fra quelli in memoria che conversano)", i)
	}
	// Solo residenti in memoria: meglio proporre quello che niente.
	if scegliPronto([]SchedaStato{{Stato: StatoInMemoria, Classe: ClasseResidente}}) != 0 {
		t.Error("con solo un residente caricato non propone niente")
	}
	// Niente in memoria: niente pronto, e non deve inventarlo.
	if scegliPronto([]SchedaStato{{Stato: StatoSpento}}) != -1 {
		t.Error("propone un pronto con la memoria vuota")
	}
}

// Le priorità dello stato. Un modello che sta scaricando non è "spento", e uno
// che non risponde non è "in memoria" perché il programma è acceso.
func TestPrioritaDelloStato(t *testing.T) {
	conConfig(t, Config{})
	m := MemStato{Caricati: []ModelloInRAM{{Nome: "publisher/Modello-8bit"}}}
	arrivo := map[string]bool{"publisher/in-arrivo": true}

	if s := statoDi(Scheda{Modello: Modello{ID: "publisher/Modello-8bit", Servito: true}}, m, arrivo); s != StatoInMemoria {
		t.Errorf("caricato → %s", s)
	}
	if s := statoDi(Scheda{Modello: Modello{ID: "publisher/Modello-8bit", Servito: false}}, m, arrivo); s != StatoGuasto {
		t.Errorf("non servito ma caricato → %s, atteso guasto: l'id in configurazione è sbagliato", s)
	}
	if s := statoDi(Scheda{Modello: Modello{ID: "publisher/in-arrivo", Servito: false}}, m, arrivo); s != StatoInArrivo {
		t.Errorf("in scaricamento → %s, atteso in-arrivo", s)
	}
	if s := statoDi(Scheda{Modello: Modello{ID: "publisher/altro", Servito: true}}, m, arrivo); s != StatoSpento {
		t.Errorf("dichiarato e non caricato → %s", s)
	}
	// Maiuscole: i nomi arrivano da servizi di terzi e non combaciano mai.
	if s := statoDi(Scheda{Modello: Modello{ID: "PUBLISHER/modello-8BIT", Servito: true}}, m, arrivo); s != StatoInMemoria {
		t.Error("il confronto con i caricati è sensibile alle maiuscole")
	}
}

// Il registro dei comandi deve contenere id che le rotte accettano davvero.
// È il difetto del 16/08/2026, riprodotto qui in modo che non torni: la voce
// del menu mandava `stoppa-tutto` e il server conosceva `ferma-tutto`.
func TestIdDelRegistroSonoEseguibili(t *testing.T) {
	conConfig(t, Config{
		FermaTutto:     "ferma-tutto.sh",
		RiaccendiTutto: "riaccendi.sh",
		Strumenti: []StrumentoCfg{
			{ID: "stato", Nome: "Stato rapido", Comando: "aistack.py", Durata: "2 s"},
			{ID: "controllo-completo", Nome: "Controllo completo", Comando: "aicheck.py"},
		},
		Regimi: []RegimeCfg{{Chiave: "esclusivo", Nome: "Un modello solo", RuntimeAttivo: "omlx"}},
		Runtime: []RuntimeCfg{
			{Chiave: "mtplx", Nome: "MTPLX", Riavvia: "riavvia-mtplx"},
			{Chiave: "omlx", Nome: "oMLX", Ferma: "ferma", Avvia: "avvia"},
			{Chiave: "muto", Nome: "Muto"}, // senza comandi: non deve comparire
		},
	})
	reg := comandi()
	if len(reg) < 6 {
		t.Fatalf("registro con %d voci: il test non sta guardando niente", len(reg))
	}
	visti := map[string]bool{}
	for _, c := range reg {
		visti[c.ID] = true
		switch c.Rotta {
		case "/api/esegui":
			if _, ok := comandoAmmesso(c.ID); !ok {
				t.Errorf("%q è nel registro ma /api/esegui lo rifiuta", c.ID)
			}
			if c.Corpo["cmd"] != c.ID {
				t.Errorf("%q manda cmd=%q: il corpo non combacia con l'id", c.ID, c.Corpo["cmd"])
			}
		case "/api/regime":
			if c.Corpo["chiave"] == "" {
				t.Errorf("%q va su /api/regime senza chiave", c.ID)
			}
		case "/api/servizio":
			if c.Corpo["servizio"] == "" || c.Corpo["azione"] == "" {
				t.Errorf("%q va su /api/servizio senza servizio o azione", c.ID)
			}
		default:
			t.Errorf("%q punta a %q, che non è una rotta prevista", c.ID, c.Rotta)
		}
	}
	if visti["riavvia:muto"] {
		t.Error("offre il riavvio di un programma che non ha comandi per farlo")
	}
	for _, atteso := range []string{"ferma-tutto", "riaccendi-tutto", "stato", "regime:esclusivo", "riavvia:mtplx", "riavvia:omlx"} {
		if !visti[atteso] {
			t.Errorf("%q manca dal registro", atteso)
		}
	}
}

// Senza comandi dichiarati non si inventa niente: un pulsante che non fa nulla
// è peggio di un pulsante assente.
func TestSenzaComandiIlRegistroNonInventa(t *testing.T) {
	conConfig(t, Config{})
	for _, c := range comandi() {
		t.Errorf("configurazione vuota, e il registro offre %q su %s", c.ID, c.Rotta)
	}
}

// I pulsanti che il pannello mostra devono corrispondere ai comandi che il
// server sa eseguire. Se le due regole si separano, il pulsante c'è e non fa
// niente — in silenzio, come le voci di menu del 16/08/2026.
func TestPulsantiDeiProgrammiSeguonoIComandi(t *testing.T) {
	conConfig(t, Config{Runtime: []RuntimeCfg{
		{Chiave: "completo", Nome: "Completo", Porta: 9001, Avvia: "su", Ferma: "giu", Riavvia: "ri"},
		{Chiave: "senzariavvio", Nome: "Senza riavvio", Porta: 9002, Avvia: "su", Ferma: "giu"},
		{Chiave: "solostop", Nome: "Solo stop", Porta: 9003, Ferma: "giu"},
		{Chiave: "muto", Nome: "Muto", Porta: 9004},
	}})
	atteso := map[string][3]bool{ // avvia, ferma, riavvia
		"completo":     {true, true, true},
		"senzariavvio": {true, true, true}, // ferma + avvia vale come riavvio
		"solostop":     {false, true, false},
		"muto":         {false, false, false},
	}
	for _, r := range runtimeConfigurati() {
		a := atteso[r.Chiave]
		if r.PuoAvviare != a[0] || r.PuoFermare != a[1] || r.PuoRiavviare != a[2] {
			t.Errorf("%s: avvia/ferma/riavvia = %v/%v/%v, atteso %v/%v/%v",
				r.Chiave, r.PuoAvviare, r.PuoFermare, r.PuoRiavviare, a[0], a[1], a[2])
		}
		// E la capacità dichiarata deve avere davvero un comando dietro.
		for i, azione := range []string{"start", "stop", "restart"} {
			puo := [3]bool{r.PuoAvviare, r.PuoFermare, r.PuoRiavviare}[i]
			var rc RuntimeCfg
			for _, x := range cfg().Runtime {
				if x.Chiave == r.Chiave {
					rc = x
				}
			}
			if puo != (comandoServizio(rc, azione) != "") {
				t.Errorf("%s: dichiara %s=%v ma comandoServizio dice il contrario", r.Chiave, azione, puo)
			}
		}
	}
}

// Le righe di shell restano sul server: /api/runtime non le porta al browser.
func TestRuntimeNonEsponeIComandi(t *testing.T) {
	conConfig(t, Config{Runtime: []RuntimeCfg{
		{Chiave: "x", Nome: "X", Porta: 9099, Avvia: "segreto-avvia", Ferma: "segreto-ferma",
			Riavvia: "segreto-riavvia", ScaricaModello: "segreto-scarica {modello}"},
	}})
	b, err := json.Marshal(runtimeConfigurati())
	if err != nil {
		t.Fatal(err)
	}
	for _, brutto := range []string{"segreto-avvia", "segreto-ferma", "segreto-riavvia", "segreto-scarica"} {
		if strings.Contains(string(b), brutto) {
			t.Errorf("/api/runtime porta al browser il comando %q", brutto)
		}
	}
}

// Nessuna lista che esce come `null`: la pagina la scorre sempre e cadrebbe.
func TestElenchiMaiNil(t *testing.T) {
	conConfig(t, Config{})
	if modelliConStato().Modelli == nil {
		t.Error("Modelli è nil: uscirebbe null e la pagina cade appena la scorre")
	}
	if comandi() == nil {
		t.Error("comandi() è nil")
	}
	if scarichiInCorso() == nil {
		t.Error("scarichiInCorso() è nil")
	}
}
