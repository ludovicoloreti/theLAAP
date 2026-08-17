package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cercare un modello su HuggingFace e scaricarlo senza uscire dal pannello.
// Filtriamo su MLX 8-bit: è il formato che i runtime di questa macchina usano,
// e 8-bit è la regola dello stack (mai 4-bit, salvo MoE molto grossi).

type Trovato struct {
	ID          string  `json:"id"`
	GB          float64 `json:"gb"`
	Downloads   int     `json:"downloads"`
	Aggiornato  string  `json:"aggiornato"`
	Formato     string  `json:"formato"` // MLX 8-bit, MLX 6-bit, GGUF…
	GiaPresente bool    `json:"giaPresente"`
	Consigliato bool    `json:"consigliato"`
	Nota        string  `json:"nota"`
}

// formato deduce quantizzazione e runtime dal nome del repo.
func formato(id string) (string, bool, string) {
	l := strings.ToLower(id)
	switch {
	case strings.Contains(l, "gguf"):
		return "GGUF", false, "per Ollama, non per i runtime MLX di questa macchina"
	case strings.Contains(l, "8bit"), strings.Contains(l, "8-bit"), strings.Contains(l, "oq8"):
		return "MLX 8-bit", true, ""
	case strings.Contains(l, "6bit"), strings.Contains(l, "oq6"):
		return "MLX 6-bit", true, "accettabile solo per MoE molto grossi"
	case strings.Contains(l, "4bit"), strings.Contains(l, "5bit"), strings.Contains(l, "3bit"),
		strings.Contains(l, "2bit"), strings.Contains(l, "nvfp4"), strings.Contains(l, "int4"):
		return "sotto 8-bit", false, "sotto la soglia di qualità dello stack"
	case strings.Contains(l, "mlx"):
		return "MLX", true, ""
	}
	return "?", false, "formato non riconosciuto"
}

func inCache(id string) bool {
	h, _ := os.UserHomeDir()
	d := filepath.Join(h, ".cache/huggingface/hub", "models--"+strings.ReplaceAll(id, "/", "--"))
	_, err := os.Stat(d)
	return err == nil
}

func apiHFCerca(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		errJSON(w, "scrivi cosa cerchi")
		return
	}
	u := "https://huggingface.co/api/models?search=" + url.QueryEscape(q) +
		"&limit=60&sort=downloads&direction=-1"
	b := httpGet(u, 25*time.Second)
	if b == nil {
		errJSON(w, "HuggingFace non raggiungibile")
		return
	}
	var grezzi []struct {
		ID           string `json:"id"`
		Downloads    int    `json:"downloads"`
		LastModified string `json:"lastModified"`
	}
	if err := json.Unmarshal(b, &grezzi); err != nil {
		errJSON(w, "risposta di HuggingFace non leggibile")
		return
	}

	// le dimensioni richiedono una chiamata per repo: le prendo in parallelo, solo per i candidati buoni
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := []Trovato{} // mai nil: nil diventa "null" nel JSON
	sem := make(chan struct{}, 8)
	for _, g := range grezzi {
		fmt_, ok, nota := formato(g.ID)
		if !ok && fmt_ != "MLX 6-bit" { // scarto GGUF e sotto-8-bit
			continue
		}
		wg.Add(1)
		go func(id string, dl int, mod, f, nota string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t := Trovato{ID: id, Downloads: dl, Formato: f, Nota: nota,
				GiaPresente: inCache(id), Consigliato: f == "MLX 8-bit"}
			if len(mod) >= 10 {
				t.Aggiornato = mod[:10]
			}
			// Il dettaglio serve per la dimensione, ma soprattutto dice se il
			// repo è davvero raggiungibile: quelli chiusi o rimossi rispondono
			// 401/404 e httpGet torna nil. Proporne uno significa far partire
			// un download destinato a fallire — meglio non mostrarlo affatto.
			bb := httpGet("https://huggingface.co/api/models/"+id+"?blobs=true", 20*time.Second)
			if bb == nil {
				return
			}
			{
				var d struct {
					Siblings []struct {
						Rfilename string  `json:"rfilename"`
						Size      float64 `json:"size"`
					} `json:"siblings"`
				}
				if json.Unmarshal(bb, &d) == nil {
					var tot float64
					for _, s := range d.Siblings {
						if strings.HasSuffix(s.Rfilename, ".safetensors") {
							tot += s.Size
						}
					}
					t.GB = tot / 1e9
				}
			}
			mu.Lock()
			out = append(out, t)
			mu.Unlock()
		}(g.ID, g.Downloads, g.LastModified, fmt_, nota)
	}
	wg.Wait()

	// prima i consigliati, poi per download
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			a, b := out[i], out[j]
			if (b.Consigliato && !a.Consigliato) ||
				(b.Consigliato == a.Consigliato && b.Downloads > a.Downloads) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > 25 {
		out = out[:25]
	}
	scriviJSON(w, out)
}

// ── download ────────────────────────────────────────────────
type Scarico struct {
	Repo   string    `json:"repo"`
	Stato  string    `json:"stato"` // in corso | finito | errore
	GB     float64   `json:"gb"`
	Errore string    `json:"errore"`
	Da     time.Time `json:"-"`
}

var (
	scarichi   = map[string]*Scarico{}
	scarichiMu sync.Mutex
)

// scarichiInCorso: i repo che stiamo scaricando adesso.
//
// Sta qui, accanto alla mappa, perché la fonte deve essere una: stati.go la usa
// per dire "in-arrivo" e /api/hf/stato per disegnare l'avanzamento. Se le due
// cose leggessero due posti diversi, un modello potrebbe risultare spento
// mentre la barra del download cresce.
func scarichiInCorso() []string {
	scarichiMu.Lock()
	defer scarichiMu.Unlock()
	out := []string{}
	for _, s := range scarichi {
		if s.Stato == "in corso" {
			out = append(out, s.Repo)
		}
	}
	return out
}

func apiHFScarica(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Repo string `json:"repo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Repo == "" {
		errJSON(w, "manca il modello da scaricare")
		return
	}
	// il nome del repo finisce in un comando: accetto solo caratteri legittimi
	for _, c := range req.Repo {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '/' || c == '-' || c == '_' || c == '.') {
			errJSON(w, "nome del modello non valido")
			return
		}
	}
	scarichiMu.Lock()
	if s, ok := scarichi[req.Repo]; ok && s.Stato == "in corso" {
		scarichiMu.Unlock()
		scriviJSON(w, map[string]any{"ok": true, "messaggio": "già in corso"})
		return
	}
	scarichi[req.Repo] = &Scarico{Repo: req.Repo, Stato: "in corso", Da: time.Now()}
	scarichiMu.Unlock()

	go func(repo string) {
		// HF_HUB_DISABLE_XET=1: xet si è piantato a metà durante i download del 25 luglio
		c := exec.Command("python3", "-c",
			"import sys;from huggingface_hub import snapshot_download;snapshot_download(sys.argv[1],max_workers=8)",
			repo)
		c.Env = append(os.Environ(), "HF_HUB_DISABLE_XET=1")
		outErr, err := c.CombinedOutput()
		scarichiMu.Lock()
		defer scarichiMu.Unlock()
		s := scarichi[repo]
		if err != nil {
			s.Stato, s.Errore = "errore", trunc(string(outErr), 300)
			return
		}
		s.Stato = "finito"
		h, _ := os.UserHomeDir()
		d := filepath.Join(h, ".cache/huggingface/hub", "models--"+strings.ReplaceAll(repo, "/", "--"))
		if o := sh("du -sk " + shQuote(d) + " 2>/dev/null | cut -f1"); o != "" {
			s.GB = parseFloat(o) / 1e6
		}
	}(req.Repo)

	scriviJSON(w, map[string]any{"ok": true,
		"messaggio": fmt.Sprintf("scarico %s — resta pure sulla pagina, ti aggiorno qui", req.Repo)})
}

func apiHFStato(w http.ResponseWriter, r *http.Request) {
	scarichiMu.Lock()
	defer scarichiMu.Unlock()
	var out []Scarico
	for _, s := range scarichi {
		c := *s
		if c.Stato == "in corso" {
			// dimensione parziale, così si vede che sta crescendo
			h, _ := os.UserHomeDir()
			d := filepath.Join(h, ".cache/huggingface/hub",
				"models--"+strings.ReplaceAll(c.Repo, "/", "--"))
			if o := sh("du -sk " + shQuote(d) + " 2>/dev/null | cut -f1"); o != "" {
				c.GB = parseFloat(o) / 1e6
			}
		}
		out = append(out, c)
	}
	scriviJSON(w, out)
}
