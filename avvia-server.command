#!/bin/bash
# Avvia theLAAP: prima il server, poi l'app.
#
# Serve come ripiego. Il doppio click sull'app basta, da quando i dati locali non
# stanno piu sotto ~/Desktop: prima l'avvio del server si bloccava dentro open()
# per TCC, e si dava la colpa alla firma ad-hoc. Vedi la nota 3 del README.
BIN="/Applications/theLAAP.app/Contents/MacOS/aipanel"
[ -x "$BIN" ] || BIN="$(cd "$(dirname "$0")" && pwd)/aipanel"

echo "▸ theLAAP"
if curl -s -m 2 http://127.0.0.1:7070/api/runtime >/dev/null 2>&1; then
  echo "  il pannello era già acceso"
else
  echo "  accendo il pannello…"
  nohup "$BIN" >/tmp/aipanel.log 2>&1 &
  for _ in $(seq 1 40); do
    curl -s -m 1 http://127.0.0.1:7070/api/runtime >/dev/null 2>&1 && break
    sleep .25
  done
fi

if curl -s -m 2 http://127.0.0.1:7070/api/runtime >/dev/null 2>&1; then
  echo "  ✅ pannello acceso"
  open -a theLAAP
  echo "  ✅ finestra aperta"
  echo
  echo "Puoi chiudere questa finestra del Terminale."
  sleep 2
  osascript -e 'tell application "Terminal" to close (every window whose name contains "Avvia theLAAP")' 2>/dev/null &
else
  echo "  ❌ non parte. Guarda /tmp/aipanel.log:"
  tail -5 /tmp/aipanel.log 2>/dev/null
  echo; echo "Premi invio per chiudere."; read
fi
