package main

import (
	"regexp"
	"strings"
	"testing"
)

// La pagina deve partire da una risposta, non dall'albero interno del
// programma. Questi sono i quattro ingressi della vista semplice.
func TestPaginaParteDallaPanoramicaSemplice(t *testing.T) {
	if !strings.Contains(UI, "schermo:'home'") {
		t.Fatal("la pagina non parte dalla panoramica")
	}
	for _, funzione := range []string{
		"function vistaHome()",
		"function vistaModelli()",
		"function vistaConfigurazioni()",
		"function vistaManutenzione()",
	} {
		if !strings.Contains(UI, funzione) {
			t.Errorf("manca l'ingresso semplice %s", funzione)
		}
	}
}

func TestConfigurazioniMostranoTreClientInsieme(t *testing.T) {
	i := strings.Index(UI, "function vistaConfigurazioni()")
	if i < 0 {
		t.Fatal("vista configurazioni assente")
	}
	fine := strings.Index(UI[i+1:], "function controllaOra()")
	if fine < 0 {
		t.Fatal("non riesco a delimitare vistaConfigurazioni")
	}
	corpo := UI[i : i+1+fine]
	for _, atteso := range []string{"Pi", "OpenCode", "DSH", "cambiaClient", "salva()"} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("la matrice configurazioni non contiene %q", atteso)
		}
	}
}

func TestControlloRapidoHaUnAzionePrimaria(t *testing.T) {
	if !strings.Contains(UI, "function controllaOra()") ||
		!strings.Contains(UI, "x.id==='controllo'") {
		t.Fatal("il controllo rapido non sceglie il comando di controllo dichiarato")
	}
}

func TestGellowMostraCheStaDavveroLavorando(t *testing.T) {
	for _, atteso := range []string{
		"gellowOccupato", "Gellow sta leggendo tutto il controllo", "gellow-gira", "gellow-lampeggia",
		"gellow-tempo", "setInterval", "In corso…", "aria-live=\"polite\"",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("attesa di Gellow senza %q", atteso)
		}
	}
}

func TestGellowNonForzaLoScrollMentreSiLegge(t *testing.T) {
	for _, atteso := range []string{
		"gellowScroll", "gellow-risposta-nuova", "vecchioTop", "S.gellowScroll==='risposta'",
		"else corpo.scrollTop=Math.min(vecchioTop", "messaggio.nuova=false",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("scroll di Gellow senza %q", atteso)
		}
	}
	if strings.Contains(UI, "if(S.tab==='aiuto'){ const c=$('insp-corpo'); c.scrollTop=c.scrollHeight; }") {
		t.Fatal("Gellow forza ancora lo scroll in fondo a ogni aggiornamento")
	}
}

func TestConfigurazioniPartonoDaiModelliConLeSpunte(t *testing.T) {
	for _, atteso := range []string{
		"ordineConfigurazioni", "Con spunte prima", "['pi','Pi']", "['opencode','OpenCode']", "['dsh','DSH']",
		"Nome A–Z", "localStorage.setItem('ordine-configurazioni'", "ordinati.map",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("ordinamento configurazioni senza %q", atteso)
		}
	}
}

func TestRicercaHuggingFaceHaFiltriCombinabili(t *testing.T) {
	for _, atteso := range []string{
		"function risultatiHF()", "hfSort:'trendingScore'", "hfFormato", "hfMemoria",
		"hfEta", "hfAutore", "hfMinLikes", "hfMinDownloads", "hfMinGB", "hfMaxGB",
		"Tendenza", "Download", "Like", "Più recenti", "Tutti i formati", "scelteFormato",
		"S.hfFormato!=='tutti'&&formato!==S.hfFormato",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("ricerca HuggingFace senza %q", atteso)
		}
	}
}

func TestRisultatiHuggingFaceHannoLinkEDateDistinte(t *testing.T) {
	for _, atteso := range []string{
		"https://huggingface.co/", "Apri su HuggingFace", "Open on HuggingFace",
		"pubblicato ", "published ", "aggiornato ", "updated ",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("risultato HuggingFace senza %q", atteso)
		}
	}
}

func TestPollingNonChiudeIFiltriHuggingFace(t *testing.T) {
	for _, atteso := range []string{
		"function staModificando()", "if(!staModificando())disegnaTutto()",
		"S.hfFiltriAperti=!S.hfFiltriAperti", "richiesta!==S.hfRichiesta",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("interazione HuggingFace non protetta dal polling: manca %q", atteso)
		}
	}
}

func TestInterfacciaNonUsaSelectNative(t *testing.T) {
	if strings.Contains(UI, "<select") {
		t.Fatal("è ricomparsa una select nativa, che nella finestra macOS prende il focus ma non apre il menu")
	}
	for _, atteso := range []string{"function scelteBottoni", "setFormatoHF", "setMemoriaHF", "setEtaHF"} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("scelte a pulsanti senza %q", atteso)
		}
	}
}

// ── la pagina del 03/10/2026: struttura di prima, contenuti a dati ──────────
//
// La richiesta era doppia: tenere la struttura grafica (navigazione a sinistra,
// pagina al centro, Gellow che si apre a destra) e togliere il superfluo —
// dati strutturati, quindi tabelle, grafici e schemi; niente paragrafi scritti
// dal codice. I test qui sotto difendono queste due cose, non le parole.

func mustContain(t *testing.T, cosa string, attesi ...string) {
	t.Helper()
	for _, atteso := range attesi {
		if !strings.Contains(UI, atteso) {
			t.Errorf("%s: manca %q", cosa, atteso)
		}
	}
}

func TestStrutturaATreColonneConGellowADestra(t *testing.T) {
	mustContain(t, "struttura a tre colonne",
		`<nav id="lato"`, "function disegnaLato()", `<aside id="insp">`,
		"main.insp-aperto{grid-template-columns:224px minmax(0,1fr) 340px}",
		"main.insp-aperto #insp{display:flex}")
	// Gellow si apre da qualunque sezione: l'ispettore dipende solo da
	// inspAperto. Quando dipendeva anche dalla schermata, il clic cambiava lo
	// stato JavaScript ma non apriva nessun pannello.
	mustContain(t, "Gellow apribile ovunque",
		"classList.toggle('insp-aperto',!!S.inspAperto)", `aria-controls="insp"`, "vaiTab('aiuto')")
	if strings.Contains(UI, "senza-insp") {
		t.Error("è tornata la classe che nascondeva l'ispettore fuori da una schermata")
	}
}

func TestRisultatoControlloSiPuoUsare(t *testing.T) {
	mustContain(t, "esito del controllo",
		"function copiaRisultato()", "function chiediRisultato()", "function chiudiInsp()",
		"Chiedi a Gellow", "iconaGellow")
}

// Lo schema è programmi → modelli: un gruppo per programma, una riga per
// modello, e su ogni riga il suo interruttore.
func TestTabellaModelliEUnoSchemaProgrammiModelli(t *testing.T) {
	i := strings.Index(UI, "function vistaModelli()")
	fine := strings.Index(UI[i+1:], "function mostraTip(")
	if i < 0 || fine < 0 {
		t.Fatal("non riesco a delimitare la tabella dei modelli")
	}
	corpo := UI[i : i+1+fine]
	for _, atteso := range []string{"S.runtime.map(rt=>({chiave:rt.chiave", "rigaGruppo(g)", "rigaModello",
		"T().colModel", "T().colWeight", "tok/s", "Client", "Chat e codice", "Strumenti", "Remoti"} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("la tabella non contiene %q", atteso)
		}
	}
	mustContain(t, "righe della tabella", "function rigaGruppo(g)", "function rigaModello(m)",
		"interruttore(m)", "processoDi(k)", "p.correnteByte")
	// chi ha qualcosa di acceso sta in cima, dentro il gruppo e fra i gruppi
	mustContain(t, "ordine", "primaGliAttivi", "g.modelli.some(accesoDi) ? 0",
		"impostaOrdineModelli", "localStorage.setItem('ordine-modelli'", "'grandi'", "'veloci'")
}

// Un gesto solo: l'interruttore. La pagina non mette in fila «accendi il
// programma» e «carica il modello», e non decide lei cosa fare al programma.
func TestAccendereESpegnereEUnGestoSolo(t *testing.T) {
	mustContain(t, "interruttore del modello",
		"function commuta(id)", "async function accendi(id)", "async function spegni(id)",
		"/api/modello/accendi", "/api/modello/spegni",
		`role="switch"`, `aria-checked="`, "i.avviaProgramma", "i.fermaProgramma",
		"if(e.key==='Escape')", "else if(S.inspAperto)chiudiInsp()",
		"/api/modello/esamina", "erroreArchivio")
	// Le due chiamate di prima — scaricare il modello, poi fermare il
	// programma — le fa il server in una. Se la pagina torna a chiamarle da sé,
	// torna anche il programma rimasto acceso a vuoto.
	for _, vecchio := range []string{"/api/modello/libera-memoria", "/api/attiva", "function disattiva(", "function liberaTuttaRAM("} {
		if strings.Contains(UI, vecchio) {
			t.Errorf("la pagina usa ancora %s invece dell'interruttore", vecchio)
		}
	}
}

// I motivi per cui un interruttore è bloccato li decide il server
// (switchOf, lifecycle.go) e arrivano come chiavi. La pagina deve saperle
// leggere tutte: una chiave senza testo dà un interruttore spento e muto.
func TestOgniBloccoDellInterruttoreHaIlSuoTesto(t *testing.T) {
	sorgente := mustRead(t, "lifecycle.go")[0]
	chiavi := map[string]bool{}
	for _, m := range regexp.MustCompile(`Blocco: "([a-z-]+)"`).FindAllStringSubmatch(sorgente, -1) {
		chiavi[m[1]] = true
	}
	if len(chiavi) < 5 {
		t.Fatalf("trovate solo %d chiavi di blocco in lifecycle.go: il test non sta guardando niente", len(chiavi))
	}
	i := strings.Index(UI, "function bloccoTesto(")
	if i < 0 {
		t.Fatal("bloccoTesto non c'è più")
	}
	corpo := UI[i : i+1500]
	for k := range chiavi {
		if !strings.Contains(corpo, "'"+k+"'") {
			t.Errorf("la pagina non sa spiegare il blocco %q", k)
		}
	}
}

// Spegnere tutto, o un programma con dei modelli dentro, dice prima cosa
// spegne e aspetta una conferma.
func TestGliSpegnimentiLarghiChiedonoConferma(t *testing.T) {
	mustContain(t, "conferme",
		"function conferme()", "function fermaTutto()", "S.armato==='ferma'",
		"function spegniProgramma(chiave)", "S.armato='stop:'+chiave", "S.armato!=='regime:'+chiave")
}

// La memoria è un grafico: un segmento per programma, coi numeri misurati sui
// processi, e ogni valore è anche nella legenda — il passaggio del mouse
// aggiunge, non nasconde.
func TestPanoramicaDisegnaLaMemoriaPerProgramma(t *testing.T) {
	mustContain(t, "grafico della memoria",
		"function graficoMemoria()", "function kpi()", "p.correnteByte", "p.modelli",
		"mem-seg", "mem-riserva", `class="legenda"`, "function coloreProgramma(chiave)",
		"--serie-1:", "--serie-8:", "data-tip-v", "function mostraTip(e)", "textContent")
	// Il colore segue il programma, non la sua posizione fra quelli accesi:
	// spegnerne uno non deve ridipingere gli altri.
	if !strings.Contains(UI, "S.runtime.findIndex(r=>r.chiave===chiave)") {
		t.Error("il colore di un programma non dipende più dalla sua posizione in configurazione")
	}
}

// Niente prosa scritta dal codice. Le frasi sulla macchina, il glossario e i
// sottotitoli spiegati sono stati tolti apposta: il testo libero lo scrive
// Gellow, quando glielo si chiede.
func TestLaPaginaNonScriveProsa(t *testing.T) {
	for _, vecchio := range []string{"function frasi()", "function suggeriti()", "const LESSICO", "function motivoDi(",
		`<p class="sub"`, "hero-semplice", "function vistaMemoria()", "function vistaProgrammi()"} {
		if strings.Contains(UI, vecchio) {
			t.Errorf("è tornato %q: la pagina mostra dati, non paragrafi", vecchio)
		}
	}
	mustContain(t, "spiegazioni affidate a Gellow", "function spiega(k)", "chiedi(IT() ? 'Cosa vuol dire")
}

// La barra non può dire «liberi 82 GB» mentre un'altra applicazione ne tiene
// 27: il tratto libero è quanto l'arbitro concede davvero, il resto è delle
// altre applicazioni del Mac e va disegnato come tale.
func TestLaBarraDistingueLeAltreAppDalLibero(t *testing.T) {
	barra := corpoFunzione(t, "graficoMemoria", "avvisi")
	for _, atteso := range []string{"disponibiliGB()", "T().otherApps", "mem-altro"} {
		if !strings.Contains(barra, atteso) {
			t.Errorf("graficoMemoria: manca %q", atteso)
		}
	}
	mustContain(t, "testi e stile delle altre app", `otherApps:"altre app"`, `otherApps:"other apps"`, ".mem-altro{")
}

// Nel dettaglio il giudizio «ci sta o no» mostra un solo numero di memoria
// libera, lo stesso della scheda in alto: prima ne mostrava due diversi.
func TestIlGiudizioMostraUnSoloNumeroDiMemoriaLibera(t *testing.T) {
	tabella := corpoFunzione(t, "tabellaVerdetto", "esitoHTML")
	if strings.Contains(tabella, "v.liberaByte") {
		t.Error("la tabella del giudizio mostra ancora due memorie libere diverse")
	}
}

// Col dettaglio aperto a fianco la tabella è stretta: tiene nome, uso e peso e
// non costringe a scorrere di lato. Contesto, velocità e client del modello
// scelto si leggono nel dettaglio, che è lì accanto.
func TestColDettaglioApertoLaTabellaNonScorreDiLato(t *testing.T) {
	mustContain(t, "tabella col dettaglio aperto",
		"@media(min-width:1181px) and (max-width:1560px){",
		"main.insp-aperto .c-ctx,main.insp-aperto .c-tok,main.insp-aperto .c-cl{display:none}",
		"main.insp-aperto .tab-testa,main.insp-aperto .tab-riga{grid-template-columns:44px minmax(160px,1.7fr) minmax(110px,.9fr) 130px}",
		"col('veloci','tok/s','c-tok')")
}

// Le schede sopra la tabella sono diventate sette: quando non ci stanno su una
// riga vanno a capo, invece di spingere la pagina a scorrere di lato.
func TestLeSchedeVannoACapoInveceDiFarScorrereLaPagina(t *testing.T) {
	mustContain(t, "schede sopra la tabella", ".strumenti .seg{flex:0 1 auto;flex-wrap:wrap}")
}

// Il contesto si legge allo stesso modo nella riga e nel dettaglio: prima la
// riga diceva 131k e il dettaglio dello stesso modello 128k.
func TestIlContestoSiLeggeUgualeNellaRigaENelDettaglio(t *testing.T) {
	dettaglio := corpoFunzione(t, "inspModello", "formNomi")
	if strings.Contains(dettaglio, "/1024") || !strings.Contains(dettaglio, "m.context/1000") {
		t.Error("il dettaglio arrotonda il contesto diversamente dalla riga")
	}
}

// «Riaccendi tutto» e «Ferma tutto» stanno insieme: se accanto alle schede non
// c'è posto vanno a capo tutti e due, a destra. Prima «Ferma tutto» finiva da
// solo sulla riga sotto, a sinistra.
func TestLeAzioniSopraLaTabellaVannoACapoInsieme(t *testing.T) {
	tabella := corpoFunzione(t, "vistaModelli", "mostraTip")
	if !strings.Contains(tabella, `</div><div class="strumenti-azioni">`) {
		t.Error("le azioni non sono raccolte in un gruppo solo")
	}
	if strings.Contains(tabella, `<span class="sp"></span>`) {
		t.Error("c'è ancora lo spaziatore che lasciava i pulsanti liberi di separarsi")
	}
	mustContain(t, "barra sopra la tabella",
		".strumenti-azioni{display:flex;align-items:center;gap:8px;margin-left:auto;flex:none}",
		".strumenti .seg button{padding:0 10px}")
}

// L'intestazione «Contesto» ha la sua colonna: a 62px toccava «Peso».
func TestLaColonnaDelContestoHaSpazioPerLaSuaIntestazione(t *testing.T) {
	mustContain(t, "griglia della tabella",
		"grid-template-columns:44px minmax(200px,1.7fr) minmax(150px,.9fr) 76px 150px 60px 108px")
}

// Sette schede e due pulsanti devono stare su una riga anche nella finestra
// ingrandita dell'app: la scheda dei modelli che accettano immagini si chiama
// come l'etichetta che quei modelli portano nella riga, «immagini».
func TestLaSchedaDelleImmaginiHaUnNomeCorto(t *testing.T) {
	tabella := corpoFunzione(t, "vistaModelli", "mostraTip")
	if !strings.Contains(tabella, "immagini:[IT()?'Immagini':'Images'") {
		t.Error("la scheda delle immagini ha ancora il nome lungo")
	}
}

// Nella barra la riga del tetto dice di quale programma è.
func TestLaRigaDelTettoDiceDiQualeProgrammaE(t *testing.T) {
	barra := corpoFunzione(t, "graficoMemoria", "avvisi")
	if !strings.Contains(barra, "M().tettoDi") {
		t.Error("la barra non dice di chi è il tetto")
	}
	mustContain(t, "testi del tetto", `ceilingOf:"tetto di"`, `ceilingOf:"ceiling of"`)
}
