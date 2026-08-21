package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
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

// ModelRoots: dove cercare i modelli sul disco.
//
// I primi due sono percorsi standard dei rispettivi prodotti, uguali su ogni
// macchina. Cartelle scelte dall'utente (per esempio quella passata a un
// runtime con --model-dir) si aggiungono in configurazione con "radiciModelli":
// scriverne una qui renderebbe il pannello sbagliato su tutti gli altri
// computer.
var ModelRoots = []string{
	"~/.cache/huggingface/hub",
	"~/.lmstudio/models",
}

// radici: quelle note più quelle dichiarate dall'utente.
func radici() []string {
	out := append([]string{}, ModelRoots...)
	return append(out, cfg().ModelRoots...)
}

// ModelStore: dove finisce cio' che si rimuove. E' una variabile per
// poter collaudare archiviazione e ripristino in una cartella temporanea.
var ModelStore = "~/.modelli-rimossi"

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		h, err := os.UserHomeDir()
		if err != nil {
			return p
		}
		return filepath.Join(h, p[2:])
	}
	return p
}

// safeName rifiuta gli identificativi che potrebbero uscire dalle radici.
// Gli id dei modelli arrivano da servizi di terzi: senza questo, un nome con
// "../.." trasformerebbe una rimozione in una cancellazione arbitraria.
func safeName(id string) bool {
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

// lmStudioAliases traduce l'identificativo breve servito dall'API nel
// percorso reale indicizzato da LM Studio. Per esempio l'API dice
// "gemma-4-31b-it-mlx", mentre sul disco c'e'
// "lmstudio-community/gemma-4-31B-it-MLX-8bit". Indovinare il publisher o la
// quantizzazione con confronti sfocati rischierebbe di archiviare il modello
// sbagliato; `lms ls --json` e' invece la fonte che usa LM Studio stesso.
func lmStudioAliases(id string) []string {
	h, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	bin := filepath.Join(h, ".lmstudio", "bin", "lms")
	if _, err := os.Stat(bin); err != nil {
		return nil
	}
	b, err := exec.Command(bin, "ls", "--json").Output()
	if err != nil {
		return nil
	}
	return lmStudioAliasesJSON(id, b)
}

func lmStudioAliasesJSON(id string, b []byte) []string {
	var voci []struct {
		ModelKey   string `json:"modelKey"`
		Path       string `json:"path"`
		Identifier string `json:"indexedModelIdentifier"`
	}
	if json.Unmarshal(b, &voci) != nil {
		return nil
	}
	var out []string
	for _, v := range voci {
		if !strings.EqualFold(v.ModelKey, id) {
			continue
		}
		for _, p := range []string{v.Path, v.Identifier} {
			if p != "" && !containsFold(out, p) {
				out = append(out, p)
			}
		}
	}
	return out
}

func containsFold(ss []string, s string) bool {
	for _, x := range ss {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// DiskSpace: una copia fisica del modello.
type DiskSpace struct {
	Percorso     string  `json:"percorso"`
	GB           float64 `json:"gb"`
	Collegamenti bool    `json:"collegamenti"` // true = punta altrove, non occupa spazio suo
}

// Dependency: qualcosa che smetterebbe di funzionare.
type Dependency struct {
	Cosa   string `json:"cosa"`
	Perche string `json:"perche"`
	Grave  bool   `json:"grave"` // true = sconsiglio di procedere
}

type ModelExam struct {
	ID         string       `json:"id"`
	Posti      []DiskSpace  `json:"posti"`
	GBTotali   float64      `json:"gbTotali"`
	Dipendenze []Dependency `json:"dipendenze"`
	Rimovibile bool         `json:"rimovibile"`
	Nota       string       `json:"nota,omitempty"`
}

type archiveFiles struct {
	Originale  string `json:"originale"`
	Archiviato string `json:"archiviato"` // percorso relativo dentro la voce
}

type archiveManifest struct {
	Versione       int            `json:"versione"`
	ID             string         `json:"id"`
	Model          string         `json:"modello"`
	Runtime        string         `json:"runtime,omitempty"`
	Creato         time.Time      `json:"creato"`
	GB             float64        `json:"gb"`
	File           []archiveFiles `json:"file"`
	Configurazioni []Model        `json:"configurazioni,omitempty"`
}

// ArchiveEntry e' la vista sicura mandata al browser. I percorsi originali
// restano nel manifesto locale e servono soltanto al ripristino.
type ArchiveEntry struct {
	ID              string  `json:"id"`
	Model           string  `json:"modello"`
	GB              float64 `json:"gb"`
	Creato          string  `json:"creato"`
	Posti           int     `json:"posti"`
	Runtime         string  `json:"runtime,omitempty"`
	Ripristinabile  bool    `json:"ripristinabile"`
	ArchivioVecchio bool    `json:"archivioVecchio,omitempty"`
	Nota            string  `json:"nota,omitempty"`
}

// findOnDisk cerca le cartelle che contengono il modello.
//
// Un modello può stare in più posti con nomi diversi: la cache HuggingFace usa
// "models--<publisher>--<nome>", LM Studio "<publisher>/<nome>". Si confronta
// sulla forma normalizzata.
func findOnDisk(id string) []DiskSpace {
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
	bersagli := []string{norm(id)}
	for _, a := range lmStudioAliases(id) {
		if n := norm(a); n != "" && !containsFold(bersagli, n) {
			bersagli = append(bersagli, n)
		}
	}
	corrisponde := func(s string) bool { return containsFold(bersagli, norm(s)) }
	var out []DiskSpace
	// Percorsi reali già trovati: servono a riconoscere le cartelle che
	// puntano QUI con altri nomi (vedi sotto).
	visti := map[string]bool{}

	for _, radice := range radici() {
		r := expandHome(radice)
		st, err := os.Stat(r)
		if err != nil || !st.IsDir() {
			continue
		}
		// Due livelli bastano: <radice>/<modello> e <radice>/<publisher>/<modello>
		voci, _ := os.ReadDir(r)
		for _, v := range voci {
			// .locks e simili sono contabilità della cache, non modelli:
			// includerli faceva comparire "2 posti" per un modello solo.
			if strings.HasPrefix(v.Name(), ".") {
				continue
			}
			p1 := filepath.Join(r, v.Name())
			st1, err := os.Stat(p1) // segue anche i collegamenti a cartelle
			if err != nil || !st1.IsDir() {
				continue
			}
			if corrisponde(v.Name()) {
				out = append(out, examineFolder(p1))
				visti[p1] = true
				continue
			}
			sotto, _ := os.ReadDir(p1)
			for _, s := range sotto {
				q := filepath.Join(p1, s.Name())
				st2, err := os.Stat(q) // LM Studio usa collegamenti a snapshot HF
				if err != nil || !st2.IsDir() {
					continue
				}
				if corrisponde(v.Name()+"/"+s.Name()) || corrisponde(s.Name()) {
					out = append(out, examineFolder(q))
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
	// a volte mente. Vedi linksTo().
	return out
}

// linksTo: chi punta a questi posti da fuori.
//
// Solo per avvisare, non per spostare: si scende al massimo di due livelli
// nelle radici note e si guarda dove finiscono i collegamenti.
func linksTo(posti []DiskSpace) []string {
	var out []string
	for _, radice := range radici() {
		r := expandHome(radice)
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
				if containedIn(q, posti) {
					continue
				}
				info, err := os.Lstat(q)
				if err != nil {
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					if reale, err := filepath.EvalSymlinks(q); err == nil && insideRoots(reale, posti) {
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

// insideRoots: questo percorso reale finisce dentro uno dei posti trovati?
func insideRoots(reale string, posti []DiskSpace) bool {
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

// containedIn: questa cartella sta già dentro un posto trovato?
func containedIn(dir string, posti []DiskSpace) bool {
	for _, p := range posti {
		if strings.HasPrefix(dir, p.Percorso+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// pointsInside: i file di questa cartella risolvono dentro uno dei posti trovati?
//
// Entrambi i lati vanno risolti prima di confrontarli: su macOS /var è un
// collegamento a /private/var, quindi il percorso risolto di un file e quello
// memorizzato della cartella non combaciano mai per prefisso.
func pointsInside(dir string, posti []DiskSpace) bool {
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

// examineFolder misura una cartella e capisce se contiene file veri o solo
// collegamenti verso un'altra copia.
func examineFolder(p string) DiskSpace {
	if st, err := os.Lstat(p); err == nil && st.Mode()&os.ModeSymlink != 0 {
		return DiskSpace{Percorso: p, Collegamenti: true}
	}
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
	return DiskSpace{
		Percorso:     p,
		GB:           float64(byte0) / 1e9,
		Collegamenti: link > 0 && file <= link,
	}
}

// esamina raccoglie tutto ciò che serve per decidere, senza toccare niente.
func esamina(id string) ModelExam {
	e := ModelExam{ID: id, Posti: []DiskSpace{}, Dipendenze: []Dependency{}}
	if !safeName(id) {
		e.Nota = "identificativo non valido"
		return e
	}
	// Mai nil: una lista nil esce come "null" nel JSON e la pagina va in
	// errore appena prova a scorrerla — stessa cautela di readsMemory().
	if p := findOnDisk(id); p != nil {
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
	if m := helperModel(); m != "" && strings.EqualFold(m, id) {
		e.Dipendenze = append(e.Dipendenze, Dependency{
			Cosa:   "il riquadro di aiuto di questo pannello",
			Perche: "è il modello che risponde alle domande; senza, ripiegherebbe su uno molto più grosso",
			Grave:  true,
		})
	}

	// È caricato in memoria adesso?
	for _, c := range currentMemory().Caricati {
		if strings.EqualFold(c.Nome, id) {
			e.Dipendenze = append(e.Dipendenze, Dependency{
				Cosa:   "è in memoria adesso",
				Perche: "va prima scaricato, altrimenti il programma che lo usa può comportarsi male",
				Grave:  true,
			})
		}
	}

	// Lo cita qualche client?
	for _, cl := range cfg().Clienti {
		f := expandHome(cl.File)
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if strings.Contains(strings.ToLower(string(b)), basso) {
			e.Dipendenze = append(e.Dipendenze, Dependency{
				Cosa:   cl.Nome + " lo ha in configurazione",
				Perche: "dopo la rimozione resterebbe una voce che punta a un modello inesistente",
				Grave:  false,
			})
		}
	}

	// Chi ci punta da fuori: togliere la cartella lo lascia nel vuoto.
	if q := linksTo(e.Posti); len(q) > 0 {
		e.Dipendenze = append(e.Dipendenze, Dependency{
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

func apiExamineModel(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	writeJSON(w, esamina(id))
}

const archiveManifestName = "archivio.json"

func archiveSlug(s string) string {
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

func rollbackMoves(file []archiveFiles, voce string) error {
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
func archivia(e ModelExam, runtime string, configurazioni []Model) (ArchiveEntry, error) {
	radice := expandHome(ModelStore)
	if err := os.MkdirAll(radice, 0o700); err != nil {
		return ArchiveEntry{}, err
	}
	if st, err := os.Lstat(radice); err != nil || st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return ArchiveEntry{}, errors.New("la radice dell'archivio non e' una cartella reale")
	}
	adesso := time.Now()
	base := adesso.Format("20060102-150405.000000000") + "-" + archiveSlug(e.ID)
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
			return ArchiveEntry{}, err
		}
		id = fmt.Sprintf("%s-%d", base, n)
		voce = filepath.Join(radice, id)
	}
	if err := os.Mkdir(filepath.Join(voce, "file"), 0o700); err != nil {
		_ = os.Remove(voce)
		return ArchiveEntry{}, err
	}

	man := archiveManifest{Versione: 1, ID: id, Model: e.ID, Runtime: runtime,
		Creato: adesso, GB: e.GBTotali, File: []archiveFiles{}, Configurazioni: configurazioni}
	for i, p := range e.Posti {
		nome := fmt.Sprintf("file/%02d-%s", i+1, filepath.Base(p.Percorso))
		man.File = append(man.File, archiveFiles{Originale: p.Percorso, Archiviato: nome})
	}
	// Il manifesto nasce prima degli spostamenti: se il processo viene chiuso a
	// meta', al prossimo avvio sappiamo ancora quali elementi sono gia' tornati
	// e quali sono rimasti nel deposito.
	b, err := json.MarshalIndent(man, "", "  ")
	if err == nil {
		err = writeAtomic(filepath.Join(voce, archiveManifestName), append(b, '\n'))
	}
	if err != nil {
		_ = os.RemoveAll(voce)
		return ArchiveEntry{}, fmt.Errorf("non riesco a registrare il ripristino: %w", err)
	}

	spostati := []archiveFiles{}
	for _, f := range man.File {
		dest := filepath.Join(voce, filepath.FromSlash(f.Archiviato))
		p := f.Originale
		if _, err := os.Lstat(p); err != nil {
			if rb := rollbackMoves(spostati, voce); rb == nil {
				_ = os.RemoveAll(voce)
				return ArchiveEntry{}, fmt.Errorf("%s non e' piu' disponibile: %w", p, err)
			}
			return ArchiveEntry{}, fmt.Errorf("%s non e' piu' disponibile e il rollback e' incompleto; non cancello il deposito %s", p, voce)
		}
		if err := os.Rename(p, dest); err != nil {
			if rb := rollbackMoves(spostati, voce); rb == nil {
				_ = os.RemoveAll(voce)
				return ArchiveEntry{}, fmt.Errorf("spostamento fallito per %s: %w", p, err)
			}
			return ArchiveEntry{}, fmt.Errorf("spostamento fallito e rollback incompleto; non cancello il deposito %s", voce)
		}
		spostati = append(spostati, f)
	}
	return ArchiveEntry{
		ID: id, Model: e.ID, GB: e.GBTotali, Creato: adesso.Format(time.RFC3339),
		Posti: len(man.File), Runtime: runtime, Ripristinabile: true,
	}, nil
}

func readManifest(voce string) (archiveManifest, error) {
	b, err := os.ReadFile(filepath.Join(voce, archiveManifestName))
	if err != nil {
		return archiveManifest{}, err
	}
	var m archiveManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return archiveManifest{}, err
	}
	if m.Versione != 1 || m.ID == "" || len(m.File) == 0 {
		return archiveManifest{}, errors.New("manifesto incompleto")
	}
	return m, nil
}

// archivePath accetta soltanto una voce diretta nuova oppure
// data/nome per gli archivi creati dalla versione precedente. Non segue un
// collegamento simbolico usato per far uscire una cancellazione dal deposito.
func archivePath(id string) (string, error) {
	if id == "" || filepath.IsAbs(id) || strings.Contains(id, "..") || strings.ContainsAny(id, "\x00\n\r") {
		return "", errors.New("voce di archivio non valida")
	}
	parti := strings.Split(filepath.ToSlash(id), "/")
	if len(parti) > 2 {
		return "", errors.New("voce di archivio non valida")
	}
	radice := filepath.Clean(expandHome(ModelStore))
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

func archiveList() []ArchiveEntry {
	radice := expandHome(ModelStore)
	voci, err := os.ReadDir(radice)
	if err != nil {
		return []ArchiveEntry{}
	}
	out := []ArchiveEntry{}
	for _, v := range voci {
		if !v.IsDir() || strings.HasPrefix(v.Name(), ".") {
			continue
		}
		p := filepath.Join(radice, v.Name())
		if m, err := readManifest(p); err == nil {
			out = append(out, ArchiveEntry{ID: v.Name(), Model: m.Model, GB: m.GB,
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
			misura := examineFolder(q)
			out = append(out, ArchiveEntry{
				ID: v.Name() + "/" + f.Name(), Model: f.Name(), GB: misura.GB,
				Creato: v.Name(), Posti: 1, Ripristinabile: false, ArchivioVecchio: true,
				Nota: "archiviato da una versione precedente: il percorso originale non era registrato",
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Creato > out[j].Creato })
	return out
}

func restoreArchived(id string) (archiveManifest, error) {
	voce, err := archivePath(id)
	if err != nil {
		return archiveManifest{}, err
	}
	m, err := readManifest(voce)
	if err != nil || m.ID != id {
		return archiveManifest{}, errors.New("questo vecchio archivio non contiene i dati necessari al ripristino automatico")
	}
	// Tutti i conflitti si controllano prima di muovere il primo byte.
	for _, f := range m.File {
		if !pathInModelRoot(f.Originale) {
			return archiveManifest{}, fmt.Errorf("il percorso originale non e' piu' fra le radici dei modelli: %s", f.Originale)
		}
		_, errOriginale := os.Lstat(f.Originale)
		originaleEsiste := errOriginale == nil
		if errOriginale != nil && !os.IsNotExist(errOriginale) {
			return archiveManifest{}, errOriginale
		}
		src := filepath.Join(voce, filepath.FromSlash(f.Archiviato))
		rel, err := filepath.Rel(voce, src)
		if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return archiveManifest{}, errors.New("manifesto con percorso non valido")
		}
		_, errArchivio := os.Lstat(src)
		archivioEsiste := errArchivio == nil
		if errArchivio != nil && !os.IsNotExist(errArchivio) {
			return archiveManifest{}, errArchivio
		}
		if originaleEsiste && archivioEsiste {
			return archiveManifest{}, fmt.Errorf("non sovrascrivo %s: esiste sia sul disco sia nell'archivio", f.Originale)
		}
		if !originaleEsiste && !archivioEsiste {
			return archiveManifest{}, fmt.Errorf("manca %s sia dal disco sia dall'archivio", f.Archiviato)
		}
	}
	spostati := []archiveFiles{}
	for _, f := range m.File {
		src := filepath.Join(voce, filepath.FromSlash(f.Archiviato))
		if _, err := os.Lstat(src); os.IsNotExist(err) {
			continue // era gia' tornato durante un rollback interrotto
		}
		if err := os.MkdirAll(filepath.Dir(f.Originale), 0o755); err != nil {
			rollbackRestore(spostati, voce)
			return archiveManifest{}, err
		}
		if err := os.Rename(src, f.Originale); err != nil {
			rollbackRestore(spostati, voce)
			return archiveManifest{}, fmt.Errorf("ripristino fallito: %w", err)
		}
		spostati = append(spostati, f)
	}
	if err := os.RemoveAll(voce); err != nil {
		return archiveManifest{}, fmt.Errorf("modello ripristinato, ma non riesco a pulire il manifesto: %w", err)
	}
	return m, nil
}

func pathInModelRoot(p string) bool {
	p = filepath.Clean(p)
	for _, r := range radici() {
		radice := filepath.Clean(expandHome(r))
		rel, err := filepath.Rel(radice, p)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func rollbackRestore(spostati []archiveFiles, voce string) {
	for i := len(spostati) - 1; i >= 0; i-- {
		f := spostati[i]
		_ = os.MkdirAll(filepath.Dir(filepath.Join(voce, filepath.FromSlash(f.Archiviato))), 0o700)
		_ = os.Rename(f.Originale, filepath.Join(voce, filepath.FromSlash(f.Archiviato)))
	}
}

func sameConfiguredModel(m Model, runtime, id string) bool {
	if !strings.EqualFold(m.ID, id) {
		return false
	}
	return runtime == "" || strings.EqualFold(m.Runtime, runtime)
}

func withoutConfiguredModel(in []Model, runtime, id string) (restanti, associate []Model) {
	for _, m := range in {
		if sameConfiguredModel(m, runtime, id) {
			if m.InPi || m.InOC {
				associate = append(associate, m)
			}
			continue
		}
		restanti = append(restanti, m)
	}
	return restanti, associate
}

func mergeConfiguredModels(in, daRipristinare []Model) []Model {
	out := append([]Model{}, in...)
	for _, torna := range daRipristinare {
		trovato := false
		for i := range out {
			if sameConfiguredModel(out[i], torna.Runtime, torna.ID) {
				// Il manifesto conserva esattamente in quali client compariva.
				out[i].InPi = out[i].InPi || torna.InPi
				out[i].InOC = out[i].InOC || torna.InOC
				if out[i].Nome == "" {
					out[i].Nome = torna.Nome
				}
				trovato = true
				break
			}
		}
		if !trovato {
			out = append(out, torna)
		}
	}
	return out
}

// apiRemoveModel archivia il modello. La cancellazione definitiva e'
// deliberatamente separata e puo' agire soltanto su una voce gia' nel deposito.
func apiRemoveModel(w http.ResponseWriter, r *http.Request) {
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
	configurate, erroriConfig := configState()
	if len(erroriConfig) > 0 {
		errJSON(w, "prima sistema le configurazioni: "+strings.Join(erroriConfig, "; "))
		return
	}
	restanti, associate := withoutConfiguredModel(configurate, req.Runtime, req.ID)
	voce, err := archivia(e, req.Runtime, associate)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	// Archivio e menu dei client sono una sola operazione: niente voci fantasma
	// in Pi/OpenCode. Se la scrittura fallisce, rimettiamo subito i file dov'erano.
	if err := writeConfig(restanti); err != nil {
		if _, rollbackErr := restoreArchived(voce.ID); rollbackErr != nil {
			errJSON(w, "configurazione non aggiornata: "+err.Error()+"; anche il ripristino dei file e' incompleto: "+rollbackErr.Error())
			return
		}
		errJSON(w, "non ho archiviato nulla: non riesco ad aggiornare Pi e OpenCode: "+err.Error())
		return
	}
	refreshMemory()
	writeJSON(w, map[string]any{
		"ok": true, "gb": voce.GB, "archivio": voce,
		"comeTornareIndietro": "apri Modelli archiviati e premi Ripristina",
	})
}

func apiModelArchive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		errJSONStatus(w, http.StatusMethodNotAllowed, "serve GET")
		return
	}
	writeJSON(w, archiveList())
}

func apiRestoreModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		errJSON(w, "corpo non leggibile")
		return
	}
	m, err := restoreArchived(req.ID)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	configurate, erroriConfig := configState()
	if len(erroriConfig) > 0 {
		errJSON(w, "file ripristinati, ma le configurazioni non sono leggibili: "+strings.Join(erroriConfig, "; "))
		return
	}
	if err := writeConfig(mergeConfiguredModels(configurate, m.Configurazioni)); err != nil {
		errJSON(w, "file ripristinati, ma non riesco a rimettere il modello in Pi e OpenCode: "+err.Error())
		return
	}
	refreshMemory()
	writeJSON(w, map[string]any{"ok": true, "modello": m.Model, "runtime": m.Runtime,
		"gb": m.GB, "configurazioni": m.Configurazioni})
}

func apiDeleteArchived(w http.ResponseWriter, r *http.Request) {
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
	p, err := archivePath(req.ID)
	if err != nil {
		errJSON(w, err.Error())
		return
	}
	misura := examineFolder(p).GB
	if err := os.RemoveAll(p); err != nil {
		errJSON(w, "cancellazione fallita: "+err.Error())
		return
	}
	// Se era una voce del vecchio archivio, prova a togliere la cartella-data
	// ormai vuota. os.Remove non tocca cartelle non vuote.
	if strings.Contains(filepath.ToSlash(req.ID), "/") {
		_ = os.Remove(filepath.Dir(p))
	}
	writeJSON(w, map[string]any{"ok": true, "gb": misura, "id": req.ID})
}
