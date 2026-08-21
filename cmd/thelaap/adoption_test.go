package main

import (
	"os"
	"path/filepath"
	"testing"
)

// I difetti che questi test bloccano sono tutti lo stesso, visto da tre lati.
//
// Il 18/08/2026: ds4 girava sulla porta 8090, serviva deepseek-v4-flash, ed era
// il modello che l'utente stava usando in quel momento. Il pannello lo dava per
// «guasto», gli attribuiva la classe «convivente» — su 81 GB — e non contava un
// solo byte della sua occupazione, così la barra del menu diceva «0 GB usati di
// 137» con la macchina a 131.
//
// La ragione: i modelli si leggono dai file dei client, i runtime dalla
// configurazione, e ds4 stava solo nel primo elenco. Due fonti di verità per la
// stessa domanda — quali motori girano qui — e la seconda non sapeva del primo.
//
// Ogni pulsante della scheda ne discendeva: «Provalo» mandava porta 0, «Libera
// memoria» chiamava un servizio che il server non conosceva, e nessuno dei due
// diceva niente. Non erano dieci difetti: era questo.

// withPiFile scrive un models.json di prova e ci punta filePi(), ripulendo
// anche le cache che leggono i provider.
func withPiFile(t *testing.T, contenuto string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(p, []byte(contenuto), 0o644); err != nil {
		t.Fatal(err)
	}
	vecchio := PI_CFG
	PI_CFG = p
	forgetRemote()
	t.Cleanup(func() { PI_CFG = vecchio; forgetRemote() })
}

const piConDs4 = `{"providers":{
 "ds4":{"baseUrl":"http://127.0.0.1:8090/v1","models":[{"id":"deepseek-v4-flash","name":"DeepSeek V4 Flash"}]},
 "remoto":{"baseUrl":"https://aihub.esempio.local/v1","models":[{"id":"coder","name":"Coder"}]}
}}`

// Un motore locale che i client usano ma la configurazione non nomina deve
// entrare fra i runtime. Altrimenti il pannello sa del suo modello e non sa
// di lui: è la contraddizione da cui nasce tutto il resto.
func TestUnProviderLocaleFuoriConfigurazioneDiventaRuntime(t *testing.T) {
	withConfig(t, Config{Porta: 7070,
		Runtime: []RuntimeCfg{{Chiave: "omlx", Nome: "oMLX", Porta: 8000, Elenco: "/v1/models"}}})
	withPiFile(t, piConDs4)

	var visto *RuntimeCfg
	for _, r := range knownRuntimes() {
		if r.Chiave == "ds4" {
			c := r
			visto = &c
		}
	}
	if visto == nil {
		t.Fatal("ds4 gira sulla 8090 e i client lo usano, ma non è fra i runtime")
	}
	if visto.Porta != 8090 {
		t.Errorf("porta %d, attesa 8090: senza porta giusta «Provalo» manda 0", visto.Porta)
	}
}

// Un provider remoto NON è un runtime: non gira qui, non ha una porta da
// interrogare e nessun comando locale lo governa. Adottarlo riempirebbe la
// schermata «programmi» di righe che non si possono né fermare né misurare.
func TestUnProviderRemotoNonDiventaRuntime(t *testing.T) {
	withConfig(t, Config{Porta: 7070})
	withPiFile(t, piConDs4)

	for _, r := range knownRuntimes() {
		if r.Chiave == "remoto" {
			t.Fatal("un provider su un'altra macchina è finito fra i runtime locali")
		}
	}
}

// Quello che la configurazione dichiara vince: se ds4 è già scritto lì con i
// suoi comandi, l'adozione non deve sovrascriverlo con una voce nuda.
func TestLaConfigurazioneVinceSullAdozione(t *testing.T) {
	withConfig(t, Config{Porta: 7070,
		Runtime: []RuntimeCfg{{Chiave: "ds4", Nome: "DeepSeek", Porta: 8090,
			Elenco: "/v1/models", Ferma: "ds4.sh off", Avvia: "ds4.sh on"}}})
	withPiFile(t, piConDs4)

	quanti := 0
	for _, r := range knownRuntimes() {
		if r.Chiave != "ds4" {
			continue
		}
		quanti++
		if r.Ferma == "" || r.Avvia == "" {
			t.Error("l'adozione ha cancellato i comandi dichiarati in configurazione")
		}
	}
	if quanti != 1 {
		t.Errorf("ds4 compare %d volte: l'adozione ha duplicato una voce già dichiarata", quanti)
	}
}

// Il peso ignoto non è «leggero». Un modello mai misurato riceveva classe
// «convivente» perché 0 non supera la soglia: è la risposta più pericolosa
// possibile, perché quel modello può pesare 81 GB e il pannello invita a
// caricarlo insieme agli altri.
func TestPesoIgnotoNonSignificaConvivente(t *testing.T) {
	withConfig(t, Config{})
	c := classOf(Card{Model: Model{Runtime: "ds4"}, GB: 0}, 40)
	if c == ClasseConvivente {
		t.Error("un modello mai pesato è dato per convivente: se pesa 81 GB il pannello mente")
	}
	if c != ClasseIgnota {
		t.Errorf("classe %q, attesa %q", c, ClasseIgnota)
	}
}

// «Spento» e «guasto» sono due cose diverse e la differenza è tutta nel
// rimedio: uno si accende, l'altro si va a cercare in configurazione. Prima
// erano lo stesso stato, e ogni modello di un programma spento era «guasto».
func TestProgrammaSpentoNonEUnModelloGuasto(t *testing.T) {
	withConfig(t, Config{Runtime: []RuntimeCfg{
		{Chiave: "lmstudio", Nome: "LM Studio", Porta: 1234, Elenco: "/v1/models"},
	}})
	m := MemState{}
	spenti := map[string]bool{"lmstudio": true}

	if s := stateOf(Card{Model: Model{Runtime: "lmstudio", ID: "gemma", Servito: false}},
		m, map[string]bool{}, spenti); s != StatoSpento {
		t.Errorf("programma spento → %q, atteso %q", s, StatoSpento)
	}
	// Programma acceso che non conosce l'id: quello sì che è guasto.
	if s := stateOf(Card{Model: Model{Runtime: "lmstudio", ID: "gemma", Servito: false}},
		m, map[string]bool{}, map[string]bool{}); s != StatoGuasto {
		t.Errorf("programma acceso e id sconosciuto → %q, atteso %q", s, StatoGuasto)
	}
}
