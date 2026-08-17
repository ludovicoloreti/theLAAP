package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Togliere un modello dal disco.
//
// Il pannello sapeva solo aggiungerne: cercare su HuggingFace e scaricare. Su
// uno già presente si poteva cambiare l'etichetta e provarlo, nient'altro.
//
// Questo file lo completa, con tre cautele imparate a caro prezzo il 28/07/2026
// facendo la stessa operazione a mano:
//
//  1. La prima rimozione NON cancella: sposta in un deposito. 30 GB si
//     riscaricano in ore, e un "rimuovi" premuto per sbaglio non deve costarle.
//     La cancellazione definitiva esiste, ma soltanto dal deposito e con una
//     conferma separata.
//  2. Guarda chi dipende dal modello PRIMA di toccarlo. Stavo per togliere un
//     modello da 5,5 GB convinto che non lo usasse nessuno: era quello che il
//     pannello stesso usa per il riquadro di aiuto. Se ne accorge solo chi
//     controlla.
//  3. Dice che LM Studio e la cache HuggingFace condividono i file. Le voci
//     di LM Studio sono collegamenti dentro la cache: togliere dalla cache
//     rompe anche LM Studio, e da fuori sembrano due modelli distinti.

// RadiciModelli: dove cercare i modelli sul disco.
//
// I primi due sono percorsi standard dei rispettivi prodotti, uguali su ogni
// macchina. Cartelle scelte dall'utente (per esempio quella passata a un
// runtime con --model-dir) si aggiungono in configurazione con "radiciModelli":
// scriverne una qui renderebbe il pannello sbagliato su tutti gli altri
// computer.
var RadiciModelli = []string{
	"~/.cache/huggingface/hub",
	"~/.lmstudio/models",
}

// radici: quelle note più quelle dichiarate dall'utente.
func radici() []string {
	out := append([]string{}, RadiciModelli...)
	return append(out, cfg().RadiciModelli...)
}

// DepositoModelli: dove finisce cio' che si rimuove. E' una variabile per
// poter collaudare archiviazione e ripristino in una cartella temporanea.
var DepositoModelli = "~/.modelli-rimossi"

func espandiHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(h, p[2:])
	}
	return p
}

// nomeSicuro rifiuta gli identificativi che potrebbero uscire dalle radici.
// Gli id dei modelli arrivano da servizi di terzi: senza questo, un nome con
// "../.." trasformerebbe una rimozione in una cancellazione arbitraria.
func nomeSicuro(id string) bool {
	if strings.TrimSpace(id) == "" || len(id) > 200 {
		return false
	}
	if strings.Contains(id, "..") || strings.ContainsAny(id, "\x00\n\r") {
		return false
	}
	if filepath.IsAbs(id) {
		return false
	}
	return true
}

// PostoSuDisco: una copia fisica del modello.
type PostoSuDisco struct {
	Percorso     string  `json:"percorso"`
	GB           float64 `json:"gb"`
	Collegamenti bool    `json:"collegamenti"` // true = punta altrove, non occupa spazio suo
}

// Dipendenza: qualcosa che smetterebbe di funzionare.
type Dipendenza struct {
	Cosa   string `json:"cosa"`
	Perche string `json:"perche"`
	Grave  bool   `json:"grave"` // true = sconsiglio di procedere
}

type EsameModello struct {
	ID         string         `json:"id"`
	Posti      []PostoSuDisco `json:"posti"`
	GBTotali   float64        `json:"gbTotali"`
	Dipendenze []Dipendenza   `json:"dipendenze"`
	Rimovibile bool           `json:"rimovibile"`
	Nota       string         `json:"nota,omitempty"`
}

type fileArchivio struct {
	Originale  string `json:"originale"`
	Archiviato string `json:"archiviato"` // percorso relativo dentro la voce
}

type manifestoArchivio struct {
	Versione       int            `json:"versione"`
	ID             string         `json:"id"`
	Modello        string         `json:"modello"`
	Runtime        string         `json:"runtime,omitempty"`
	Creato         time.Time      `json:"creato"`
	GB             float64        `json:"gb"`
	File           []fileArchivio `json:"file"`
	Configurazioni []Modello      `json:"configurazioni,omitempty"`
}

// VoceArchivio e' la vista sicura mandata al browser. I percorsi originali
// restano nel manifesto locale e servono soltanto al ripristino.
type VoceArchivio struct {
	ID              string  `json:"id"`
	Modello         string  `json:"modello"`
	GB              float64 `json:"gb"`
	Creato          string  `json:"creato"`
	Posti           int     `json:"posti"`
	Runtime         string  `json:"runtime,omitempty"`
	Ripristinabile  bool    `json:"ripristinabile"`
	ArchivioVecchio bool    `json:"archivioVecchio,omitempty"`
	Nota            string  `json:"nota,omitempty"`
}

// trovaSuDisco cerca le cartelle che contengono il modello.
//
// Un modello può stare in più posti con nomi diversi: la cache HuggingFace usa
// "models--<publisher>--<nome>", LM Studio "<publisher>/<nome>". Si confronta
// sulla forma normalizzata.
func trovaSuDisco(id string) []PostoSuDisco {
	// Il prefisso "models--" della cache HuggingFace va TOLTO, non sostituito
	// con un separatore: sostituendolo resta un trattino iniziale e la forma
	// della cache non combacia mai con quella di LM Studio.
	norm := func(s string) string {
		s = strings.ToLower(s)
		s = strings.TrimPrefix(s, "models--")
		for _, r := range []string{"--", "/", "_", "."} {
			s = strings.ReplaceAll(s, r, "-")
		}
		return strings.Trim(s, "-")
	}
	bersaglio := norm(id)
	var out []PostoSuDisco
	// Percorsi reali già trovati: servono a riconoscere le cartelle che
	// puntano QUI con altri nomi (vedi sotto).
	visti := map[string]bool{}

	for _, radice := range radici() {
		r := espandiHome(radice)
		st, err := os.Stat(r)
		if err != nil || !st.IsDir() {
			continue
		}
		// Due livelli bastano: <radice>/<modello> e <radice>/<publisher>/<modello>
		voci, _ := os.ReadDir(r)
		for _, v := range voci {
			// .locks e simili sono contabilità della cache, non modelli:
			// includerli faceva comparire "2 posti" per un modello solo.
			if !v.IsDir() || strings.HasPrefix(v.Name(), ".") {
				continue
			}
			p1 := filepath.Join(r, v.Name())
			if norm(v.Name()) == bersaglio {
				out = append(out, esaminaCartella(p1))
				visti[p1] = true
				continue
			}
			sotto, _ := os.ReadDir(p1)
			for _, s := range sotto {
				if !s.IsDir() {
					continue
				}
				if norm(v.Name()+"/"+s.Name()) == bersaglio || norm(s.Name()) == bersaglio {
					q := filepath.Join(p1, s.Name())
					out = append(out, esaminaCartella(q))
					visti[q] = true
				}
			}
		}
	}

	// NON si cerca di indovinare tutte le copie camminando nel filesystem.
	// Ci ho provato: LM Studio crea collegamenti a CARTELLE con nomi diversi
	// ("gemma-4-26B-A4B-MLX-8bit" per "gemma-4-26B-A4B-it-MLX-8bit"), annidati
	// a profondità variabile, e ogni euristica sbagliava un caso — arrivando a
	// proporre di spostare la cartella di un publisher, cioè anche i modelli
	// che si volevano tenere. Meglio trovare la cartella vera e AVVISARE che
	// qualcuno ci punta: chi legge decide, invece di fidarsi di un elenco che
	// a volte mente. Vedi collegamentiVerso().
	return out
}

// collegamentiVerso: chi punta a questi posti da fuori.
//
// Solo per avvisare, non per spostare: si scende al massimo di due livelli
// nelle radici note e si guarda dove finiscono i collegamenti.
func collegamentiVerso(posti []PostoSuDisco) []string {
	var out []string
	for _, radice := range radici() {
		r := espandiHome(radice)
		var scendi func(string, int)
		scendi = func(dir string, prof int) {
			if prof > 2 || len(out) > 8 {
				return
			}
			voci, err := os.ReadDir(dir)
			if err != nil {
				return
			}
			for _, v := range voci {
				if strings.HasPrefix(v.Name(), ".") {
					continue
				}
				q := filepath.Join(dir, v.Name())
				if dentroUnPosto(q, posti) {
					continue
				}
				info, err := os.Lstat(q)
				if err != nil {
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					if reale, err := filepath.EvalSymlinks(q); err == nil && dentroRadici(reale, posti) {
						h, _ := os.UserHomeDir()
						out = append(out, strings.Replace(q, h, "~", 1))
					}
					continue
				}
				if info.IsDir() {
					scendi(q, prof+1)
				}
			}
		}
		scendi(r, 0)
	}
	return out
}

// dentroRadici: questo percorso reale finisce dentro uno dei posti trovati?
func dentroRadici(reale string, posti []PostoSuDisco) bool {
	for _, p := range posti {
		r, err := filepath.EvalSymlinks(p.Percorso)
		if err != nil {
			continue
		}
		if reale == r || strings.HasPrefix(reale, r+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// dentroUnPosto: questa cartella sta già dentro un posto trovato?
func dentroUnPosto(dir string, posti []PostoSuDisco) bool {
	for _, p := range posti {
		if strings.HasPrefix(dir, p.Percorso+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// puntaDentro: i file di questa cartella risolvono dentro uno dei posti trovati?
//
// Entrambi i lati vanno risolti prima di confrontarli: su macOS /var è un
// collegamento a /private/var, quindi il percorso risolto di un file e quello
// memorizzato della cartella non combaciano mai per prefisso.
func puntaDentro(dir string, posti []PostoSuDisco) bool {
	voci, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	radici := make([]string, 0, len(posti))
	for _, p := range posti {
		if r, err := filepath.EvalSymlinks(p.Percorso); err == nil {
			radici = append(radici, r+string(filepath.Separator))
		}
	}
	for _, v := range voci {
		if v.IsDir() {
			continue
		}
		reale, err := filepath.EvalSymlinks(filepath.Join(dir, v.Name()))
		if err != nil {
			continue
		}
		for _, r := range radici {
			if strings.HasPrefix(reale, r) {
				return true
			}
		}
	}
	return false
}

// esaminaCartella misura una cartella e capisce se contiene file veri o solo
// collegamenti verso un'altra copia.
func esaminaCartella(p string) PostoSuDisco {
	var byte0 int64
	var file, link int
	filepath.WalkDir(p, func(q string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			link++
			return nil
		}
		file++
		byte0 += info.Size()
		return nil
	})
	return PostoSuDisco{
		Percorso:     p,
		GB:           float64(byte0) / 1e9,
		Collegamenti: link > 0 && file <= link,
	}
}

// esamina raccoglie tutto ciò che serve per decidere, senza toccare niente.
func esamina(id string) EsameModello {
	e := EsameModello{ID: id, Posti: []PostoSuDisco{}, Dipendenze: []Dipendenza{}}
	if !nomeSicuro(id) {
		e.Nota = "identificativo non valido"
		return e
	}
	// Mai nil: una lista nil esce come "null" nel JSON e la pagina va in
	// errore appena prova a scorrerla — stessa cautela di leggeMemoria().
	if p := trovaSuDisco(id); p != nil {
		e.Posti = p
	}
	for _, p := range e.Posti {
		e.GBTotali += p.GB
	}
	if len(e.Posti) == 0 {
		e.Nota = "non l'ho trovato su questo disco: potrebbe essere su un server remoto, " +
			"e in quel caso non c'è niente da togliere di qui"
		return e
	}

	basso := strings.ToLower(id)

	// È il modello che il pannello usa per rispondere alle domande?
	if m := modelloAiuto(); m != "" && strings.EqualFold(m, id) {
		e.Dipendenze = append(e.Dipendenze, Dipendenza{
			Cosa:   "il riquadro di aiuto di questo pannello",
			Perche: "è il modello che risponde alle domande; senza, ripiegherebbe su uno molto più grosso",
			Grave:  true,
		})
	}

	// È caricato in memoria adesso?
	for _, c := range memoriaCorrente().Caricati {
		if strings.EqualFold(c.Nome, id) {
			e.Dipendenze = append(e.Dipendenze, Dipendenza{
				Cosa:   "è in memoria adesso",
				Perche: "va prima scaricato, altrimenti il programma che lo usa può comportarsi male",
				Grave:  true,
			})
		}
	}

	// Lo cita qualche client?
	for _, cl := range cfg().Clienti {
		f := espandiHome(cl.File)
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(b)), basso) {
			e.Dipendenze = append(e.Dipendenze, Dipendenza{
				Cosa:   cl.Nome + " lo ha in configurazione",
				Perche: "dopo la rimozione resterebbe una voce che punta a un modello inesistente",
				Grave:  false,
			})
		}
	}

	// Chi ci punta da fuori: togliere la cartella lo lascia nel vuoto.
	if q := collegamentiVerso(e.Posti); len(q) > 0 {
		e.Dipendenze = append(e.Dipendenze, Dipendenza{
			Cosa: "altri programmi ci puntano: " + strings.Join(q, ", "),
			Perche: "sono collegamenti alla stessa copia, non copie separate: " +
				"spostandola smetteranno di vedere il modello",
			Grave: false,
		})
	}

	e.Rimovibile = true
	for _, d := range e.Dipendenze {
		if d.Grave {
			e.Rimovibile = false
		}
	}
	return e
}

func apiEsaminaModello(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	scriviJSON(w, esamina(id))
}

const nomeManifestoArchivio = "archivio.json"

func slugArchivio(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
			b.WriteByte('-')
		}
		if b.Len() >= 42 {
			break
		}
	}
	return strings.Trim(b.String(), "-")
}

func rollbackSpostamenti(file []fileArchivio, voce string) error {
	var errori []string
	for i := len(file) - 1; i >= 0; i-- {
		src := filepath.Join(voce, filepath.FromSlash(file[i].Archiviato))
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file[i].Originale), 0o755); err != nil {
			errori = append(errori, err.Error())
			continue
		}
		if err := os.Rename(src, file[i].Originale); err != nil {
			errori = append(errori, err.Error())
		}
	}
	if len(errori) > 0 {
		return errors.New(strings.Join(errori, "; "))
	}
	return nil
}

// archivia crea una voce autonoma con un manifesto. La vecchia versione
// appiattiva i nomi dentro una cartella per data: dopo non era piu' possibile
// sapere con certezza dove rimetterli. Il manifesto rende il ripristino un
// pulsante e permette il rollback se uno degli spostamenti fallisce.
func archivia(e EsameModello, runtime string, configurazioni []Modello) (VoceArchivio, error) {
	radice := espandiHome(DepositoModelli)
	if err := os.MkdirAll(radice, 0o700); err != nil {
		return VoceArchivio{}, err
	}
	if st, err := os.Lstat(radice); err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return VoceArchivio{}, errors.New("la radice dell'archivio non e' una cartella reale")
	}
	adesso := time.Now()
	base := adesso.Format("20060102-150405.000000000") + "-" + slugArchivio(e.ID)
	if strings.HasSuffix(base, "-") {
		base += "modello"
	}
	id := base
	voce := filepath.Join(radice, id)
	for n := 2; ; n++ {
		err := os.Mkdir(voce, 0o700)
		if err == nil {
			break
		}
		if !os.IsExist(err) {
			return VoceArchivio{}, err
		}
		id = fmt.Sprintf("%s-%d", base, n)
		voce = filepath.Join(radice, id)
	}
	if err := os.Mkdir(filepath.Join(voce, "file"), 0o700); err != nil {
		_ = os.Remove(voce)
		return VoceArchivio{}, err
	}

	man := manifestoArchivio{Versione: 1, ID: id, Modello: e.ID, Runtime: runtime,
		Creato: adesso, GB: e.GBTotali, File: []fileArchivio{}, Configurazioni: configurazioni}
	for i, p := range e.Posti {
		nome := fmt.Sprintf("file/%02d-%s", i+1, filepath.Base(p.Percorso))
		man.File = append(man.File, fileArchivio{Originale: p.Percorso, Archiviato: nome})
	}
	// Il manifesto nasce prima degli spostamenti: se il processo viene chiuso a
	// meta', al prossimo avvio sappiamo ancora quali elementi sono gia' tornati
	// e quali sono rimasti nel deposito.
	b, err := json.MarshalIndent(man, "", "  ")
	if err == nil {
		err = scriviAtomico(filepath.Join(voce, nomeManifestoArchivio), append(b, '\n'))
	}
	if err != nil {
		_ = os.RemoveAll(voce)
		return VoceArchivio{}, fmt.Errorf("non riesco a registrare il ripristino: %w", err)
	}

	spostati := []fileArchivio{}
	for _, f := range man.File {
		dest := filepath.Join(voce, filepath.FromSlash(f.Archiviato))
		p := f.Originale
		if _, err := os.Lstat(p); err != nil {
			if rb := rollbackSpostamenti(spostati, voce); rb == nil {
				_ = os.RemoveAll(voce)
				return VoceArchivio{}, fmt.Errorf("%s non e' piu' disponibile: %w", p, err)
			}
			return VoceArchivio{}, fmt.Errorf("%s non e' piu' disponibile e il rollback e' incompleto; non cancello il deposito %s", p, voce)
		}
		if err := os.Rename(p, dest); err != nil {
			if rb := rollbackSpostamenti(spostati, voce); rb == nil {
				_ = os.RemoveAll(voce)
				return VoceArchivio{}, fmt.Errorf("spostamento fallito per %s: %w", p, err)
			}
			return VoceArchivio{}, fmt.Errorf("spostamento fallito e rollback incompleto; non cancello il deposito %s", voce)
		}
		spostati = append(spostati, f)
	}
	return VoceArchivio{
		ID: id, Modello: e.ID, GB: e.GBTotali, Creato: adesso.Format(time.RFC3339),
		Posti: len(man.File), Runtime: runtime, Ripristinabile: true,
	}, nil
}

func leggiManifesto(voce string) (manifestoArchivio, error) {
	b, err := os.ReadFile(filepath.Join(voce, nomeManifestoArchivio))
	if err != nil {
		return manifestoArchivio{}, err
	}
	var m manifestoArchivio
	if err := json.Unmarshal(b, &m); err != nil {
		return manifestoArchivio{}, err
	}
	if m.Versione != 1 || m.ID == "" || len(m.File) == 0 {
		return manifestoArchivio{}, errors.New("manifesto incompleto")
	}
	return m, nil
}

// percorsoArchivio accetta soltanto una voce diretta nuova oppure
// data/nome per gli archivi creati dalla versione precedente. Non segue un
// collegamento simbolico usato per far uscire una cancellazione dal deposito.
func percorsoArchivio(id string) (string, error) {
	if id == "" || filepath.IsAbs(id) || strings.Contains(id, "..") || strings.ContainsAny(id, "\x00\n\r") {
		return "", errors.New("voce di archivio non valida")
	}
	parti := strings.Split(filepath.ToSlash(id), "/")
	if len(parti) > 2 {
		return "", errors.New("voce di archivio non valida")
	}
	radice := filepath.Clean(espandiHome(DepositoModelli))
	stRadice, err := os.Lstat(radice)
	if err != nil {
		return "", err
	}
	if stRadice.Mode()&os.ModeSymlink != 0 || !stRadice.IsDir() {
		return "", errors.New("la radice dell'archivio non e' una cartella reale")
	}
	p := radice
	// Controlla ogni componente, non soltanto l'ultima: data/modello sarebbe
	// lessicalmente dentro il deposito anche se "data" fosse un symlink verso
	// un'altra cartella del disco.
	for _, parte := range parti {
		p = filepath.Join(p, parte)
		st, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
			return "", errors.New("la voce di archivio non e' una cartella reale")
		}
	}
	rel, err := filepath.Rel(radice, p)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("voce di archivio non valida")
	}
	return p, nil
}

func elencoArchivio() []VoceArchivio {
	radice := espandiHome(DepositoModelli)
	voci, err := os.ReadDir(radice)
	if err != nil {
		return []VoceArchivio{}
	}
	out := []VoceArchivio{}
	for _, v := range voci {
		if !v.IsDir() || strings.HasPrefix(v.Name(), ".") {
			continue
		}
		p := filepath.Join(radice, v.Name())
		if m, err := leggiManifesto(p); err == nil {
			out = append(out, VoceArchivio{ID: v.Name(), Modello: m.Modello, GB: m.GB,
				Creato: m.Creato.Format(time.RFC3339), Posti: len(m.File), Runtime: m.Runtime, Ripristinabile: true})
			continue
		}
		// Compatibilita' con il vecchio deposito YYYY-MM-DD/<cartella>. Non
		// conosciamo il percorso originale, quindi si puo' vedere ed eliminare
		// ma non promettiamo un ripristino automatico inventato.
		if _, err := time.Parse("2006-01-02", v.Name()); err != nil {
			continue
		}
		figli, _ := os.ReadDir(p)
		for _, f := range figli {
			if !f.IsDir() || strings.HasPrefix(f.Name(), ".") {
				continue
			}
			q := filepath.Join(p, f.Name())
			misura := esaminaCartella(q)
			out = append(out, VoceArchivio{
				ID: v.Name() + "/" + f.Name(), Modello: f.Name(), GB: misura.GB,
				Creato: v.Name(), Posti: 1, Ripristinabile: false, ArchivioVecchio: true,
				Nota: "archiviato da una versione precedente: il percorso originale non era registrato",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Creato > out[j].Creato })
	return out
}

func ripristinaArchivio(id string) (manifestoArchivio, error) {
	voce, err := percorsoArchivio(id)
	if err != nil {
		return manifestoArchivio{}, err
	}
	m, err := leggiManifesto(voce)
	if err != nil || m.ID != id {
		return manifestoArchivio{}, errors.New("questo vecchio archivio non contiene i dati necessari al ripristino automatico")
	}
	// Tutti i conflitti si controllano prima di muovere il primo byte.
	for _, f := range m.File {
		if !percorsoInRadiceModelli(f.Originale) {
			return manifestoArchivio{}, fmt.Errorf("il percorso originale non e' piu' fra le radici dei modelli: %s", f.Originale)
		}
		_, errOriginale := os.Lstat(f.Originale)
		originaleEsiste := errOriginale == nil
		if errOriginale != nil && !os.IsNotExist(errOriginale) {
			return manifestoArchivio{}, errOriginale
		}
		src := filepath.Join(voce, filepath.FromSlash(f.Archiviato))
		rel, err := filepath.Rel(voce, src)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return manifestoArchivio{}, errors.New("manifesto con percorso non valido")
		}
		_, errArchivio := os.Lstat(src)
		archivioEsiste := errArchivio == nil
		if errArchivio != nil && !os.IsNotExist(errArchivio) {
			return manifestoArchivio{}, errArchivio
		}
		if originaleEsiste && archivioEsiste {
			return manifestoArchivio{}, fmt.Errorf("non sovrascrivo %s: esiste sia sul disco sia nell'archivio", f.Originale)
		}
		if !originaleEsiste && !archivioEsiste {
			return manifestoArchivio{}, fmt.Errorf("manca %s sia dal disco sia dall'archivio", f.Archiviato)
		}
	}
	spostati := []fileArchivio{}
	for _, f := range m.File {
		src := filepath.Join(voce, filepath.FromSlash(f.Archiviato))
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue // era gia' tornato durante un rollback interrotto
		}
		if err := os.MkdirAll(filepath.Dir(f.Originale), 0o755); err != nil {
			rollbackRipristino(spostati, voce)
			return manifestoArchivio{}, err
		}
		if err := os.Rename(src, f.Originale); err != nil {
			rollbackRipristino(spostati, voce)
			return manifestoArchivio{}, fmt.Errorf("ripristino fallito: %w", err)
		}
		spostati = append(spostati, f)
	}
	if err := os.RemoveAll(voce); err != nil {
		return manifestoArchivio{}, fmt.Errorf("modello ripristinato, ma non riesco a pulire il manifesto: %w", err)
	}
	return m, nil
}

func percorsoInRadiceModelli(p string) bool {
	p = filepath.Clean(p)
	for _, r := range radici() {
		radice := filepath.Clean(espandiHome(r))
		rel, err := filepath.Rel(radice, p)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func rollbackRipristino(spostati []fileArchivio, voce string) {
	for i := len(spostati) - 1; i >= 0; i-- {
		f := spostati[i]
		_ = os.MkdirAll(filepath.Dir(filepath.Join(voce, filepath.FromSlash(f.Archiviato))), 0o700)
		_ = os.Rename(f.Originale, filepath.Join(voce, filepath.FromSlash(f.Archiviato)))
	}
}

// apiRimuoviModello archivia il modello. La cancellazione definitiva e'
// deliberatamente separata e puo' agire soltanto su una voce gia' nel deposito.
func apiRimuoviModello(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Runtime string `json:"runtime"`
		Forza   bool   `json:"forza"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile")
		return
	}
	e := esamina(req.ID)
	if len(e.Posti) == 0 {
		errJSON(w, e.Nota)
		return
	}
	if !e.Rimovibile && !req.Forza {
		var motivi []string
		for _, d := range e.Dipendenze {
			if d.Grave {
				motivi = append(motivi, d.Cosa+" — "+d.Perche)
			}
		}
		errJSON(w, "non lo archivio: "+strings.Join(motivi, "; "))
		return
	}
	configurate, _ := statoConfig()
	associate := []Modello{}
	for _, m := range configurate {
		if strings.EqualFold(m.ID, req.ID) {
			associate = append(associate, m)
		}
	}
	voce, err := archivia(e, req.Runtime, associate)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	rinfrescaMemoria()
	scriviJSON(w, map[string]any{
		"ok": true, "gb": voce.GB, "archivio": voce,
		"comeTornareIndietro": "apri Modelli archiviati e premi Ripristina",
	})
}

func apiArchivioModelli(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		errJSONStatus(w, http.StatusMethodNotAllowed, "serve GET")
		return
	}
	scriviJSON(w, elencoArchivio())
}

func apiRipristinaModello(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile")
		return
	}
	m, err := ripristinaArchivio(req.ID)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	rinfrescaMemoria()
	scriviJSON(w, map[string]any{"ok": true, "modello": m.Modello, "runtime": m.Runtime,
		"gb": m.GB, "configurazioni": m.Configurazioni})
}

func apiEliminaArchivio(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID       string `json:"id"`
		Conferma string `json:"conferma"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile")
		return
	}
	if req.Conferma != "ELIMINA "+req.ID {
		errJSON(w, "conferma non valida")
		return
	}
	p, err := percorsoArchivio(req.ID)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	misura := esaminaCartella(p).GB
	if err := os.RemoveAll(p); err != nil {
		errJSON(w, "cancellazione fallita: "+err.Error())
		return
	}
	// Se era una voce del vecchio archivio, prova a togliere la cartella-data
	// ormai vuota. os.Remove non tocca cartelle non vuote.
	if strings.Contains(filepath.ToSlash(req.ID), "/") {
		_ = os.Remove(filepath.Dir(p))
	}
	scriviJSON(w, map[string]any{"ok": true, "gb": misura, "id": req.ID})
}
