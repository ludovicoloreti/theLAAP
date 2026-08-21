#!/bin/bash
# build.sh — builds theLAAP (The Local AI Admin Panel).
#
#   ./build.sh              binaries only
#   ./build.sh --app        creates theLAAP.app next to this file
#   ./build.sh --install    puts it in /Applications
#   ./build.sh --desktop    puts it on the Desktop
#
# The app carries two programs:
#   aipanel   the panel server (Go)
#   theLAAP   the menu bar item (Swift), which is what actually launches
#
# The ad-hoc codesign is NOT optional: unsigned, the Go binary runs from a
# terminal but, launched by launchd, hangs inside dyld without saying anything.

set -euo pipefail
cd "$(dirname "$0")"
QUI="$(pwd)"
PORTA=7070
BUNDLE_ID="${THELAAP_BUNDLE_ID:-app.thelaap.panel}"
export PATH=/usr/bin:/bin:/usr/sbin:/sbin:/opt/homebrew/bin:$PATH

echo "> building the panel (Go)"
go build -o aipanel ./cmd/thelaap
codesign -s - --force ./aipanel >/dev/null 2>&1
echo "  aipanel ok ($(du -h aipanel | cut -f1))"

echo "> building the menu bar item (Swift)"
if swiftc -O menubar/theLAAP.swift -o menubar/theLAAP -framework Cocoa -framework WebKit 2>/dev/null; then
  echo "  theLAAP ok ($(du -h menubar/theLAAP | cut -f1))"
  BARRA=1
else
  echo "  Swift toolchain missing: the app will carry only the panel"
  BARRA=0
fi

[[ "${1:-}" == "" ]] && { echo; echo "Run it with: ./aipanel   (or ./build.sh --install)"; exit 0; }

DEST="$QUI"
case "${1:-}" in
  --install) DEST="/Applications" ;;
  --desktop)
    DEST="$(osascript -e 'POSIX path of (path to desktop folder)' 2>/dev/null)"
    DEST="${DEST%/}"
    ;;
esac
APP="$DEST/theLAAP.app"

echo "> assembling theLAAP.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

# The activation policy is set at runtime, not here: see note 2 in the README.
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>theLAAP</string>
	<key>CFBundleDisplayName</key><string>theLAAP</string>
	<!-- Without these two, the menu entries AppKit adds by itself (Undo, Redo,
	     Enter Full Screen, Close All, Emoji and Symbols) come out in english in
	     the middle of a menu written in italian. -->
	<key>CFBundleDevelopmentRegion</key><string>it</string>
	<key>CFBundleLocalizations</key><array><string>it</string><string>en</string></array>
	<key>CFBundleIdentifier</key><string>$BUNDLE_ID</string>
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
cp start-server.command "$APP/Contents/Resources/start-server.command" 2>/dev/null || true
chmod +x "$APP/Contents/Resources/start-server.command" 2>/dev/null || true
[ "$BARRA" = 1 ] && cp menubar/theLAAP "$APP/Contents/MacOS/theLAAP"

cat > "$APP/Contents/MacOS/avvia" <<AVVIO
#!/bin/bash
# Starts the panel server if it is not already up, then hands over to the menu
# bar item (or opens the panel directly).
QUI="\$(cd "\$(dirname "\$0")" && pwd)"
PORTA=$PORTA
# theLAAP starts the server: an app launched from the Finder has a reduced
# environment, and the child started by this script never got to listen.
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

# icon generated on the fly: no binary files to keep in the repository
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
echo "  $APP ok"
echo
if [ "$BARRA" = 1 ]; then
  echo "Open it: the icon appears in the menu bar. The full panel opens from there."
else
  echo "Open it: the panel comes up on http://127.0.0.1:$PORTA"
fi
