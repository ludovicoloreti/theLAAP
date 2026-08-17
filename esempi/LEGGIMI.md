# Esempi

Roba **specifica di una macchina**, tenuta qui come modello da copiare — non
fa parte del programma e non viene usata da nessuno finché non la richiami tu
dalla configurazione.

## `regime-esclusivo.sh.esempio`

Il profilo di memoria per far girare un modello molto grande da solo, scritto
per oMLX su un Mac da 128 GB. Contiene percorsi ed etichette di servizio di
*quella* macchina: copialo, adattalo, e poi puntalo da
`~/.config/thelaap/configurazione.json`:

```json
{ "regimi": [ { "chiave": "esclusivo", "nome": "Un modello solo",
    "runtimeAttivo": "omlx",
    "attiva":    "/percorso/tuo/regime.sh margini-larghi",
    "disattiva": "/percorso/tuo/regime.sh margini-prudenti",
    "segno": "~/.omlx/.esclusivo" } ] }
```

Cosa fa, in sostanza: riduce la cache dei prompt (che viene **sottratta** dal
tetto di memoria del runtime) e allarga il margine del memory guard. Ha senso
**solo** quando sulla macchina non gira nient'altro di grosso — ed è per
questo che il pannello ferma gli altri programmi prima di eseguirlo.
