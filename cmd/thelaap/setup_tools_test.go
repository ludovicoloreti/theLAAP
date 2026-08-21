package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectoryDegliStrumentiEConfigurabile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "aistack.py")
	if err := os.WriteFile(file, []byte("#!/usr/bin/env python3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("THELAAP_TOOLS_DIR", dir)
	if got := detectedToolPath("aistack.py"); got != file {
		t.Fatalf("strumento trovato in %q, atteso %q", got, file)
	}
}

func TestDirectoryDegliStrumentiNonIndovinaPercorsiPersonali(t *testing.T) {
	t.Setenv("THELAAP_TOOLS_DIR", t.TempDir())
	if got := detectedToolPath("strumento-che-non-esiste-thelaap"); got != "" {
		t.Fatalf("strumento inesistente trovato in %q", got)
	}
}

func TestSorgentiPubbliciNonContengonoIdentificatoriOPercorsiPersonali(t *testing.T) {
	for _, file := range []string{
		"../../build.sh", "../../menubar/theLAAP.swift", "setup.go",
		"../../README.md", "../../README.it.md", "../../start-server.command",
		"../../go.mod", "../../LICENSE", "memory.go", "runtimes.go", "states.go",
	} {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		testo := string(b)
		for _, vietato := range []string{
			"com." + "llo" + "reti", "ludo" + "vico", "lo" + "reti",
			"~/" + "Desktop", "Desktop" + "/AI", "/" + "Users/", "/home" + "/",
		} {
			if strings.Contains(testo, vietato) {
				t.Errorf("%s contiene ancora %q", file, vietato)
			}
		}
	}
}
