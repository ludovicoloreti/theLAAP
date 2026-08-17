<div align="center">

# theLAAP

### The Local AI Admin Panel

**One place to see and govern every AI model running on your Mac.**

Unified memory measured on processes, not on files. An arbiter that refuses a load
before it takes the machine down. Two axes per model, computed once on the server.

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Swift](https://img.shields.io/badge/Swift-6.3-F05138?logo=swift&logoColor=white)](https://swift.org)
[![macOS](https://img.shields.io/badge/macOS-13%2B-000000?logo=apple&logoColor=white)](#-install)
[![Dependencies](https://img.shields.io/badge/dependencies-1-brightgreen)](go.mod)
[![Tests](https://img.shields.io/badge/tests-73-success)](#-tests)
[![License](https://img.shields.io/badge/license-MIT-blue)](LICENSE)

**English** · [Italiano](README.it.md)

</div>

---

## 🔥 Why this exists

On 27 July 2026 at 18:42 this Mac hit a kernel panic.

```
watchdog timeout: no checkins from watchdogd in 93 seconds
```

Two inference servers were resident at once, ~154 GB on a 128 GB machine. Swap
thrashing, saturated I/O, `watchdogd` starved, kernel down.

The interesting part is not the crash. It is that the memory guard of one server,
asked whether it could admit the model, said yes, and wrote it in its own log:

```
Admitting 'Laguna-S-2.1-oQ6e' above the admission soft target
with no idle model left to evict (90.82GB > 86.70GB, ceiling 102.00GB)
```

It was not lying. **Every server knows only itself.** An admission decision cannot be
delegated to something that cannot see the whole machine. Somebody has to sit above
all of them, and a panel is the only thing in that position.

That is what this is.

---

## ⚡ What it does

| | |
|---|---|
| 🧠 **Unified memory, measured** | One bar for the whole Mac. RAM and VRAM are the same pool on Apple Silicon. Occupancy comes from the **processes**, not from the model files: one server here declares 29.3 GB and holds 33.3. |
| ⚖️ **An arbiter that says no** | Before loading, `budget.go` answers "does it fit?" with a verdict: how much is missing, and what to stop to make room. One large model at a time, above a threshold you set. |
| 🧭 **Two axes, one source** | Every model carries a **state** (how it is now) and a **class** (how it can coexist), computed on the server so the panel cannot contradict the arbiter. |
| ⌘ **⌘K without a model** | A deterministic interpreter. It reads the action, the amount in GB, the model and the program, shows what it understood, and proposes one thing. No LLM in the loop. |
| 🤖 **A small local model** | Writes model descriptions and answers questions about the panel, with the live machine state in front of it. Picked by weight, never above 8B. |
| 📝 **Config editor** | The panel's own file and every client file, JSON or YAML, validated, backed up, conflict checked. |
| 🔎 **HuggingFace search** | Only the MLX formats this Mac can run, with real sizes and whether each one fits right now. |
| 🎛 **Regimes** | Machine profiles that flip together: stop the other programs, **then** widen the memory margins. The order is not cosmetic. |

---

## 🧩 Two axes, not one

```
state    ready · in-memory · off · incoming · faulty · remote
class    coexisting · exclusive · resident · remote
```

They are not synonyms. A model can be `off` and `exclusive`: not loaded, and such that
loading it demands the machine to itself.

Both come from `/api/modelli`, computed in `stati.go`. The threshold above which a
model is `exclusive` is **the same** `SogliaGrandeByte` the arbiter uses to refuse the
second large model. With two separate numbers the panel would label a model
"coexisting" while the preflight rejects it, and nobody would notice.

For the same reason "if I load it now, does it fit?" is not a sum done in the page. It
is the arbiter's verdict, shipped with the model.

### Three numbers that are three different things

| | |
|---|---|
| **used** | measured on the processes |
| **free** | total minus used, what the bar draws |
| **free for a new model** | minus the reserve kept for the OS |

Conflating them means promising 103 GB when there are 42.

---

## 📦 Install

```bash
git clone https://github.com/ludovicoloreti/theLAAP.git
cd theLAAP
./build.sh --install     # macOS: builds and installs theLAAP.app
```

The menu bar item shows the GB currently held by models. The panel opens in a real
`NSWindow` with a `WKWebView` inside: system traffic lights, a real menu bar, no
browser and no address bar.

Other platforms build too, with a smaller feature set:

```bash
go build -o thelaap ./cmd/thelaap && ./thelaap    # then open http://127.0.0.1:7070
```

| | macOS | Linux | Windows |
|---|---|---|---|
| memory | `sysctl` + `vm_stat` | `/proc/meminfo` | CIM via PowerShell |
| GPU ceiling | `iogpu.wired_limit_mb` | `nvidia-smi` / `rocm-smi` | `Win32_VideoController` |
| services | `launchctl` | `systemctl --user` | direct commands |
| menu bar item | yes | no, panel only | no, panel only |

---

## ⚙️ Configure

The program knows nothing about the machine it runs on. Everything specific lives in
`~/.config/thelaap/configurazione.json`, written on first run by detecting what is
installed. Add a server it has never heard of without touching the code:

```json
{
  "runtime": [
    { "nome": "My server", "chiave": "mine", "porta": 9000,
      "elencoModelli": "/v1/models",
      "avvia": "systemctl --user start myserver",
      "ferma": "systemctl --user stop myserver",
      "modelliCaricati": "my-cli ps" }
  ],
  "modelloAiuto": "gemma-4-E2B-it-MLX-8bit",
  "riservaSistemaGB": 24,
  "sogliaModelloGrandeGB": 40
}
```

`sogliaModelloGrandeGB` is the one threshold: it drives both the `exclusive` label and
the refusal. `riservaSistemaGB` is what stays with the OS. `modelloAiuto` pins the
helper model; empty means the smallest one that can hold a conversation.

Detected on its own: Ollama, LM Studio, oMLX, MTPLX, llama.cpp, vLLM, plus the Pi and
OpenCode config files.

---

## 🏗 How it is built

```
menu bar item (Swift)          panel (Go + one HTML file)
NSWindow + WKWebView           127.0.0.1:7070, localhost only
        |                              |
        +--------- HTTP + token -------+
                       |
        +--------------+--------------+
    budget.go       stati.go      memory.go
    the arbiter    state/class    the measure
```

```
cmd/thelaap/      the program: server, routes, embedded page
internal/budget/  the memory arbiter, no I/O
menubar/          menu bar item and window (Swift)
examples/         an adaptable example of a regime script
```

`internal/budget` is the only extracted piece, and not for symmetry: `budget.go`
imports `fmt` and `sort` and uses nothing else from the program. Its comment
promises "nothing is executed and nothing is read from the system here", and in a
package the compiler enforces that promise instead of the comment.

The rest is a flat `package main`, and that is measured rather than lazy: **128 of
228 package level symbols are used outside the file that defines them** —
`scriviJSON` from 17 files, `cfg()` from 13. Splitting would mean exporting a
hundred identifiers or creating a `util` bag, which is worse than flat.
Acknowledged debt: it gets paid by redesigning the boundaries, not by moving
files. And `cmd/`/`pkg/` is not an official Go standard: the layout that goes by
that name states itself that it is not affiliated with the Go team.

- **One dependency**, `yaml.v3`, used by the config editor. Nothing else.
- **One HTML file**, embedded in the binary. No build step, no framework, no network.
- **Localhost only**, and anything that mutates state needs `Origin` plus a token that
  is regenerated at every start and never written to disk.
- **Shell command lines never reach the browser.** The panel receives three booleans
  saying whether a program can be started, stopped or restarted, decided by the same
  function that later runs it.

---

## ✅ Tests

65 tests. Each one was verified by breaking the rule before writing it: a test that
does not tell correct code from broken code is not a test.

| | |
|---|---|
| `budget_test.go` | the 27/07/2026 kernel panic scenario, without risking the machine |
| `stati_test.go` | state, class and the command registry: one source |
| `aiuto_test.go` | size read from total parameters; names distinct and jargon free |
| `menubar_contratto_test.go` | the menu bar hardcodes no id, reads the registry, and reports the same number as the panel |
| `sicurezza_test.go` | localhost, `Host`, Origin, token, POST only; config routes closed to unauthenticated reads |

```bash
go test ./...
```

---

## 💡 Notes learned the hard way

**A wrong diagnosis costs more than no diagnosis.** For weeks the app could not start
its own server. The note in this repo said the binary was ad-hoc signed and macOS would
only run it with a terminal among its ancestors. False. The sampler found the main
thread parked inside `open()`, 100% of samples: a data file under `~/Desktop`, which is
TCC protected, and a child of a `.app` waiting forever for a permission prompt that
never appears. Moving the file fixed it in one second. The note is rewritten in place,
not deleted, because the wrong explanation was plausible and that is why it cost so
much.

**An accessory app draws no menu bar.** `.accessory` is required for the status item to
appear at all, and it is also why the panel had no menu for months. The way out is that
the two things do not happen at the same moment: create the status item while
`.accessory`, then switch to `.regular` when a window opens.

**A cancelled navigation is not a failure.** `WKWebView` reports both through the same
delegate method, so the panel claimed to be offline while the server answered 200.

**Two programs talking over HTTP have a contract, and an unverified contract rots.**
The menu bar items silently did nothing for weeks: GET on a POST only route, ids that
had been renamed, no token. Keeping two hand written lists aligned with a test was
better than not doing it, but it was still one list too many: the menu bar now builds
its items from `/api/comandi` and executes on the route each item declares. No id and
no route in the Swift at all, and the test verifies that none comes back.

**`lms ps` costs 4 seconds** because it is Node and restarts every time. With a page
refreshing every 5 seconds it kept the request permanently pending. A background worker
keeps the snapshot ready: 4100 ms to 0 ms.

The long version, with the call graphs and the numbers, is in the
[Italian README](README.it.md).

---

## 🔐 Security

- Listens on `127.0.0.1` only and refuses anything that does not come from localhost.
  Services get restarted from here.
- **`Host` is checked on every request, reads included.** Checking the source address is
  not enough: it is the user's own browser making the request, so it stays `127.0.0.1`
  whichever page asked. A domain that resolves to `127.0.0.1` would otherwise become
  same origin with the panel and get to read its answers.
- Anything that mutates state needs `Origin` plus a token, regenerated at every start
  and never written to disk.
- **The routes that return client config files ask for the token in reads too.** Those
  files are where the panel writes provider keys.
- Executable commands are a closed list, resolved from local config by id. No text from
  a request ever reaches a shell.
- Model names come from third party repositories. Inside an `onclick` they are escaped
  for both parsers, the HTML attribute and the JavaScript string: escaping for one only
  is what left ten buttons dead, silently, until someone tried to click them.
- Config writes go through a backup, a temporary file and a rename, and refuse to
  overwrite an external change in silence.
- Archive deletion resolves the path against the archive root, rejecting `..`, absolute
  paths and a symlink in any component.
- Provider keys are read to make the request and never leave the process. The panel
  shows the last four digits and nothing else.

---

## 📄 License

MIT. See [LICENSE](LICENSE).
