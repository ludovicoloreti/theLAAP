# Examples

Machine specific material, kept here as something to copy. It is not part of the
program and nothing runs it until you point at it from your configuration.

## `exclusive-regime.sh.example`

The memory profile for running one very large model on its own, written for oMLX
on a 128 GB Mac. It contains paths and service labels from *that* machine: copy
it, adapt it, then point at it from `~/.config/thelaap/configurazione.json`:

```json
{ "regimi": [ { "chiave": "esclusivo", "nome": "One model only",
    "runtimeAttivo": "omlx",
    "attiva":    "/your/path/regime.sh margini-larghi",
    "disattiva": "/your/path/regime.sh margini-prudenti",
    "segno": "~/.omlx/.esclusivo" } ] }
```

What it does, in short: it shrinks the prompt cache, which is **subtracted** from
the runtime's memory ceiling, and widens the memory guard margin. It only makes
sense when nothing else large is running on the machine, which is exactly why the
panel stops the other programs before running it.

The two subcommands the panel invokes are `margini-larghi` and
`margini-prudenti`: wide margins and cautious margins. The panel already knows
how to stop and restart the programs, so the script only has to move the memory
knobs.
