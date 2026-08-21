package main

import (
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

func TestConfigurazioniMostranoPiEOpenCodeInsieme(t *testing.T) {
	i := strings.Index(UI, "function vistaConfigurazioni()")
	if i < 0 {
		t.Fatal("vista configurazioni assente")
	}
	fine := strings.Index(UI[i+1:], "function controllaOra()")
	if fine < 0 {
		t.Fatal("non riesco a delimitare vistaConfigurazioni")
	}
	corpo := UI[i : i+1+fine]
	for _, atteso := range []string{"Pi", "OpenCode", "cambiaClient", "salva()"} {
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

func TestRisultatoControlloSiPuoUsare(t *testing.T) {
	for _, azione := range []string{"function copiaRisultato()", "function chiediRisultato()", "function chiudiInsp()"} {
		if !strings.Contains(UI, azione) {
			t.Errorf("manca %s", azione)
		}
	}
	if !strings.Contains(UI, "main.insp-aperto #insp") {
		t.Fatal("su uno schermo piccolo l'aiutante resterebbe invisibile")
	}
	// La classe che allarga il centro non deve nascondere Gellow proprio nelle
	// pagine Panoramica e Controlla. Era il motivo per cui il clic cambiava lo
	// stato JavaScript ma non apriva nessun pannello.
	if !strings.Contains(UI, "S.schermo!=='modelli'&&!S.inspAperto") {
		t.Fatal("aprendo Gellow fuori da Modelli, senza-insp lo nasconderebbe")
	}
	for _, atteso := range []string{"Chiedi a Gellow", "aria-controls=\"insp\"", "iconaGellow"} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("manca l'accesso visibile a Gellow: %q", atteso)
		}
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

func TestListaModelliNonUsaPiuClassiEPesiComeColonne(t *testing.T) {
	i := strings.Index(UI, "function vistaModelli()")
	fine := strings.Index(UI[i+1:], "const metrica")
	if i < 0 || fine < 0 {
		t.Fatal("non riesco a delimitare la lista modelli")
	}
	corpo := UI[i : i+1+fine]
	for _, atteso := range []string{"A cosa serve", "Programma", "Dove compare", "Strumenti interni", "nomeReale"} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("la nuova lista non contiene %q", atteso)
		}
	}
	if strings.Contains(corpo, "T().colClass") || strings.Contains(corpo, "T().colWeight") {
		t.Fatal("la lista semplice mostra ancora classe o peso come colonne")
	}
}

func TestListaModelliHaOrdinamentoComprensibile(t *testing.T) {
	for _, atteso := range []string{
		"ordineModelli", "Attivi prima", "Usati di recente", "Più grandi", "Più veloci", "Nome A–Z", "Programma",
		"primaGliAttivi", "localStorage.setItem('ordine-modelli'", "Chat e codice", "Strumenti",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("ordinamento o filtro modelli senza %q", atteso)
		}
	}
}

func TestConfigurazioniPartonoDaiModelliConLeSpunte(t *testing.T) {
	for _, atteso := range []string{
		"ordineConfigurazioni", "Con spunte prima", "['pi','Pi']", "['opencode','OpenCode']",
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
		"Tendenza", "Download", "Like", "Più recenti",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("ricerca HuggingFace senza %q", atteso)
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

func TestModelliPossonoLiberareTuttaLaRAM(t *testing.T) {
	for _, atteso := range []string{
		"function liberaTuttaRAM()", "Libera RAM modelli", "RAM dei modelli libera",
		"Resta così finché Pi, OpenCode o Gellow non ne usa uno", "-webkit-line-clamp:2",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("gestione RAM o righe adattive senza %q", atteso)
		}
	}
}

func TestAzioniModelloSonoSempliciEVisibili(t *testing.T) {
	for _, atteso := range []string{
		`makeReady:"Attiva"`, `if(e.key==='Escape')`, `else if(S.inspAperto)chiudiInsp()`,
		"function attiva(id)", "/api/attiva", "erroreArchivio", "/api/modello/esamina",
		`.mod-row>.pill`, `if(s==='pronto')return [IT()?'attivo'`, "function disattiva(id)",
		"Disattiva modello", "/api/modello/libera-memoria", "SE VUOI SPEGNERLO",
	} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("azione o stato modello senza %q", atteso)
		}
	}
}

func TestPanoramicaMostraChiOccupaLaMemoria(t *testing.T) {
	for _, atteso := range []string{"mem-home-barra", "Memoria usata dai modelli", "p.correnteByte", "p.modelli"} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("grafico memoria semplice senza %q", atteso)
		}
	}
}

func TestMemoriaMostraLoStatoSenzaProposteCasuali(t *testing.T) {
	for _, atteso := range []string{"stato-adesso", "titoloAdesso", "RAM libera", "Per un altro modello"} {
		if !strings.Contains(UI, atteso) {
			t.Errorf("stato memoria immediato senza %q", atteso)
		}
	}
	i := strings.Index(UI, "function vistaMemoria()")
	fine := strings.Index(UI[i+1:], "function vistaModelli()")
	if i < 0 || fine < 0 {
		t.Fatal("non riesco a delimitare la vista memoria")
	}
	corpo := UI[i : i+1+fine]
	if strings.Contains(corpo, "T().readingNote") || strings.Contains(corpo, "sug.map") {
		t.Fatal("la memoria mostra ancora la nota burocratica o una proposta casuale")
	}
}
