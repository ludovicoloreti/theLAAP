package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DocumentoConfig e' un file che il pannello puo' mostrare nell'editor.
// L'identificativo viene scelto dal server: il browser non puo' passare un
// percorso arbitrario e usare il pannello come editor di tutto il disco.
type DocumentoConfig struct {
	ID          string `json:"id"`
	Nome        string `json:"nome"`
	File        string `json:"file"`
	Formato     string `json:"formato"`
	Descrizione string `json:"descrizione,omitempty"`
	Esiste      bool   `json:"esiste"`
}

type documentoLetto struct {
	DocumentoConfig
	FormatoNativo string `json:"formatoNativo"`
	Contenuto     string `json:"contenuto"`
	Revisione     string `json:"revisione"`
}

// documentiConfigurazione e' anche la lista bianca di file modificabili.
// Oltre al file di theLAAP include tutti i client dichiarati nella
// configurazione, quindi un client YAML aggiunto domani compare senza cambiare
// il codice.
func documentiConfigurazione() []DocumentoConfig {
	docs := []DocumentoConfig{{
		ID:          "thelaap",
		Nome:        "theLAAP",
		File:        CFGFILE,
		Formato:     "json",
		Descrizione: "runtime, percorsi, regimi e limiti di memoria",
	}}
	for i, c := range cfg().Clienti {
		docs = append(docs, DocumentoConfig{
			ID:          "client-" + strconv.Itoa(i),
			Nome:        c.Nome,
			File:        espandiHome(c.File),
			Formato:     formatoDocumento(c.File, c.Formato),
			Descrizione: "configurazione del client " + c.Nome,
		})
	}
	for i := range docs {
		_, err := os.Stat(docs[i].File)
		docs[i].Esiste = err == nil
	}
	return docs
}

func formatoDocumento(path, dichiarato string) string {
	f := strings.ToLower(strings.TrimSpace(dichiarato))
	if f == "yaml" || f == "yml" {
		return "yaml"
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".yaml" || ext == ".yml" {
		return "yaml"
	}
	return "json"
}

func trovaDocumento(id string) (DocumentoConfig, bool) {
	for _, d := range documentiConfigurazione() {
		if d.ID == id {
			return d, true
		}
	}
	return DocumentoConfig{}, false
}

func revisione(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// validaDocumento usa un parser vero anche per YAML. Controllare soltanto
// parentesi e indentazione lascerebbe passare file che poi il client rifiuta.
func validaDocumento(d DocumentoConfig, contenuto string) error {
	if strings.TrimSpace(contenuto) == "" {
		return errors.New("il file e' vuoto")
	}
	if d.Formato == "yaml" {
		dec := yaml.NewDecoder(strings.NewReader(contenuto))
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			return fmt.Errorf("YAML non valido: %w", err)
		}
		// I file di configurazione sono un documento solo. Accettarne molti e
		// mostrarne uno al client e' un errore troppo difficile da vedere.
		var altro yaml.Node
		if err := dec.Decode(&altro); err != io.EOF {
			if err == nil {
				return errors.New("YAML non valido: contiene piu' documenti")
			}
			return fmt.Errorf("YAML non valido: %w", err)
		}
		return nil
	}

	var v any
	if err := json.Unmarshal([]byte(contenuto), &v); err != nil {
		return fmt.Errorf("JSON non valido: %w", err)
	}
	if v == nil {
		return errors.New("il JSON non puo' essere null")
	}
	if d.ID == "thelaap" {
		var c Config
		if err := json.Unmarshal([]byte(contenuto), &c); err != nil {
			return fmt.Errorf("configurazione theLAAP non valida: %w", err)
		}
		if len(c.Runtime) == 0 {
			return errors.New("la configurazione theLAAP deve contenere almeno un runtime")
		}
	}
	return nil
}

// convertiDocumento permette di lavorare anche in YAML su un file che sul
// disco resta JSON (e viceversa). E' particolarmente utile per i file grandi:
// si sceglie la sintassi piu' leggibile senza cambiare il formato atteso dal
// programma che lo usa.
func convertiDocumento(contenuto, da, a string) (string, error) {
	if da == a {
		return contenuto, nil
	}
	// JSON e' un sottoinsieme di YAML: passando da un nodo YAML conserviamo
	// ordine e interi grandi esattamente. Decodificare in map[string]any li
	// trasformerebbe in float64 (9007199254740993 diventerebbe 9007199254740992).
	if da == "json" && a == "yaml" {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(contenuto), &n); err != nil {
			return "", err
		}
		var usaBlocchi func(*yaml.Node)
		usaBlocchi = func(n *yaml.Node) {
			// Togli anche le virgolette imposte dalla sintassi JSON. Il taglio
			// resta !!str, quindi valori ambigui come "yes" verranno quotati di
			// nuovo dall'emettitore e non cambieranno tipo.
			n.Style = 0
			for _, figlio := range n.Content {
				usaBlocchi(figlio)
			}
		}
		usaBlocchi(&n)
		b, err := yaml.Marshal(&n)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	var v any
	if err := yaml.Unmarshal([]byte(contenuto), &v); err != nil {
		return "", err
	}
	if a == "json" {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return "", err
		}
		return string(append(b, '\n')), nil
	}
	b, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func scriviAtomico(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	modo := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		modo = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	nomeTmp := tmp.Name()
	defer os.Remove(nomeTmp)
	if err := tmp.Chmod(modo); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(nomeTmp, path)
}

func apiDocumenti(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		errJSONStatus(w, http.StatusMethodNotAllowed, "serve GET")
		return
	}
	scriviJSON(w, documentiConfigurazione())
}

// apiDocumento legge, valida e salva i file della lista bianca. La revisione
// evita di sovrascrivere in silenzio una modifica fatta nel frattempo da un
// altro programma o da un terminale.
func apiDocumento(w http.ResponseWriter, r *http.Request) {
	d, ok := trovaDocumento(r.URL.Query().Get("id"))
	if !ok {
		errJSONStatus(w, http.StatusNotFound, "file di configurazione sconosciuto")
		return
	}
	if r.Method == http.MethodGet {
		b, err := os.ReadFile(d.File)
		if err != nil {
			errJSONStatus(w, http.StatusNotFound, "non riesco a leggere il file: "+err.Error())
			return
		}
		nativo := d.Formato
		vista := strings.ToLower(r.URL.Query().Get("formato"))
		if vista != "json" && vista != "yaml" {
			vista = nativo
		}
		contenuto, err := convertiDocumento(string(b), nativo, vista)
		if err != nil {
			errJSON(w, "non riesco a convertire il file: "+err.Error())
			return
		}
		d.Formato = vista
		scriviJSON(w, documentoLetto{DocumentoConfig: d, FormatoNativo: nativo,
			Contenuto: contenuto, Revisione: revisione(b)})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		errJSONStatus(w, http.StatusMethodNotAllowed, "serve GET oppure POST")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	var req struct {
		Contenuto string `json:"contenuto"`
		Formato   string `json:"formato"`
		Revisione string `json:"revisione"`
		// SoloValida non scrive niente e non crea backup.
		SoloValida bool `json:"soloValida"`
		Forza      bool `json:"forza"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non valido: "+err.Error())
		return
	}
	formatoInput := strings.ToLower(req.Formato)
	if formatoInput != "json" && formatoInput != "yaml" {
		formatoInput = d.Formato
	}
	dInput := d
	dInput.Formato = formatoInput
	if err := validaDocumento(dInput, req.Contenuto); err != nil {
		errJSON(w, err.Error())
		return
	}
	contenutoNativo, err := convertiDocumento(req.Contenuto, formatoInput, d.Formato)
	if err != nil {
		errJSON(w, "non riesco a convertire il file: "+err.Error())
		return
	}
	if err := validaDocumento(d, contenutoNativo); err != nil {
		errJSON(w, err.Error())
		return
	}
	if req.SoloValida {
		scriviJSON(w, map[string]any{"ok": true, "messaggio": strings.ToUpper(formatoInput) + " valido"})
		return
	}

	attuale, err := os.ReadFile(d.File)
	if err != nil && !os.IsNotExist(err) {
		errJSON(w, "non riesco a rileggere il file: "+err.Error())
		return
	}
	if err == nil && req.Revisione != "" && req.Revisione != revisione(attuale) && !req.Forza {
		errJSONStatus(w, http.StatusConflict,
			"il file e' cambiato dopo che lo hai aperto; ricaricalo per non perdere le modifiche esterne")
		return
	}
	if err == nil {
		if err := backup(d.File); err != nil {
			errJSON(w, "non riesco a fare la copia di sicurezza: "+err.Error())
			return
		}
	}
	if err := scriviAtomico(d.File, []byte(contenutoNativo)); err != nil {
		errJSON(w, "non riesco a salvare: "+err.Error())
		return
	}

	// Il file principale ha effetto subito: non serve riavviare il pannello
	// per cambiare radici, runtime o limiti. La porta resta quella del processo
	// corrente e cambiera' al prossimo avvio.
	if d.ID == "thelaap" {
		var c Config
		_ = json.Unmarshal([]byte(contenutoNativo), &c) // gia' validato sopra
		cfgMu.Lock()
		CFG = c
		cfgMu.Unlock()
	}
	scordaRemoto()
	rinfrescaMemoria()
	nuova := revisione([]byte(contenutoNativo))
	scriviJSON(w, map[string]any{
		"ok": true, "revisione": nuova,
		"messaggio": "salvato " + filepath.Base(d.File) + " (copia di sicurezza in " + BACKUP + ")",
	})
}
