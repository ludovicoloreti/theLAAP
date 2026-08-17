#!/bin/bash
# build.sh — compila theLAAP (The Local AI Admin Panel).
#
#   ./build.sh              solo i binari
#   ./build.sh --app        crea theLAAP.app qui accanto
#   ./build.sh --install    la mette in /Applications
#   ./build.sh --desktop    la mette sulla Scrivania
#
# L'app contiene due programmi:
#   · aipanel  — il server del pannello web (Go)
#   · theLAAP  — la voce nella barra dei menu (Swift), che è quello che parte
#
# Il codesign ad-hoc NON è facoltativo: senza firma il binario Go parte da
# terminale ma, lanciato da launchd, resta appeso dentro dyld senza dire niente.

set -euo pipefail
cd "$(dirname "$0")"
QUI="$(pwd)"
PORTA=7070
export PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:$PATH

echo "▸ compilo il pannello (Go)"
go build -o aipanel .
codesign -s - --force ./aipanel >/dev/null 2>&1
echo "  ✅ aipanel ($(du -h aipanel | cut -f1))"

echo "▸ compilo la voce nella barra (Swift)"
if swiftc -O menubar/theLAAP.swift -o menubar/theLAAP -framework Cocoa -framework WebKit 2>/dev/null; then
  echo "  ✅ theLAAP ($(du -h menubar/theLAAP | cut -f1))"
  BARRA=1
else
  echo "  ⚠️  Swift non disponibile: l'app avrà solo il pannello"
  BARRA=0
fi

[[ "${1:-}" == "" ]] && { echo; echo "Avvia con: ./aipanel   (oppure ./build.sh --install)"; exit 0; }

DEST="$QUI"
case "${1:-}" in
  --install) DEST="/Applications" ;;
  --desktop) DEST="$HOME/Desktop" ;;
esac
APP="$DEST/theLAAP.app"

echo "▸ preparo theLAAP.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# LSUIElement=true → vive nella barra dei menu, senza icona nel Dock
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>theLAAP</string>
	<key>CFBundleDisplayName</key><string>theLAAP</string>
	<!-- Senza queste due, le voci di menu che AppKit aggiunge da sé (Annulla,
	     Ripristina, Schermo intero, Chiudi tutto, Emoji e simboli) escono in
	     inglese in mezzo a un menu scritto in italiano. -->
	<key>CFBundleDevelopmentRegion</key><string>it</string>
	<key>CFBundleLocalizations</key><array><string>it</string><string>en</string></array>
	<key>CFBundleIdentifier</key><string>com.lloreti.thelaap</string>
	<key>CFBundleVersion</key><string>1.0</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleExecutable</key><string>$([ "$BARRA" = 1 ] && echo theLAAP || echo avvia)</string>
	<key>CFBundleIconFile</key><string>icona</string>
	<key>LSMinimumSystemVersion</key><string>13.0</string>
	<key>LSUIElement</key><false/>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

cp aipanel "$APP/Contents/MacOS/aipanel"
cp avvia-server.command "$APP/Contents/Resources/avvia-server.command" 2>/dev/null || true
chmod +x "$APP/Contents/Resources/avvia-server.command" 2>/dev/null || true
[ "$BARRA" = 1 ] && cp menubar/theLAAP "$APP/Contents/MacOS/theLAAP"

cat > "$APP/Contents/MacOS/avvia" <<AVVIO
#!/bin/bash
# Avvia il server del pannello se non c'è già, poi passa il comando
# alla voce nella barra dei menu (o apre direttamente il pannello).
QUI="\$(cd "\$(dirname "\$0")" && pwd)"
PORTA=$PORTA
# Il server lo avvia theLAAP: un'app lanciata dal Finder ha un ambiente ridotto
# e il figlio avviato da questo script non arrivava mai ad ascoltare.
if [ -x "\$QUI/theLAAP" ]; then
  exec "\$QUI/theLAAP"
fi
if ! curl -s -m 2 "http://127.0.0.1:\$PORTA/api/runtime" >/dev/null 2>&1; then
  "\$QUI/aipanel" >/tmp/aipanel.log 2>&1 &
  sleep 3
fi
for B in "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \\
         "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge" \\
         "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"; do
  [ -x "\$B" ] && exec "\$B" --app="http://127.0.0.1:\$PORTA" --window-size=1280,900
done
exec open "http://127.0.0.1:\$PORTA"
AVVIO
chmod +x "$APP/Contents/MacOS/avvia"

# icona generata al volo: niente file binari da tenere nel progetto
ICO=$(mktemp -d)/icona.iconset
mkdir -p "$ICO"
python3 - "$ICO" <<'PY' 2>/dev/null || true
import subprocess, sys, os
d = sys.argv[1]
svg = '''<svg xmlns="http://www.w3.org/2000/svg" width="1024" height="1024">
<rect width="1024" height="1024" rx="220" fill="#171b21"/>
<rect x="150" y="330" width="724" height="118" rx="24" fill="#7dd3fc"/>
<rect x="150" y="500" width="470" height="118" rx="24" fill="#c4b5fd"/>
<rect x="150" y="670" width="280" height="118" rx="24" fill="#4ade80"/>
<circle cx="812" cy="559" r="44" fill="#fbbf24"/></svg>'''
p = os.path.join(d, "base.svg"); open(p, "w").write(svg)
for s in (16, 32, 64, 128, 256, 512, 1024):
    subprocess.run(["qlmanage", "-t", "-s", str(s), "-o", d, p], capture_output=True)
    src = os.path.join(d, "base.svg.png")
    if os.path.exists(src):
        os.rename(src, os.path.join(d, f"icon_{s}x{s}.png"))
PY
ls "$ICO"/*.png >/dev/null 2>&1 && iconutil -c icns "$ICO" -o "$APP/Contents/Resources/icona.icns" 2>/dev/null || true

codesign -s - --force --deep "$APP" >/dev/null 2>&1 || true
echo "  ✅ $APP"
echo
if [ "$BARRA" = 1 ]; then
  echo "Aprila: compare l'icona nella barra in alto. Da lì apri il pannello completo."
else
  echo "Aprila: si apre il pannello su http://127.0.0.1:$PORTA"
fi
