package main

import (
	"regexp"
	"strings"
	"testing"
)

// The page builds handlers as HTML text, so an argument that ends up inside
// onclick="..." has to survive the attribute parser first and the JavaScript
// parser second.
//
// It did not. Writing onclick="scegli('+JSON.stringify(m.id)+')" produced
//
//	onclick="scegli("coder")"
//
// and the browser read the attribute as scegli( , stopping at the second double
// quote, plus a stray attribute named coder")". Ten elements were inert, among
// them every row of the model table and the suggested questions in the help
// panel. Nothing was logged: an unparsable handler is not an error, it is just
// an element that does not answer.
//
// The fix is arg(), which quotes with single quotes and escapes for both
// contexts. These tests are here because the defect leaves no trace at runtime.

var onclickAttribute = regexp.MustCompile(`onclick="[^"]*"`)

func TestNessunJSONDentroUnAttributoOnclick(t *testing.T) {
	for _, a := range onclickAttribute.FindAllString(UI, -1) {
		if strings.Contains(a, "JSON.stringify") {
			t.Errorf("JSON.stringify emette virgolette doppie e chiude l'attributo: %s", a)
		}
	}
}

// Every argument interpolated into an onclick must go through arg(). Catching
// the concatenation itself is what makes the guard useful: a new call site
// written the old way fails here instead of shipping a dead button.
// The handlers that receive a model id, a runtime key or a question: text that
// arrives from the configuration or from the model catalogue, so text that can
// hold a quote, an apostrophe or an accent. Interpolating one of these without
// arg() is the defect above, and these are the sites where it hurts.
var handlersWithData = []string{"scegli(", "ridescrivi(", "prova(", "archivia(",
	"applicaNomi(", "chiedi(", "daFermare:"}

func TestIGestoriConDatiPassanoDaArg(t *testing.T) {
	for _, a := range onclickAttribute.FindAllString(UI, -1) {
		// senza '+ non c'è interpolazione: il gestore legge dal DOM al clic,
		// e lì non passa da questo attributo nessun testo da sfuggire.
		if !strings.Contains(a, "'+") {
			continue
		}
		for _, g := range handlersWithData {
			if strings.Contains(a, g) && !strings.Contains(a, "arg") {
				t.Errorf("%s riceve dati senza arg(): %s", g, a)
			}
		}
	}
}

// arg() has to be there for the two tests above to mean anything, and it has to
// escape both the JavaScript string and the HTML attribute.
func TestArgSfuggeEntrambiIContesti(t *testing.T) {
	i := strings.Index(UI, "const arg =")
	if i < 0 {
		t.Fatal("arg() non c'è più: i due test qui sopra non provano più nulla")
	}
	corpo := UI[i : i+400]
	for _, atteso := range []string{`&quot;`, `\\'`, `&amp;`} {
		if !strings.Contains(corpo, atteso) {
			t.Errorf("arg() non sfugge %s: %s", atteso, corpo[:120])
		}
	}
}
