package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Flash-Next gira su un secondo MTPLX, su un'altra porta. Per il server sono
// due programmi (si accendono e si spengono con comandi diversi), ma chi
// guarda il pannello cerca il modello sotto MTPLX: il programma dichiara sotto
// quale sezione va mostrato, e la dichiarazione arriva alla pagina.
func TestProgrammaDichiaraSottoQualeSezioneVaMostrato(t *testing.T) {
	withConfig(t, Config{Runtime: []RuntimeCfg{
		{Chiave: "mtplx", Nome: "MTPLX", Porta: 8080},
		{Chiave: "qwen-next", Nome: "MTPLX esclusivo", Porta: 8082, Esclusivo: true, Gruppo: "mtplx"},
	}})
	rt := configuredRuntimes()
	if rt[0].Gruppo != "" || rt[1].Gruppo != "mtplx" {
		t.Fatalf("gruppo non dichiarato alla pagina: %+v", rt)
	}
	b, _ := json.Marshal(rt[1])
	if !strings.Contains(string(b), `"gruppo":"mtplx"`) {
		t.Fatalf("la pagina non riceve il campo gruppo: %s", b)
	}
}

func TestIlCampoGruppoSiLeggeDallaConfigurazione(t *testing.T) {
	var c Config
	err := json.Unmarshal([]byte(`{"runtime":[{"chiave":"qwen-next","nome":"x","porta":8082,"gruppo":"mtplx"}]}`), &c)
	if err != nil || c.Runtime[0].Gruppo != "mtplx" {
		t.Fatalf("gruppo non letto dalla configurazione: %v %+v", err, c.Runtime)
	}
}

func corpoFunzione(t *testing.T, nome, successiva string) string {
	t.Helper()
	i := strings.Index(UI, "function "+nome+"(")
	if i < 0 {
		t.Fatalf("funzione %s assente", nome)
	}
	fine := strings.Index(UI[i+1:], "function "+successiva+"(")
	if fine < 0 {
		t.Fatalf("non riesco a delimitare %s", nome)
	}
	return UI[i : i+1+fine]
}

// La pagina non apre una sezione per un programma che ne dichiara un'altra:
// i suoi modelli finiscono in quella.
func TestLaPaginaRaggruppaSottoIlProgrammaDichiarato(t *testing.T) {
	corpo := corpoFunzione(t, "vistaModelli", "mostraTip")
	for _, atteso := range []string{
		"sezioneDi(m.runtime)",                       // il modello va nella sezione dichiarata dal suo programma
		".filter(g=>sezioneDi(g.chiave)===g.chiave)", // nessuna intestazione per chi sta sotto un altro
	} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("vistaModelli non raggruppa per sezione dichiarata: manca %q", atteso)
		}
	}
	if !strings.Contains(UI, "function sezioneDi(") {
		t.Error("manca sezioneDi")
	}
}

// Una sezione è accesa se lo è uno qualunque dei programmi che ospita: in
// modalità esclusiva MTPLX sulla 8080 è spento ma Flash-Next sulla 8082 no.
func TestLaSezioneEAccesaSeLoEUnoDeiSuoiProgrammi(t *testing.T) {
	corpo := corpoFunzione(t, "rigaGruppo", "rigaModello")
	if !strings.Contains(corpo, "ospiti.some(x=>x.attivo)") {
		t.Error("l'intestazione guarda solo il programma principale")
	}
}

// «Esclusivo» lo decide il server dal peso (da sogliaGB in su) o dal
// programma: la riga lo mostra senza che nessuno lo scriva modello per modello.
func TestLaRigaMostraDaSolaCheUnModelloEEsclusivo(t *testing.T) {
	corpo := corpoFunzione(t, "rigaModello", "vistaModelli")
	if !strings.Contains(corpo, "m.classe==='esclusivo'") || !strings.Contains(corpo, "T().cl.esclusivo") {
		t.Error("la riga del modello non mostra la classe esclusivo")
	}
}

func TestOgniModelloSopraLaSogliaEEsclusivoSenzaDichiararlo(t *testing.T) {
	withConfig(t, Config{Runtime: []RuntimeCfg{{Chiave: "lmstudio", Nome: "LM Studio", Porta: 1234}}})
	if got := classOf(Card{Model: Model{Runtime: "lmstudio", ID: "grande"}, GB: 85}, 40); got != ClasseEsclusivo {
		t.Fatalf("un modello da 85 GB con soglia 40 è %q", got)
	}
	if got := classOf(Card{Model: Model{Runtime: "lmstudio", ID: "piccolo"}, GB: 30}, 40); got != ClasseConvivente {
		t.Fatalf("un modello da 30 GB con soglia 40 è %q", got)
	}
}

// La scheda in alto conta le sezioni che si vedono nella tabella, non le
// porte: Flash-Next gira su un secondo MTPLX, ma non è un programma in più.
func TestLaSchedaDeiProgrammiContaLeSezioni(t *testing.T) {
	corpo := corpoFunzione(t, "kpi", "graficoMemoria")
	for _, atteso := range []string{
		"S.runtime.filter(r=>sezioneDi(r.chiave)===r.chiave)", // quante sezioni ci sono
		"sezioneDi(x.chiave)===r.chiave && x.attivo",          // accesa se lo è uno dei suoi programmi
	} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("kpi conta i programmi invece delle sezioni: manca %q", atteso)
		}
	}
	if strings.Contains(corpo, "S.runtime.length") {
		t.Error("kpi conta ancora ogni porta come un programma")
	}
}
