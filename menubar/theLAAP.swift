// theLAAP — voce nella barra dei menu del Mac.
//
// Non duplica il pannello: mostra a colpo d'occhio cosa sta girando e quanta
// memoria è occupata, e permette le due o tre cose che si fanno di corsa
// (aprire il pannello, riavviare un servizio, attivare il modello grande).
// Tutto il resto lo fa il pannello, che questa voce si limita ad aprire.
//
// Compilazione: swiftc -O theLAAP.swift -o theLAAP -framework Cocoa

import Cocoa
import WebKit

let PORTA = 7070
let BASE  = "http://127.0.0.1:\(PORTA)"

// MARK: - la finestra del pannello
//
// Senza una finestra, aprendo l'app "non succede niente": resta solo un'icona
// nella barra che è facilissimo non notare. Quindi l'app apre subito il pannello
// in una finestra propria, e l'icona nella barra resta come scorciatoia.

final class Finestra: NSWindowController, WKNavigationDelegate, WKScriptMessageHandler, NSWindowDelegate {
    let web = WKWebView()
    weak var barra: Barra?

    /// Chiusa la finestra si torna `.accessory`: l'app resta nella barra di
    /// stato e sparisce dal Dock, che è il suo posto quando non c'è niente da
    /// guardare. Senza questo resterebbe un'icona nel Dock senza finestre.
    func windowWillClose(_ n: Notification) {
        NSApp.setActivationPolicy(.accessory)
    }

    // ── quello che le voci di menu chiedono alla pagina ──
    func zoom(_ d: CGFloat) { web.pageZoom = max(0.5, min(2.0, web.pageZoom + d)) }
    func zoomNormale() { web.pageZoom = 1 }
    /// Il menu parla alla pagina con le sue stesse funzioni: nessuna logica
    /// duplicata in Swift, solo la scorciatoia resa visibile e di sistema.
    func chiama(_ js: String) { web.evaluateJavaScript(js, completionHandler: nil) }

    /// Schermata di attesa: il server ci mette qualche secondo e la pagina bianca
    /// fa pensare che sia tutto rotto.
    func mostraCaricamento(_ testo: String = "Accendo il pannello…") {
        let html = """
        <html><head><meta charset="utf-8"><style>
        body{background:#0f1216;color:#e8edf3;font:15px/1.6 -apple-system,system-ui;
             display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
        .c{text-align:center}
        .r{width:34px;height:34px;border:3px solid #262c35;border-top-color:#7dd3fc;
           border-radius:50%;margin:0 auto 16px;animation:g .9s linear infinite}
        @keyframes g{to{transform:rotate(360deg)}}
        @media(prefers-reduced-motion:reduce){.r{animation:none}}
        p{color:#9aa5b1;margin:6px 0 0;font-size:13.5px}
        </style></head><body><div class="c">
        <div class="r"></div><strong>\(testo)</strong>
        <p>ci vuole qualche secondo</p></div></body></html>
        """
        web.loadHTMLString(html, baseURL: nil)
    }

    func userContentController(_ c: WKUserContentController, didReceive m: WKScriptMessage) {
        // Il pulsante "Avvia il pannello" della schermata d'errore.
        mostraCaricamento("Avvio in corso…")
        barra?.avviaServerSeSpento(daFinestra: true)
        DispatchQueue.main.asyncAfter(deadline: .now() + 3) { self.ricarica() }
    }

    convenience init() {
        let f = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 1180, height: 860),
                         styleMask: [.titled, .closable, .miniaturizable, .resizable],
                         backing: .buffered, defer: false)
        f.title = "theLAAP"
        // Chiudendo la finestra l'app resta viva nella barra di stato, quindi la
        // finestra va riusata: senza questo AppKit la libera alla chiusura e la
        // riapertura lavorerebbe su un oggetto già morto.
        f.isReleasedWhenClosed = false
        f.center()
        f.setFrameAutosaveName("theLAAP")
        f.minSize = NSSize(width: 720, height: 520)
        self.init(window: f)
        f.delegate = self
        web.frame = f.contentView!.bounds
        web.autoresizingMask = [.width, .height]
        web.navigationDelegate = self
        web.configuration.userContentController.add(self, name: "avvia")
        f.contentView?.addSubview(web)
        // Il titolo della finestra lo dice la pagina, non questo file: cablarlo
        // qui vuol dire che cambia la pagina e la barra del titolo continua a
        // raccontare la versione di prima — è già successo. E così segue anche
        // la lingua scelta dentro il pannello.
        osservaTitolo = web.observe(\.title, options: [.new]) { [weak f] w, _ in
            guard let t = w.title?.trimmingCharacters(in: .whitespaces), !t.isEmpty else { return }
            f?.title = t
        }
    }

    /// Va tenuto vivo: se il token muore, l'osservazione smette.
    private var osservaTitolo: NSKeyValueObservation?

    /// Se il frame ricordato cade fuori da ogni schermo, la finestra si apre dove
    /// non si vede e l'app sembra non essere partita: niente da cliccare, niente
    /// da capire. Succede scollegando un monitor, e succede se il frame salvato
    /// va storto — visto qui, `y = -1010`, la finestra interamente sopra il bordo
    /// superiore. `center()` nell'init non basta: il frame salvato viene
    /// applicato dopo. Va ricontrollato ogni volta che si mostra.
    private func riportaInVista() {
        guard let w = window else { return }
        let visibile = NSScreen.screens.contains { $0.visibleFrame.intersects(w.frame) }
        if !visibile { w.center() }
    }

    func mostra() {
        // Con la finestra aperta l'app diventa `.regular`, e solo così macOS
        // disegna la barra dei menu in alto: un'app `.accessory` non ce l'ha, e
        // il mainMenu serviva soltanto a instradare le scorciatoie.
        //
        // La voce nella barra di stato non si perde: è già stata creata in
        // applicationDidFinishLaunching mentre la politica era `.accessory`, e
        // cambiarla dopo non la rimuove. È diverso dal partire `.regular`, che
        // è il caso in cui non compariva.
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        showWindow(nil)
        window?.makeKeyAndOrderFront(nil)
        // DOPO showWindow, non prima: è showWindow a riapplicare il frame
        // salvato, quindi un controllo anticipato guarda la posizione sbagliata.
        // Il secondo giro copre chi lo sposta ancora dopo di noi.
        riportaInVista()
        DispatchQueue.main.async { self.riportaInVista() }
        mostraCaricamento()
        DispatchQueue.global().async {
            let acceso = URLSession.pingaSincrono(BASE + "/api/runtime", 2)
            DispatchQueue.main.async { self.ricarica() }
            _ = acceso
        }
    }

    func ricarica() {
        web.load(URLRequest(url: URL(string: BASE)!))
    }

    /// I link ai repository non devono sostituire il pannello dentro la sua
    /// WKWebView. Li apre il browser predefinito e la finestra resta dov'era.
    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction,
                 decisionHandler: @escaping (WKNavigationActionPolicy) -> Void) {
        guard let url = navigationAction.request.url else {
            decisionHandler(.cancel)
            return
        }
        let host = (url.host ?? "").lowercased()
        if url.scheme == "http" && (host == "127.0.0.1" || host == "localhost") {
            decisionHandler(.allow)
            return
        }
        if navigationAction.navigationType == .linkActivated,
           url.scheme == "https", host == "huggingface.co" {
            NSWorkspace.shared.open(url)
            decisionHandler(.cancel)
            return
        }
        decisionHandler(.cancel)
    }

    // Se il server non risponde, invece della pagina bianca del browser
    // spieghiamo cosa fare, in italiano.
    func webView(_ w: WKWebView, didFailProvisionalNavigation n: WKNavigation!, withError e: Error) {
        // Una navigazione ANNULLATA non è un errore: è quello che succede quando
        // ne parte una seconda mentre la prima è in volo, e qui capita di
        // proposito (mostra() ricarica, e apriPannello ricarica di nuovo appena
        // il server risponde). Trattandola come un guasto, il pannello diceva
        // «non è acceso» con il server che rispondeva 200 — visto chiudendo e
        // riaprendo la finestra.
        if (e as NSError).code == NSURLErrorCancelled { return }
        let html = """
        <html><head><meta charset="utf-8"><style>
        body{background:#0f1216;color:#e8edf3;font:15px/1.6 -apple-system,system-ui;
             display:flex;align-items:center;justify-content:center;height:100vh;margin:0}
        .b{max-width:44ch;padding:30px}
        h2{font-size:19px;margin:0 0 10px}
        p{color:#9aa5b1;margin:0 0 14px}
        code{background:#0b0e12;border:1px solid #262c35;border-radius:5px;padding:2px 6px;font-size:13px}
        button{background:#166534;border:1px solid #22c55e;color:#4ade80;font:inherit;font-weight:600;
               border-radius:8px;padding:10px 18px;cursor:pointer}
        </style></head><body><div class="b">
        <h2>Il pannello non è acceso</h2>
        <p>Il programma che lo fa funzionare non è partito. Premi qui sotto: si apre
           una finestra di Terminale che lo avvia, poi puoi chiuderla.</p>
        <button onclick="window.webkit.messageHandlers.avvia.postMessage('')">Avvia il pannello</button>
        <p style="margin-top:18px;font-size:13px">In alternativa, doppio click su
           <code>start-server.command</code> nella cartella del progetto.</p>
        </div></body></html>
        """
        w.loadHTMLString(html, baseURL: nil)
    }
}

// MARK: - dati letti dal server Go

struct Caricato: Decodable { let nome: String; let runtime: String; let gb: Double }
/// Quanto tiene davvero un programma acceso, misurato sul processo.
/// `caricati` dice quali modelli ci sono e quanto pesano i loro FILE; questo
/// dice quanta memoria è occupata. Le due cose differiscono parecchio — mtplx
/// dichiara 29,3 GB e ne occupa 33,3 — e il pannello disegna queste. Sommare i
/// file qui vorrebbe dire scrivere nella barra un numero diverso da quello che
/// si legge nel pannello, per la stessa domanda.
struct Processo: Decodable { let chiave: String; let nome: String; let correnteByte: UInt64 }
struct Memoria: Decodable {
    let totaleGB: Double, liberaGB: Double, ceilingGB: Double
    let caricati: [Caricato]
    let processi: [Processo]?
    let avvisi: [String]?

    /// I GB occupati adesso: la stessa somma che fa il pannello.
    var occupatiGB: Double {
        (processi ?? []).reduce(0) { $0 + Double($1.correnteByte) / 1e9 }
    }
}
struct Runtime: Decodable { let chiave: String, nome: String; let porta: Int; let attivo: Bool }

/// Una voce del registro dei comandi, da /api/comandi.
///
/// Il menu non conosce nessun id: li chiede al server, che li ricava dalla
/// configurazione. Prima erano scritti qui a mano, e il 16/08/2026 si e scoperto
/// che tre non esistevano piu: le voci non facevano niente, in silenzio. Con una
/// fonte sola quel difetto non si puo riprodurre.
struct Comando: Decodable {
    let id: String
    let nome: String
    let gruppo: String
    let cosa: String?
    let durata: String?
    let rischio: Bool?
    let rotta: String
    let corpo: [String: String]?
}

func chiedi<T: Decodable>(_ percorso: String, _ tipo: T.Type, _ poi: @escaping (T?) -> Void) {
    guard let url = URL(string: BASE + percorso) else { return poi(nil) }
    var req = URLRequest(url: url)
    req.timeoutInterval = 4
    URLSession.shared.dataTask(with: req) { dati, _, _ in
        guard let dati, let v = try? JSONDecoder().decode(tipo, from: dati) else { return poi(nil) }
        poi(v)
    }.resume()
}

/// Piccolo aiuto: una chiamata sincrona per sapere se il server risponde.
extension URLSession {
    static func pingaSincrono(_ url: String, _ attesa: TimeInterval = 1.2) -> Bool {
        guard let u = URL(string: url) else { return false }
        var req = URLRequest(url: u); req.timeoutInterval = attesa
        let sem = DispatchSemaphore(value: 0)
        var ok = false
        URLSession.shared.dataTask(with: req) { d, r, _ in
            ok = (r as? HTTPURLResponse)?.statusCode == 200 && d != nil
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + attesa + 0.3)
        return ok
    }
}

@discardableResult
func esegui(_ riga: String) -> String {
    let p = Process()
    p.launchPath = "/bin/sh"
    p.arguments = ["-c", riga]
    let tubo = Pipe()
    p.standardOutput = tubo; p.standardError = tubo
    try? p.run()
    let d = tubo.fileHandleForReading.readDataToEndOfFile()
    p.waitUntilExit()
    return String(data: d, encoding: .utf8) ?? ""
}

// MARK: - la voce nella barra

final class Barra: NSObject, NSApplicationDelegate {
    // Creato dentro applicationDidFinishLaunching: se lo si costruisce prima che
    // l'app sia inizializzata, la voce non compare nella barra.
    var voce: NSStatusItem!
    let menu = NSMenu()
    var timer: Timer?
    var serverVivo = false
    // tenuti in vita apposta: se ARC li libera, il server figlio si blocca
    var processoServer: Process?
    var logServer: FileHandle?
    var finestra: Finestra?
    // Una guardia: l'avvio parte sia da applicationDidFinishLaunching sia da
    // apriPannello, e due tentativi in parallelo si annullavano a vicenda.
    private let semAvvio = DispatchSemaphore(value: 1)

    func applicationDidFinishLaunching(_ n: Notification) {
        // Se è già in esecuzione, un secondo avvio non deve sembrare "non è successo
        // niente": apre il pannello e si chiude, lasciando il primo al suo posto.
        let mie = Bundle.main.bundleIdentifier.map {
            NSRunningApplication.runningApplications(withBundleIdentifier: $0)
        } ?? []
        if mie.count > 1 {
            apriPannello()
            NSApp.terminate(nil)
            return
        }

        // La voce nella barra. Perché compaia servono due cose, scoperte a fatica:
        //  · l'eseguibile del bundle dev'essere QUESTO programma, non uno script
        //    che poi fa exec (con lo script la voce nasce alta 0 pixel e resta
        //    invisibile pur risultando presente);
        //  · la politica di attivazione dev'essere .accessory.
        voce = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        voce.isVisible = true
        if let b = voce.button {
            // Un simbolo di sistema alla dimensione giusta per la barra: il
            // carattere "◔" veniva disegnato minuscolo e non si distingueva.
            let conf = NSImage.SymbolConfiguration(pointSize: 15, weight: .medium)
            if let img = NSImage(systemSymbolName: "memorychip.fill",
                                 accessibilityDescription: "theLAAP")?
                            .withSymbolConfiguration(conf) {
                img.isTemplate = true          // si adatta a barra chiara o scura
                b.image = img
                b.imagePosition = .imageLeading
            } else {
                b.font = NSFont.systemFont(ofSize: 15, weight: .semibold)
                b.title = "◉"
            }
            b.toolTip = "theLAAP — clicca per il pannello"
        }

        menu.delegate = self
        voce.menu = menu

        // In sottofondo: se blocca il thread principale, la barra dei menu non
        // fa in tempo a disegnare la voce e sembra che l'app non ci sia.
        DispatchQueue.global().async { self.avviaServerSeSpento() }

        aggiorna()
        timer = Timer.scheduledTimer(withTimeInterval: 8, repeats: true) { _ in self.aggiorna() }

        // La finestra dopo un istante: aprendola nello stesso giro di eventi in cui
        // si crea la voce nella barra, quest'ultima non fa in tempo ad agganciarsi
        // e resta invisibile (finestra alta 0 pixel).
        DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { self.apriPannello() }
    }

    /// Il titolo nella barra è la sola cosa sempre visibile: ci metto la percentuale
    /// di memoria occupata dai modelli, che è il numero che conta davvero.
    func aggiorna() {
        chiedi("/api/memoria", Memoria.self) { m in
            DispatchQueue.main.async {
                guard let m else {
                    self.serverVivo = false
                    self.voce.button?.toolTip = "theLAAP — il pannello non risponde"
                    return
                }
                self.serverVivo = true
                let usati = m.occupatiGB
                let quota = m.totaleGB > 0 ? usati / m.totaleGB : 0
                let cerchi = ["○", "◔", "◑", "◕", "●"]
                let idx = min(4, Int(quota * 5))
                // i GB accanto all'icona: è il dato che serve a colpo d'occhio
                self.voce.button?.font = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .medium)
                self.voce.button?.title = usati > 0 ? " \(Int(usati))" : ""
                _ = cerchi; _ = idx
                self.voce.button?.toolTip = usati > 0
                    ? "\(m.caricati.count) modelli in memoria — \(Int(usati)) GB di \(Int(m.totaleGB))"
                    : "nessun modello in memoria"
            }
        }
    }

    // MARK: azioni

    /// Avvia il server del pannello come processo figlio, se non risponde già.
    /// Usa Process invece della shell: niente dipendenze dal PATH.
    func avviaServerSeSpento(daFinestra: Bool = false) {
        semAvvio.wait()
        defer { semAvvio.signal() }
        if URLSession.pingaSincrono(BASE + "/api/runtime") { serverVivo = true; return }

        // Solo il binario dentro il bundle: cercarne uno sulla Scrivania fa
        // comparire il dialogo dei permessi di macOS, e finché non si risponde
        // l'app resta bloccata sembrando morta.
        let bin = Bundle.main.bundlePath + "/Contents/MacOS/aipanel"
        guard FileManager.default.isExecutableFile(atPath: bin) else {
            avvisa("Non trovo il pannello", "Manca il programma del server dentro l'app.")
            return
        }
        // Il server va avviato da una shell di LOGIN. Provato tutto il resto:
        // come processo figlio di questa app, con ambiente pulito, e via
        // `launchctl submit` — in tutti quei casi il processo nasce ma resta
        // appeso senza mai aprire la porta (è un binario firmato solo ad-hoc,
        // e macOS lo tratta diversamente a seconda di chi lo lancia).
        // Con `bash -l` parte sempre: è l'unico contesto che funziona.
        let p = Process()
        p.executableURL = URL(fileURLWithPath: "/bin/bash")
        p.arguments = ["-l", "-c", "nohup '\(bin)' >/tmp/aipanel.log 2>&1 &"]
        do { try p.run() } catch {
            avvisa("Il pannello non parte", error.localizedDescription)
            return
        }

        for _ in 0..<40 {                       // fino a 10 secondi
            if URLSession.pingaSincrono(BASE + "/api/runtime") { serverVivo = true; return }
            Thread.sleep(forTimeInterval: 0.25)
        }
        // non è partito: passo dal Terminale, che funziona sempre
        avviaViaTerminale()
        for _ in 0..<60 {
            if URLSession.pingaSincrono(BASE + "/api/runtime") { serverVivo = true; return }
            Thread.sleep(forTimeInterval: 0.25)
        }
    }

    @objc func apriPannello() {
        if finestra == nil {
            finestra = Finestra()
            finestra?.barra = self
        }
        finestra?.mostra()
        // il server può non essere ancora su: lo avvio in sottofondo e ricarico
        DispatchQueue.global().async {
            self.avviaServerSeSpento()
            DispatchQueue.main.async { self.finestra?.ricarica() }
        }
    }

    /// Apre una finestra di Terminale che avvia il server. È l'unico contesto in
    /// cui un binario firmato solo ad-hoc parte su macOS 26: provati e falliti
    /// processo figlio, launchd, launchctl submit e shell di login dentro l'app.
    ///
    /// Con un freno, perché senza non ce l'aveva: ogni tentativo apriva una
    /// finestra di Terminale nuova, e i tentativi arrivano da più punti (l'avvio,
    /// l'apertura del pannello, le voci di menu). Visto succedere: otto finestre
    /// sovrapposte, tutte con lo stesso errore. Se il ripiego non ha funzionato
    /// la prima volta, non funziona nemmeno alla settima — e intanto sommerge lo
    /// schermo di chi sta cercando di capire cosa non va.
    private var ultimoTerminale = Date.distantPast
    func avviaViaTerminale() {
        let script = Bundle.main.bundlePath + "/Contents/Resources/start-server.command"
        guard FileManager.default.isExecutableFile(atPath: script) else { return }
        guard Date().timeIntervalSince(ultimoTerminale) > 120 else { return }
        ultimoTerminale = Date()
        esegui("open -a Terminal \"\(script)\"")
    }



    /// Esegue una voce del registro, sulla rotta e col corpo che la voce dichiara.
    /// Nessun id e nessuna rotta scritti qui: indovinarli e il difetto del
    /// 16/08/2026, quando il menu mandava GET a una rotta che accetta solo POST.
    func lancia(_ c: Comando, timeout: Int = 300) {
        guard let dati = try? JSONSerialization.data(withJSONObject: c.corpo ?? [:]),
              let json = String(data: dati, encoding: .utf8) else { return }
        let corpo = json.replacingOccurrences(of: "'", with: "'\\''")
        let riga = """
        T=$(curl -s -m 10 '\(BASE)/' \
            | sed -n 's/.*name="thelaap-token" content="\\([a-f0-9]*\\)".*/\\1/p' | head -1); \
        [ -n "$T" ] && curl -s -m \(timeout) -X POST '\(BASE)\(c.rotta)' \
            -H 'Content-Type: application/json' \
            -H 'Origin: \(BASE)' \
            -H "X-theLAAP-Token: $T" \
            -d '\(corpo)' >/dev/null &
        """
        esegui(riga)
    }

    /// Un comando del registro cliccato dal menu. Quelli marcati a rischio
    /// chiedono conferma: da qui si spengono programmi che stanno servendo.
    @objc func comandoDelRegistro(_ v: NSMenuItem) {
        guard let c = v.representedObject as? Comando else { return }
        if c.rischio == true {
            let a = NSAlert()
            a.messageText = "\(c.nome)?"
            a.informativeText = c.cosa ?? "Questo comando cambia lo stato della macchina."
            a.addButton(withTitle: c.nome)
            a.addButton(withTitle: "Annulla")
            a.alertStyle = .warning
            NSApp.activate(ignoringOtherApps: true)
            guard a.runModal() == .alertFirstButtonReturn else { return }
        }
        // I comandi lunghi non vanno troncati a meta: la durata la dichiara la
        // configurazione, e "minuti" non sta in trecento secondi.
        let lungo = (c.durata ?? "").lowercased().contains("minut")
        lancia(c, timeout: lungo ? 900 : 300)
        avvisa(c.nome, c.cosa ?? "Apri il pannello per vedere il risultato.")
    }

    @objc func esci() { NSApp.terminate(nil) }

    // ── le voci del menu in alto ──────────────────────────────────────────
    // Target nil nel menu: AppKit risale la catena e arriva qui, che è il
    // delegato dell'app. Se la finestra non c'è, la si apre invece di non fare
    // niente — una voce di menu che tace è il difetto che abbiamo già pagato.
    private func conFinestra(_ cosa: @escaping (Finestra) -> Void) {
        if finestra == nil { apriPannello() }
        guard let f = finestra else { return }
        if f.window?.isVisible != true { f.mostra() }
        cosa(f)
    }
    @objc func menuPalette() { conFinestra { $0.chiama("apriCmdk(true)") } }
    @objc func menuAiuto() { conFinestra { $0.chiama("vaiTab('aiuto')") } }
    @objc func menuMemoria() { conFinestra { $0.chiama("vai('memoria','tutti')") } }
    @objc func menuProgrammi() { conFinestra { $0.chiama("vai('programmi')") } }
    @objc func menuManutenzione() { conFinestra { $0.chiama("vai('manutenzione')") } }
    @objc func menuConfig() { conFinestra { $0.chiama("vai('config')") } }
    @objc func menuRicarica() { conFinestra { $0.ricarica() } }
    @objc func menuZoomPiu() { conFinestra { $0.zoom(0.1) } }
    @objc func menuZoomMeno() { conFinestra { $0.zoom(-0.1) } }
    @objc func menuZoomZero() { conFinestra { $0.zoomNormale() } }

    /// Click sull'icona nel Dock quando non ci sono finestre aperte.
    func applicationShouldHandleReopen(_ s: NSApplication, hasVisibleWindows flag: Bool) -> Bool {
        if !flag { apriPannello() }
        return true
    }

    /// Chiudendo la finestra l'app resta nella barra dei menu, non muore.
    func applicationShouldTerminateAfterLastWindowClosed(_ s: NSApplication) -> Bool { false }



    func avvisa(_ titolo: String, _ testo: String) {
        esegui("osascript -e 'display notification \"\(testo)\" with title \"\(titolo)\"'")
    }
}

// MARK: - il menu, ricostruito a ogni apertura

extension Barra: NSMenuDelegate {
    func menuWillOpen(_ m: NSMenu) {
        m.removeAllItems()
        let attesa = NSMenuItem(title: "leggo…", action: nil, keyEquivalent: "")
        attesa.isEnabled = false
        m.addItem(attesa)

        let gruppo = DispatchGroup()
        var mem: Memoria?
        var rts: [Runtime] = []
        var cmds: [Comando] = []

        gruppo.enter(); chiedi("/api/memoria", Memoria.self) { mem = $0; gruppo.leave() }
        gruppo.enter(); chiedi("/api/runtime", [Runtime].self) { rts = $0 ?? []; gruppo.leave() }
        gruppo.enter(); chiedi("/api/comandi", [Comando].self) { cmds = $0 ?? []; gruppo.leave() }

        gruppo.notify(queue: .main) {
            m.removeAllItems()

            if mem == nil {
                m.addItem(self.voceInerte("Il pannello non è in esecuzione"))
                m.addItem(.separator())
                m.addItem(self.voce("Avvia e apri il pannello", #selector(self.apriPannello)))
                m.addItem(.separator())
                m.addItem(self.voce("Esci", #selector(self.esci)))
                return
            }
            let mm = mem!
            let usati = mm.occupatiGB

            m.addItem(self.voceInerte(String(format: "Memoria — %.0f GB usati di %.0f", usati, mm.totaleGB)))
            if mm.caricati.isEmpty {
                m.addItem(self.voceInerte("   nessun modello caricato"))
            } else {
                for c in mm.caricati {
                    m.addItem(self.voceInerte(String(format: "   %@ · %.1f GB", self.corto(c.nome), c.gb)))
                }
            }
            for a in (mm.avvisi ?? []) { m.addItem(self.voceInerte("⚠︎ " + a)) }

            m.addItem(.separator())
            // Il riavvio viene dal registro, cercato per servizio: la voce a
            // mano mandava una POST senza token, il pannello rispondeva 403 e
            // la notifica diceva comunque che era partita. Se il registro non
            // ha il comando, la voce resta inerte invece di promettere.
            for r in rts {
                let c = cmds.first { $0.gruppo == "programs" && $0.corpo?["servizio"] == r.chiave }
                let titolo = (r.attivo ? "● " : "○ ") + self.nomeAmichevole(r.chiave)
                guard let c else {
                    m.addItem(self.voceInerte(titolo))
                    continue
                }
                let v = NSMenuItem(title: titolo,
                                   action: #selector(self.comandoDelRegistro(_:)), keyEquivalent: "")
                v.target = self
                v.representedObject = c
                v.toolTip = r.attivo ? "In funzione sulla porta \(r.porta). Clicca per riavviarlo."
                                     : "Spento. Clicca per riavviarlo."
                m.addItem(v)
            }

            // I comandi vengono dal registro, nell'ordine in cui il server li da:
            // regimi e strumenti prima, i due di macchina in fondo, dove stavano.
            // Nessun id e nessuna etichetta scritti qui.
            for (titolo, gruppi) in [("", ["regime", "maintenance"]), ("", ["machine"])] {
                let voci = cmds.filter { gruppi.contains($0.gruppo) }
                if voci.isEmpty { continue }
                m.addItem(.separator())
                if !titolo.isEmpty { m.addItem(self.voceInerte(titolo)) }
                for c in voci {
                    let v = NSMenuItem(title: c.nome, action: #selector(self.comandoDelRegistro(_:)),
                                       keyEquivalent: "")
                    v.target = self
                    v.representedObject = c
                    v.toolTip = [c.cosa, c.durata].compactMap { $0 }.joined(separator: " · ")
                    m.addItem(v)
                }
            }
            if cmds.isEmpty {
                m.addItem(.separator())
                m.addItem(self.voceInerte("Nessun comando in configurazione"))
            }

            m.addItem(.separator())
            m.addItem(self.voce("Apri il pannello…", #selector(self.apriPannello), "o"))
            m.addItem(.separator())
            m.addItem(self.voce("Esci", #selector(self.esci), "q"))
        }
    }

    func voce(_ t: String, _ sel: Selector, _ tasto: String = "") -> NSMenuItem {
        let v = NSMenuItem(title: t, action: sel, keyEquivalent: tasto)
        v.target = self
        return v
    }
    func voceInerte(_ t: String) -> NSMenuItem {
        let v = NSMenuItem(title: t, action: nil, keyEquivalent: "")
        v.isEnabled = false
        return v
    }
    func corto(_ n: String) -> String {
        let ultimo = n.components(separatedBy: "--").last ?? n
        return ultimo.count > 30 ? String(ultimo.prefix(28)) + "…" : ultimo
    }
    func nomeAmichevole(_ chiave: String) -> String {
        switch chiave {
        case "mtplx":    return "Modello per il codice"
        case "omlx":     return "Modelli grandi e senza filtri"
        case "lmstudio": return "Modelli di chat"
        case "ollama":   return "Ricerca nei documenti"
        default:         return chiave
        }
    }
}

/// Il menu principale dell'applicazione.
///
/// Serve anche se non si vede. Un'app `.accessory` non disegna la barra dei
/// menu, ma AppKit instrada le scorciatoie da tastiera **attraverso**
/// `NSApp.mainMenu`: senza, Cmd+A, Cmd+C, Cmd+V, Cmd+X e Cmd+Z non arrivano al
/// campo di testo, e Cmd+W / Cmd+M non chiudono né riducono la finestra. Era
/// esattamente il sintomo: "nei campi non funzionano i comandi classici, e
/// nella finestra nemmeno le cose normali di un'app".
///
/// Le voci usano i selettori standard di AppKit (`copy:`, `paste:`, …), che
/// AppKit risolve sul primo responder — dentro una WKWebView è il campo che ha
/// il fuoco.
func costruisciMenuPrincipale() -> NSMenu {
    let principale = NSMenu()

    func sottomenu(_ titolo: String, _ voci: [(String, Selector?, String, NSEvent.ModifierFlags)]) {
        let contenitore = NSMenuItem()
        let m = NSMenu(title: titolo)
        for (nome, azione, tasto, modificatori) in voci {
            if nome == "-" { m.addItem(.separator()); continue }
            let v = NSMenuItem(title: nome, action: azione, keyEquivalent: tasto)
            if !modificatori.isEmpty { v.keyEquivalentModifierMask = modificatori }
            m.addItem(v)
        }
        contenitore.submenu = m
        principale.addItem(contenitore)
    }

    sottomenu("theLAAP", [
        ("Informazioni su theLAAP", #selector(NSApplication.orderFrontStandardAboutPanel(_:)), "", []),
        ("-", nil, "", []),
        ("Nascondi theLAAP", #selector(NSApplication.hide(_:)), "h", [.command]),
        ("Nascondi gli altri", #selector(NSApplication.hideOtherApplications(_:)), "h", [.command, .option]),
        ("Mostra tutti", #selector(NSApplication.unhideAllApplications(_:)), "", []),
        ("-", nil, "", []),
        ("Esci da theLAAP", #selector(NSApplication.terminate(_:)), "q", [.command]),
    ])

    sottomenu("Modifica", [
        ("Annulla", Selector(("undo:")), "z", [.command]),
        ("Ripristina", Selector(("redo:")), "z", [.command, .shift]),
        ("-", nil, "", []),
        ("Taglia", #selector(NSText.cut(_:)), "x", [.command]),
        ("Copia", #selector(NSText.copy(_:)), "c", [.command]),
        ("Incolla", #selector(NSText.paste(_:)), "v", [.command]),
        ("Seleziona tutto", #selector(NSText.selectAll(_:)), "a", [.command]),
    ])

    // Le voci del pannello non rifanno niente: chiamano le stesse funzioni della
    // pagina. Servono a rendere di sistema — e quindi visibili, con la loro
    // scorciatoia scritta accanto — cose che altrimenti si scoprono per caso.
    sottomenu("Pannello", [
        ("Chiedi o comanda…", #selector(Barra.menuPalette), "k", [.command]),
        ("Aiuto del pannello", #selector(Barra.menuAiuto), "/", [.command]),
        ("-", nil, "", []),
        ("Memoria unificata", #selector(Barra.menuMemoria), "1", [.command]),
        ("Programmi", #selector(Barra.menuProgrammi), "2", [.command]),
        ("Manutenzione", #selector(Barra.menuManutenzione), "3", [.command]),
        ("Configurazioni", #selector(Barra.menuConfig), "4", [.command]),
        ("-", nil, "", []),
        ("Ricarica il pannello", #selector(Barra.menuRicarica), "r", [.command]),
    ])

    sottomenu("Vista", [
        ("Ingrandisci", #selector(Barra.menuZoomPiu), "+", [.command]),
        ("Riduci", #selector(Barra.menuZoomMeno), "-", [.command]),
        ("Dimensione reale", #selector(Barra.menuZoomZero), "0", [.command]),
        ("-", nil, "", []),
        ("Schermo intero", #selector(NSWindow.toggleFullScreen(_:)), "f", [.command, .control]),
    ])

    sottomenu("Finestra", [
        ("Riduci a icona", #selector(NSWindow.performMiniaturize(_:)), "m", [.command]),
        ("Zoom", #selector(NSWindow.performZoom(_:)), "", []),
        ("-", nil, "", []),
        ("Chiudi", #selector(NSWindow.performClose(_:)), "w", [.command]),
    ])

    return principale
}

let app = NSApplication.shared
let delegato = Barra()
app.delegate = delegato
// .accessory, non .regular: con l'icona nel Dock il sistema NON disegna la voce
// nella barra (verificato con un'app di prova: con .accessory compare, con
// .regular no). La finestra si apre lo stesso, serve solo attivare l'app.
app.setActivationPolicy(.accessory)
// Il pannello è una finestra sola. Lasciando le schede attive, macOS aggiunge
// da sé «Mostra la barra dei pannelli» e «Mostra tutti i pannelli» dentro Vista:
// due voci che qui non governano niente.
NSWindow.allowsAutomaticWindowTabbing = false
app.mainMenu = costruisciMenuPrincipale()
app.run()
