package main

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Footprint: quanto pesa un processo in memoria fisica.
//
// Stimato distingue una misura da un ripiego meno preciso. L'interfaccia deve
// mostrare la differenza: presentare una stima come misura è il modo in cui si
// prendono decisioni sbagliate con la coscienza a posto.
type Footprint struct {
	CorrenteByte uint64 `json:"correnteByte"`
	PiccoByte    uint64 `json:"piccoByte"`
	Stimato      bool   `json:"stimato"`
}

func (o Footprint) CurrentGB() float64 { return float64(o.CorrenteByte) / 1e9 }
func (o Footprint) PeakGB() float64    { return float64(o.PiccoByte) / 1e9 }

// PesoDaPrevedere: su quale numero decidere se un modello ci sta.
//
// Il picco, non il valore corrente. Un processo che ora occupa 59 GB ma ne ha
// toccati 79 tornerà a 79 appena lo si usa sul serio: ammetterlo sulla base
// dei 59 significa autorizzare un sovraccarico che si manifesta dopo, quando
// è tardi. Misurato su questa macchina: mtplx è passato da 30 GB appena
// avviato a 71 GB in una sessione, con picco 79.
func (o Footprint) ExpectedWeightBytes() uint64 {
	if o.PiccoByte > o.CorrenteByte {
		return o.PiccoByte
	}
	return o.CorrenteByte
}

// cmdErr è cmdT che dice se è andata male.
//
// cmdT restituisce "" sia quando il comando fallisce sia quando non stampa
// niente, e le due cose non sono la stessa: senza distinguerle un runtime
// spento e un comando rotto sono indistinguibili, e nessuno se ne accorge.
// L'output torna anche quando il comando fallisce: è lì che i programmi
// scrivono il motivo. Scartarlo lascia all'utente un "exit status 1" al posto
// di "model 'pippo' not found".
func cmdErr(limite time.Duration, nome string, args ...string) (string, error) {
	ctx, annulla := context.WithTimeout(context.Background(), limite)
	defer annulla()
	out, err := exec.CommandContext(ctx, nome, args...).Output()
	testo := strings.TrimSpace(string(out))
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return testo, fmt.Errorf("%s non ha risposto entro %s", nome, limite)
		}
		return testo, err
	}
	return testo, nil
}

func shErr(limite time.Duration, linea string) (string, error) {
	return cmdErr(limite, "/bin/sh", "-c", linea)
}

// I programmi da terminale colorano l'output e muovono il cursore anche quando
// scrivono su una pipe (ollama lo fa). Quelle sequenze, dentro un messaggio
// d'errore mostrato in una pagina web, diventano caratteri incomprensibili.
var reAnsi = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func withoutAnsi(s string) string {
	return strings.TrimSpace(reAnsi.ReplaceAllString(s, ""))
}
