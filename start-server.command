#!/bin/bash
# Starts theLAAP: the server first, then the app.
#
# This is a fallback. Double clicking the app is enough now that the local data
# no longer lives under ~/Desktop: before that the server start blocked inside
# open() because of TCC, and the ad-hoc signature took the blame. See note 3 in
# the README.
BIN="/Applications/theLAAP.app/Contents/MacOS/aipanel"
[ -x "$BIN" ] || BIN="$(cd "$(dirname "$0")" && pwd)/aipanel"

echo "> theLAAP"
if curl -s -m 2 http://127.0.0.1:7070/api/runtime >/dev/null 2>&1; then
  echo "  the panel was already running"
else
  echo "  starting the panel..."
  nohup "$BIN" >/tmp/aipanel.log 2>&1 &
  for _ in $(seq 1 40); do
    curl -s -m 1 http://127.0.0.1:7070/api/runtime >/dev/null 2>&1 && break
    sleep .25
  done
fi

if curl -s -m 2 http://127.0.0.1:7070/api/runtime >/dev/null 2>&1; then
  echo "  panel up"
  open -a theLAAP
  echo "  window open"
  echo
  echo "You can close this Terminal window."
  sleep 2
  osascript -e 'tell application "Terminal" to close (every window whose name contains "start-server")' 2>/dev/null &
else
  echo "  it did not start. Look at /tmp/aipanel.log:"
  tail -5 /tmp/aipanel.log 2>/dev/null
  echo; echo "Press return to close."; read
fi
