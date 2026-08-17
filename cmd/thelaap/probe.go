package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Outcome di un test reale su un modello: risponde? quanto va? ragiona?
type Outcome struct {
	OK        bool    `json:"ok"`
	TokS      float64 `json:"tokS"`
	Reasoning bool    `json:"reasoning"`
	Risposta  string  `json:"risposta"`
	LoadSec   float64 `json:"loadSec"`
	Errore    string  `json:"errore"`
}

// destinazione: dove mandare la richiesta. Un runtime locale è una porta su
// 127.0.0.1; un provider remoto ha un indirizzo e una credenziale sue. La
// credenziale serve per la chiamata e non finisce mai in una risposta.
type destinazione struct {
	baseURL string
	apiKey  string
}

func localDestination(porta int) destinazione {
	return destinazione{baseURL: "http://127.0.0.1:" + itoa(porta) + "/v1"}
}

func chiamata(porta int, modello, prompt string, maxTok int, timeout time.Duration) (map[string]any, float64, error) {
	return callTo(localDestination(porta), modello, prompt, maxTok, timeout)
}

func callTo(d destinazione, modello, prompt string, maxTok int, timeout time.Duration) (map[string]any, float64, error) {
	corpo, _ := json.Marshal(map[string]any{
		"model":      modello,
		"messages":   []any{map[string]any{"role": "user", "content": prompt}},
		"max_tokens": maxTok,
	})
	req, err := http.NewRequest(http.MethodPost, d.baseURL+"/chat/completions", bytes.NewReader(corpo))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if d.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+d.apiKey)
	}
	cl := &http.Client{Timeout: timeout}
	t0 := time.Now()
	resp, err := cl.Do(req)
	if err != nil {
		// L'errore di Go contiene l'URL completo, che può portare credenziali.
		return nil, 0, fmt.Errorf("il server non ha risposto")
	}
	defer resp.Body.Close()
	var v map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, 0, err
	}
	return v, time.Since(t0).Seconds(), nil
}

// probeModel: warmup (carica) + misura vera. Rileva anche se produce reasoning_content,
// così il flag `reasoning` nelle config non va indovinato a mano.
func probeModel(porta int, modello string) Outcome {
	return probeModelAt(localDestination(porta), modello)
}

func probeModelAt(d destinazione, modello string) Outcome {
	// 1) warmup: serve a caricare il modello, il tempo qui è quasi tutto load
	_, loadSec, err := callTo(d, modello, "ok", 5, 20*time.Minute)
	if err != nil {
		return Outcome{Errore: err.Error()}
	}
	// 2) misura a caldo — 800 token di spazio: i modelli che ragionano ne consumano molti
	v, sec, err := callTo(d, modello,
		"Spiega in dettaglio il quicksort in Python, passo per passo.", 300, 20*time.Minute)
	if err != nil {
		return Outcome{Errore: err.Error(), LoadSec: loadSec}
	}
	if e, ok := v["error"]; ok {
		return Outcome{Errore: trunc(sprint(e), 200), LoadSec: loadSec}
	}
	scelte, _ := v["choices"].([]any)
	if len(scelte) == 0 {
		return Outcome{Errore: "nessuna risposta", LoadSec: loadSec}
	}
	c0, _ := scelte[0].(map[string]any)
	msg, _ := c0["message"].(map[string]any)
	testo, _ := msg["content"].(string)
	_, ragiona := msg["reasoning_content"]
	if rc, ok := msg["reasoning_content"].(string); ok && rc == "" {
		ragiona = false
	}
	var tokS float64
	if u, ok := v["usage"].(map[string]any); ok {
		if n, ok := u["completion_tokens"].(float64); ok && sec > 0 {
			tokS = n / sec
		}
	}
	return Outcome{OK: true, TokS: tokS, Reasoning: ragiona,
		Risposta: trunc(testo, 160), LoadSec: loadSec}
}

func apiProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Porta   int    `json:"porta"`
		Model   string `json:"modello"`
		Runtime string `json:"runtime"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, err.Error())
		return
	}
	// Un modello remoto non ha una porta locale: si passa dall'indirizzo del
	// suo provider. Prima si prova quella strada, poi si ripiega sulla porta.
	var e Outcome
	if d, ok := destinationFor(req.Runtime); ok && d.baseURL != "" {
		e = probeModelAt(d, req.Model)
	} else {
		e = probeModel(req.Porta, req.Model)
	}
	// La misura è la fonte più affidabile che abbiamo: la conserviamo, così
	// l'interfaccia non deve indovinare le velocità né tenerle scritte nel codice.
	if e.OK && req.Runtime != "" {
		updateProfile(req.Runtime, req.Model, func(p *Profile) {
			p.TokS = e.TokS
			p.Reasoning = e.Reasoning
			p.Provato = time.Now()
		})
	}
	writeJSON(w, e)
}
