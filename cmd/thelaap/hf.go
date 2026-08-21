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
// La ricerca non decide al posto dell'utente: mostra tutti i formati trovati e
// segnala soltanto quali sono direttamente adatti ai runtime MLX configurati.

type Found struct {
	ID          string  `json:"id"`
	GB          float64 `json:"gb"`
	Downloads   int     `json:"downloads"`
	Likes       int     `json:"likes"`
	Tendenza    float64 `json:"tendenza"`
	Aggiornato  string  `json:"aggiornato"`
	Creato      string  `json:"creato"`
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
	case strings.Contains(l, "mlx") && (strings.Contains(l, "8bit") || strings.Contains(l, "8-bit") || strings.Contains(l, "oq8")):
		return "MLX 8-bit", true, ""
	case strings.Contains(l, "mlx") && (strings.Contains(l, "6bit") || strings.Contains(l, "6-bit") || strings.Contains(l, "oq6")):
		return "MLX 6-bit", true, "accettabile solo per MoE molto grossi"
	case strings.Contains(l, "mlx") && (strings.Contains(l, "4bit") || strings.Contains(l, "4-bit") || strings.Contains(l, "5bit") || strings.Contains(l, "3bit") ||
		strings.Contains(l, "2bit") || strings.Contains(l, "nvfp4") || strings.Contains(l, "int4")):
		return "MLX sotto 6-bit", false, "sotto la soglia di qualità consigliata"
	case strings.Contains(l, "mlx") && (strings.Contains(l, "bf16") || strings.Contains(l, "fp16")):
		return "MLX BF16/FP16", true, "qualità alta, ma richiede molta memoria"
	case strings.Contains(l, "mlx"):
		return "MLX", true, ""
	case strings.Contains(l, "onnx"):
		return "ONNX", false, "richiede un runtime ONNX"
	case strings.Contains(l, "coreml"):
		return "Core ML", false, "richiede un'app compatibile con Core ML"
	}
	return "?", false, "formato non riconosciuto"
}

func cached(id string) bool {
	h, _ := os.UserHomeDir()
	d := filepath.Join(h, ".cache/huggingface/hub", "models--"+strings.ReplaceAll(id, "/", "--"))
	_, err := os.Stat(d)
	return err == nil
}

func hfSearchTerms(q string) string {
	return strings.TrimSpace(q)
}

// hfSortField accetta soltanto i quattro ordinamenti mostrati dalla pagina.
// Il valore finisce nella query verso HuggingFace: lasciarlo libero renderebbe
// il pulsante Cerca dipendente da parametri arbitrari e da errori poco chiari.
func hfSortField(v string) string {
	switch v {
	case "downloads", "likes", "lastModified", "trendingScore":
		return v
	default:
		return "trendingScore"
	}
}

func apiHFSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		errJSON(w, "scrivi cosa cerchi")
		return
	}
	// Nessun suffisso implicito: se l'utente cerca "qwen" deve poter vedere il
	// risultato reale di HuggingFace, non soltanto i repository MLX scelti dal
	// pannello. Il formato si filtra esplicitamente nella pagina.
	qHF := hfSearchTerms(q)
	ordine := hfSortField(r.URL.Query().Get("sort"))
	u := "https://huggingface.co/api/models?search=" + url.QueryEscape(qHF) +
		"&limit=60&sort=" + url.QueryEscape(ordine) + "&direction=-1" +
		"&expand%5B%5D=downloads&expand%5B%5D=likes" +
		"&expand%5B%5D=trendingScore&expand%5B%5D=lastModified&expand%5B%5D=createdAt"
	b := httpGet(u, 25*time.Second)
	if b == nil {
		errJSON(w, "HuggingFace non raggiungibile")
		return
	}
	var grezzi []struct {
		ID           string  `json:"id"`
		Downloads    int     `json:"downloads"`
		Likes        int     `json:"likes"`
		Trending     float64 `json:"trendingScore"`
		LastModified string  `json:"lastModified"`
		CreatedAt    string  `json:"createdAt"`
	}
	if err := json.Unmarshal(b, &grezzi); err != nil {
		errJSON(w, "risposta di HuggingFace non leggibile")
		return
	}

	// le dimensioni richiedono una chiamata per repo: le prendo in parallelo, solo per i candidati buoni
	var mu sync.Mutex
	var wg sync.WaitGroup
	out := []Found{} // mai nil: nil diventa "null" nel JSON
	sem := make(chan struct{}, 8)
	for _, g := range grezzi {
		fmt_, _, nota := formato(g.ID)
		wg.Add(1)
		go func(id string, dl, likes int, trend float64, mod, creato, f, nota string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			t := Found{ID: id, Downloads: dl, Likes: likes, Tendenza: trend, Formato: f, Nota: nota,
				GiaPresente: cached(id), Consigliato: f == "MLX 8-bit"}
			t.Aggiornato = mod
			t.Creato = creato
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
					CreatedAt    string `json:"createdAt"`
					LastModified string `json:"lastModified"`
					Siblings     []struct {
						Rfilename string  `json:"rfilename"`
						Size      float64 `json:"size"`
					} `json:"siblings"`
				}
				if json.Unmarshal(bb, &d) == nil {
					if d.CreatedAt != "" {
						t.Creato = d.CreatedAt
					}
					if d.LastModified != "" {
						t.Aggiornato = d.LastModified
					}
					var tot float64
					haSafe, haGGUF, haONNX := false, false, false
					for _, s := range d.Siblings {
						nomeFile := strings.ToLower(s.Rfilename)
						if strings.HasSuffix(nomeFile, ".safetensors") {
							tot += s.Size
							haSafe = true
						}
						if strings.HasSuffix(nomeFile, ".gguf") {
							haGGUF = true
							tot += s.Size
						}
						if strings.HasSuffix(nomeFile, ".onnx") {
							haONNX = true
						}
					}
					t.GB = tot / 1e9
					if t.Formato == "?" {
						switch {
						case haGGUF:
							t.Formato, t.Nota = "GGUF", "per Ollama o un runtime GGUF"
						case haONNX:
							t.Formato, t.Nota = "ONNX", "richiede un runtime ONNX"
						case haSafe:
							t.Formato, t.Nota = "Safetensors", "il runtime dipende dall'architettura del modello"
						default:
							t.Formato, t.Nota = "Altro/non indicato", "controlla i file del repository prima di usarlo"
						}
					}
				}
			}
			mu.Lock()
			out = append(out, t)
			mu.Unlock()
		}(g.ID, g.Downloads, g.Likes, g.Trending, g.LastModified, g.CreatedAt, fmt_, nota)
	}
	wg.Wait()

	// Le goroutine finiscono in ordine casuale. Rimettiamo lo stesso ordine
	// richiesto a HuggingFace; la pagina potrà poi applicare filtri combinati
	// senza far saltare le righe a ogni ridisegno.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			a, b := out[i], out[j]
			var prima bool
			switch ordine {
			case "likes":
				prima = b.Likes > a.Likes
			case "lastModified":
				prima = b.Aggiornato > a.Aggiornato
			case "downloads":
				prima = b.Downloads > a.Downloads
			default:
				prima = b.Tendenza > a.Tendenza
			}
			if prima {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	writeJSON(w, out)
}

// ── download ────────────────────────────────────────────────
type Download struct {
	Repo   string    `json:"repo"`
	Stato  string    `json:"stato"` // in corso | finito | errore
	GB     float64   `json:"gb"`
	Errore string    `json:"errore"`
	Da     time.Time `json:"-"`
}

var (
	scarichi   = map[string]*Download{}
	scarichiMu sync.Mutex
)

// downloadsInProgress: i repo che stiamo scaricando adesso.
//
// Sta qui, accanto alla mappa, perché la fonte deve essere una: states.go la usa
// per dire "in-arrivo" e /api/hf/stato per disegnare l'avanzamento. Se le due
// cose leggessero due posti diversi, un modello potrebbe risultare spento
// mentre la barra del download cresce.
func downloadsInProgress() []string {
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

func apiHFDownload(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, map[string]any{"ok": true, "messaggio": "già in corso"})
		return
	}
	scarichi[req.Repo] = &Download{Repo: req.Repo, Stato: "in corso", Da: time.Now()}
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

	writeJSON(w, map[string]any{"ok": true,
		"messaggio": fmt.Sprintf("scarico %s — resta pure sulla pagina, ti aggiorno qui", req.Repo)})
}

func apiHFStatus(w http.ResponseWriter, r *http.Request) {
	scarichiMu.Lock()
	defer scarichiMu.Unlock()
	var out []Download
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
	writeJSON(w, out)
}
