package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ludovicoloreti/theLAAP/internal/budget"
)

// MemState — fotografia della memoria unificata e di chi la sta occupando.
type MemState struct {
	TotaleGB    float64 `json:"totaleGB"`
	LiberaGB    float64 `json:"liberaGB"`
	UsataGB     float64 `json:"usataGB"`
	WiredGB     float64 `json:"wiredGB"`
	CompressaGB float64 `json:"compressaGB"`
	SwapUsatoGB float64 `json:"swapUsatoGB"`
	WiredCapGB  float64 `json:"wiredCapGB"` // iogpu.wired_limit_mb
	// Tetto per singolo modello, se il runtime lo dichiara in /health.
	// Non tutti lo fanno: zero significa "non lo so", non "nessun limite".
	CeilingGB float64      `json:"ceilingGB"`
	Caricati  []ModelInRAM `json:"caricati"`
	// Quanto occupa davvero ogni programma acceso, misurato sul processo.
	// `Caricati` dice quali modelli ci sono e quanto pesano i loro file;
	// `Processi` dice quanta memoria tengono davvero, e le due cose
	// differiscono parecchio: mtplx dichiara 29,3 GB e ne occupa 84,8.
	// La barra deve disegnare questi, non quelli.
	Processi   []budget.RuntimeUsage `json:"processi"`
	Avvisi     []string              `json:"avvisi"`
	Aggiornato string                `json:"aggiornato"`
	// Istante della lettura, e quanti secondi ha al momento della risposta.
	// La sola stringa "15:04:05" non permette all'interfaccia di capire se il
	// dato è di 4 secondi o di 40 minuti fa: con un monitor morto mostrerebbe
	// numeri vecchi senza dirlo.
	Istante    time.Time `json:"-"`
	EtaSecondi int       `json:"etaSecondi"`
}

type ModelInRAM struct {
	Nome    string  `json:"nome"`
	Runtime string  `json:"runtime"`
	GB      float64 `json:"gb"`
	Stato   string  `json:"stato"`
}

// loadedModelsStatus legge l'endpoint strutturato dei runtime che lo
// espongono (oMLX). /health dice soltanto "loaded_count: 1"; qui invece c'e'
// l'id preciso, quindi il pannello puo' colorare il modello giusto come attivo
// anche dopo un proprio riavvio.
func loadedModelsStatus(b []byte, runtime string) ([]ModelInRAM, float64) {
	var s struct {
		FinalCeiling float64 `json:"final_ceiling"`
		Models       []struct {
			ID            string  `json:"id"`
			Loaded        bool    `json:"loaded"`
			ActualSize    float64 `json:"actual_size"`
			EstimatedSize float64 `json:"estimated_size"`
			EngineType    string  `json:"engine_type"`
		} `json:"models"`
	}
	if json.Unmarshal(b, &s) != nil {
		return nil, 0
	}
	out := []ModelInRAM{}
	for _, x := range s.Models {
		if !x.Loaded || x.ID == "" || x.EngineType == "markitdown" {
			continue
		}
		peso := x.ActualSize
		if peso <= 0 {
			peso = x.EstimatedSize
		}
		out = append(out, ModelInRAM{Nome: x.ID, Runtime: runtime, GB: peso / 1e9, Stato: "caricato"})
	}
	return out, s.FinalCeiling / 1e9
}

// Ogni comando esterno ha un tetto di tempo. Senza, basta un programma che si
// impianta (è successo con `lms`, che è Node e a volte non torna) per bloccare
// tutto quello che sta dietro.
func cmd(nome string, args ...string) string { return cmdT(12*time.Second, nome, args...) }

func cmdT(limite time.Duration, nome string, args ...string) string {
	ctx, annulla := context.WithTimeout(context.Background(), limite)
	defer annulla()
	out, err := exec.CommandContext(ctx, nome, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func sh(linea string) string { return cmd("/bin/sh", "-c", linea) }

// shQuote: mette una stringa fra apici singoli per la shell, in sicurezza.
// Serve perché model_path arriva da un servizio esterno e finisce in `du -sk`.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Le dimensioni su disco non cambiano, ma `du` su una cartella da 30 GB costa
// secondi: senza cache la pagina si aggiorna ogni 5s e resta sempre in attesa.
var (
	cacheGB   = map[string]float64{}
	cacheGBMu sync.RWMutex
)

func sizeGB(path string) float64 {
	cacheGBMu.RLock()
	v, ok := cacheGB[path]
	cacheGBMu.RUnlock()
	if ok {
		return v
	}
	gb := folderSize(path)
	cacheGBMu.Lock()
	cacheGB[path] = gb
	cacheGBMu.Unlock()
	return gb
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// readsMemory mette insieme vm_stat, sysctl e lo stato dei runtime.
func readsMemory() MemState {
	// liste sempre inizializzate: se restano nil il JSON esce con "null" e la
	// pagina va in errore appena prova a scorrerle
	adesso := time.Now()
	m := MemState{Aggiornato: adesso.Format("15:04:05"), Istante: adesso,
		Caricati: []ModelInRAM{}, Avvisi: []string{}}

	// letture di sistema: l'implementazione cambia per sistema operativo
	m.TotaleGB, m.LiberaGB, m.WiredGB, m.CompressaGB, m.SwapUsatoGB = systemMemory()
	m.UsataGB = m.TotaleGB - m.LiberaGB
	m.WiredCapGB = graphicsCeilingGB()

	// Quali modelli sono in memoria: ogni programma lo dice a modo suo, e il
	// comando è dichiarato nella configurazione. Il formato atteso è tabellare,
	// con da qualche parte un numero seguito da GB.
	// In parallelo, non uno dopo l'altro. Ogni interrogazione ha un tetto di
	// 12 s e ce ne sono una per runtime: in fila, con quattro programmi lenti,
	// un giro può durare un minuto contro i 4 s di attesa fra un giro e
	// l'altro — il monitor resterebbe cronicamente indietro e la pagina
	// mostrerebbe numeri vecchi credendoli freschi.
	type esitoRT struct {
		rc  RuntimeCfg
		out string
		err error
	}
	var wg sync.WaitGroup
	motori := knownRuntimes()
	esiti := make([]esitoRT, len(motori))
	for i, rc := range motori {
		if rc.Caricati == "" {
			continue
		}
		wg.Add(1)
		go func(i int, rc RuntimeCfg) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					esiti[i] = esitoRT{rc: rc, err: fmt.Errorf("errore interno: %v", r)}
				}
			}()
			o, err := shErr(12*time.Second, rc.Caricati+" 2>/dev/null")
			esiti[i] = esitoRT{rc: rc, out: o, err: err}
		}(i, rc)
	}
	wg.Wait()

	for _, e := range esiti {
		rc, out := e.rc, e.out
		if rc.Chiave == "" {
			continue
		}
		// #5: un comando fallito non è la stessa cosa di uno che non stampa
		// niente. Prima erano indistinguibili e non lo sapeva nessuno.
		if e.err != nil {
			m.Avvisi = append(m.Avvisi, fmt.Sprintf(
				"non riesco a sapere cosa ha in memoria %s: %s", rc.Nome, withoutAnsi(e.err.Error())))
			continue
		}
		if out == "" {
			continue
		}
		for _, riga := range strings.Split(out, "\n") {
			campi := strings.Fields(riga)
			if len(campi) < 3 || strings.EqualFold(campi[0], "IDENTIFIER") ||
				strings.EqualFold(campi[0], "NAME") {
				continue
			}
			for i := 1; i < len(campi); i++ {
				gb, ok := measureGB(campi, i)
				if !ok {
					continue
				}
				stato := "caricato"
				if i >= 2 {
					if s := strings.ToLower(campi[i-1]); s == "idle" || s == "loaded" || s == "ready" {
						stato = s
					}
				}
				m.Caricati = append(m.Caricati, ModelInRAM{
					Nome: campi[0], Runtime: rc.Nome, GB: gb, Stato: stato})
				break
			}
		}
	}

	// I programmi che tengono il modello sempre residente lo dicono nel loro
	// stato: se non c'è un comando apposta, si guarda /health.
	corpi := make([][]byte, len(motori))
	statiModelli := make([][]byte, len(motori))
	var wgH sync.WaitGroup
	for i, rc := range motori {
		if rc.Caricati != "" {
			continue
		}
		wgH.Add(1)
		go func(i, porta int) {
			defer wgH.Done()
			corpi[i] = httpGet(fmt.Sprintf("http://127.0.0.1:%d/health", porta), 3*time.Second)
			statiModelli[i] = httpGet(fmt.Sprintf("http://127.0.0.1:%d/v1/models/status", porta), 3*time.Second)
		}(i, rc.Porta)
	}
	wgH.Wait()

	for i, rc := range motori {
		if rc.Caricati != "" {
			continue
		}
		if caricati, tetto := loadedModelsStatus(statiModelli[i], rc.Nome); len(caricati) > 0 {
			m.Caricati = append(m.Caricati, caricati...)
			if tetto > 0 {
				m.CeilingGB = tetto
			}
			continue
		}
		b := corpi[i]
		if b == nil {
			continue
		}
		var h struct {
			Model      string `json:"model"`
			ModelPath  string `json:"model_path"`
			EnginePool struct {
				LoadedCount   int     `json:"loaded_count"`
				CurrentMemory float64 `json:"current_model_memory"`
				FinalCeiling  float64 `json:"final_ceiling"`
			} `json:"engine_pool"`
		}
		if json.Unmarshal(b, &h) != nil {
			continue
		}
		if h.EnginePool.FinalCeiling > 0 {
			m.CeilingGB = h.EnginePool.FinalCeiling / 1e9
		}
		switch {
		case h.Model != "":
			gb := 0.0
			if h.ModelPath != "" {
				gb = sizeGB(h.ModelPath)
			}
			m.Caricati = append(m.Caricati, ModelInRAM{
				Nome: h.Model, Runtime: rc.Nome, GB: gb, Stato: "residente"})
		case h.EnginePool.LoadedCount > 0 && h.EnginePool.CurrentMemory > 0:
			nome := recentlyActive(rc.Chiave)
			if nome == "" {
				nome = "modello attivo"
			}
			m.Caricati = append(m.Caricati, ModelInRAM{
				Nome: nome, Runtime: rc.Nome,
				GB: h.EnginePool.CurrentMemory / 1e9, Stato: "caricato"})
		}
	}

	// Quanto tengono davvero i processi. Va dopo la raccolta dei modelli
	// perché associa a ogni programma i modelli che ha dentro.
	m.Processi = runtimeFootprints(m.Caricati)

	// avvisi utili
	//
	// La soglia era codificata a 124518 MB, che è il valore raccomandato da
	// oMLX ed è esattamente quello con cui questa macchina è andata in kernel
	// panic il 27/07/2026. Avvisando solo *sopra*, a 124518 esatti taceva.
	// Ora la soglia si deriva: allarme quando il tetto grafico lascia al
	// sistema meno della riserva configurata.
	if m.TotaleGB > 0 && m.WiredCapGB > 0 &&
		m.TotaleGB-m.WiredCapGB < minimoSottoIlTettoGBDefault {
		m.Avvisi = append(m.Avvisi, fmt.Sprintf(
			"il tetto grafico (%.0f GB) lascia al sistema solo %.0f GB: così un solo "+
				"programma può affamare il Mac fino a bloccarlo",
			m.WiredCapGB, m.TotaleGB-m.WiredCapGB))
	}
	if m.SwapUsatoGB > 20 && m.LiberaGB < 20 {
		m.Avvisi = append(m.Avvisi, "swap alto e poca RAM libera: scarica un modello grosso")
	}
	// Gli avvisi si riferiscono a quello che è caricato ADESSO, non a modelli
	// per nome: se domani i modelli sono altri, questi messaggi restano validi.
	var somma float64
	var piuGrosso ModelInRAM
	for _, c := range m.Caricati {
		somma += c.GB
		if c.GB > piuGrosso.GB {
			piuGrosso = c
		}
	}
	if m.CeilingGB > 0 && piuGrosso.GB > m.CeilingGB*0.9 {
		m.Avvisi = append(m.Avvisi, fmt.Sprintf(
			"%s occupa %.0f GB su un tetto di %.0f: un modello più grande non entrerebbe",
			piuGrosso.Nome, piuGrosso.GB, m.CeilingGB))
	}
	if m.TotaleGB > 0 && somma > m.TotaleGB*0.75 {
		m.Avvisi = append(m.Avvisi, fmt.Sprintf(
			"i modelli occupano %.0f GB dei %.0f disponibili: usa «Ferma tutto» per liberare",
			somma, m.TotaleGB))
	}
	return m
}

// Raccogliere lo stato costa ~4 secondi, quasi tutti spesi da `lms ps` (è un
// programma Node che si riavvia a ogni chiamata). Con la pagina che si aggiorna
// ogni 5 secondi, farlo dentro la richiesta significa tenerla sempre in attesa.
// Quindi: un lavoratore in sottofondo aggiorna la fotografia, e la richiesta
// restituisce subito l'ultima disponibile.
var (
	ultimaMem   MemState
	ultimaMemMu sync.RWMutex
)

// Tutto in sottofondo, compresa la prima raccolta: il server deve mettersi in
// ascolto subito. Se la prima fotografia costa secondi — o si impianta — la
// porta non si apre e l'app sembra non partita.
func startMemoryMonitor() {
	go func() {
		for {
			readCycle()
			// Un rinfresco su richiesta (dopo aver scaricato un modello) evita
			// che l'interfaccia mostri ancora quello che è appena stato tolto.
			select {
			case <-refreshedAt:
			case <-time.After(4 * time.Second):
			}
		}
	}()
}

// readCycle è una funzione a sé per una ragione precisa: il recover() deve
// far cadere il singolo giro, non il ciclo.
//
// readsMemory fa parsing di output di programmi esterni — indicizza slice,
// converte numeri — cioè proprio il codice che può andare fuori indice quando
// un programma cambia formato. E in Go un panic dentro una goroutine che non
// sia un handler HTTP **termina l'intero processo**: il pannello sparirebbe
// senza lasciare traccia, e da fuori sembrerebbe semplicemente "non parte".
func readCycle() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("monitor memoria: giro saltato per un errore interno: %v", r)
			ultimaMemMu.Lock()
			ultimaMem.Avvisi = append(ultimaMem.Avvisi,
				"la lettura della memoria è fallita: i numeri qui sotto potrebbero essere vecchi")
			ultimaMemMu.Unlock()
		}
	}()
	m := readsMemory()
	ultimaMemMu.Lock()
	ultimaMem = m
	ultimaMemMu.Unlock()
}

// currentMemory: l'ultima fotografia disponibile.
func currentMemory() MemState {
	ultimaMemMu.RLock()
	defer ultimaMemMu.RUnlock()
	return ultimaMem
}

func apiMemory(w http.ResponseWriter, r *http.Request) {
	m := currentMemory()
	// Quanti secondi ha questa fotografia. Senza, l'interfaccia non può
	// distinguere un dato fresco da uno fermo da mezz'ora perché il monitor
	// è morto: mostrerebbe numeri stantii con l'aria di essere aggiornati.
	if !m.Istante.IsZero() {
		m.EtaSecondi = int(time.Since(m.Istante).Seconds())
	}
	writeJSON(w, m)
}

// measureGB riconosce "29.3 GB", "29.3GB" e "30012 MB" dentro una riga tabellare.
func measureGB(campi []string, i int) (float64, bool) {
	c := campi[i]
	unita := ""
	if i+1 < len(campi) {
		unita = strings.ToUpper(campi[i+1])
	}
	num := strings.TrimRight(strings.ToUpper(c), "GBMIB")
	v := parseFloat(num)
	if v <= 0 {
		return 0, false
	}
	su := strings.ToUpper(c)
	switch {
	case strings.HasSuffix(su, "GB"), strings.HasSuffix(su, "GIB"), unita == "GB", unita == "GIB":
		return v, true
	case strings.HasSuffix(su, "MB"), strings.HasSuffix(su, "MIB"), unita == "MB", unita == "MIB":
		return v / 1024, true
	}
	return 0, false
}
