# 038 — Galleria e caricamento delle foto dalla scheda (T4.3)

- **Fonte:** T4.3 del PIANO; piano delle slice nella ricevuta della 036.
- **Voci:**
  - chiusa RIP-S-31 (visibilità per canale);
  - parziale RIP-S-28 (galleria e visualizzatore): frecce, swipe ed Esc sono in `app.js`, si provano
    nel browser (`TestVisualizzatoreFotoNelBrowser`, saltato);
  - parziale RIP-S-29 (caricamento): anteprima e barra di avanzamento non ci sono, l'invio automatico
    e il messaggio che sparisce si provano nel browser (`TestCaricamentoFotoAnteprimaEAvanzamento`);
  - parziale RIP-S-32 (elimina): la conferma «Eliminare la foto?» è `data-confirm` in `app.js`, come
    la riapertura RIP-S-14 (`TestEliminaFotoConfermaNelBrowser`, saltato).
- **Lavoro:** implementazione e un giro di correzioni di un sottoagente, review e mutazioni di un
  sottoagente diverso; `scripts/check` e banco rilanciati dalla sessione principale, che ha anche
  verificato i sorgenti di LiveView 1.2.11 (pacchetto hex con l'impronta del `mix.lock` dello
  standalone).

## Cosa c'è

- **Galleria sulla scheda:** «Foto (N)», celle con la miniatura (`?size=thumb`), visualizzatore con
  ancore `:target` come `photo_viewer.ex` (contatore, vicini, «← Indietro» a `#repair-photos`), foto
  intera solo nel riquadro; lo staff vede tutte le foto. In `app.js` il porting di `photo_viewer.js`
  (frecce, swipe, Esc).
- **Rotte**, tutte `personale` come gli eventi di `RepairLive`, in `tabellaPermessi`, con le regole
  D-44 (segno del modulo, `allaScheda` o `dopoRifiuto`):
  - `POST /repairs/{id}/foto` (RIP-S-29), registrata con `f.rottaConFile`;
  - `POST /repairs/{id}/foto/{foto}/visibilita` (RIP-S-31), campi `audience` e `visible`;
  - `POST /repairs/{id}/foto/{foto}/elimina` (RIP-S-32).
- **Modulo con file senza JavaScript** (`csrf.ModuloConFile`): token nel corpo, in testa al modulo,
  controllato prima di qualunque file; prima del token il corpo si legge al più fino a
  `LimiteModulo`; dopo, fino a 2×25 MiB + 1 MiB. Regola in `docs/SVILUPPO.md`, «Moduli» (il piano
  della 036 diceva `x-csrf-token`: un modulo vero non può mandare intestazioni).
- **Caricamento come LiveView 1.2.11** (verificato sui sorgenti):
  - Fotocamera e Galleria sono due caricamenti separati; per ognuno contano le prime 2 voci, anche
    se una è rifiutata (`put_invalid_entry` la conta); le altre non si scrivono e compare «Massimo 2
    foto per volta.»;
  - per ogni voce prima la misura, poi il formato (`validate_max_file_size |> validate_accepted`);
  - gli errori delle voci in `#repair-photo-upload-errors`, nell'ordine di `repair_live.ex`; gli
    errori del dominio nel flash, l'ultimo; «Foto caricata.» se almeno una è salvata;
  - con JavaScript, `app.js` fa il preflight prima dell'invio automatico (più di 2 foto o una oltre
    25 MiB); senza, decide il server leggendo, e il 413 resta solo per un corpo abusivo;
  - la scadenza di lettura si sposta a ogni pezzo di 64 000 byte, 10 s per pezzo
    (`chunk_size` e `chunk_timeout` di LiveView): il `ReadTimeout` di 60 s del server vale per tutto
    il corpo e due foto grandi da rete mobile non ci starebbero;
  - sessione ricontrollata all'arrivo dei file e nella transazione; errore di risorse del figlio →
    `MsgFotoNonElaborata` (D-48); il file passa sempre da `buste.AllegaFoto`, quindi dal processo
    figlio.
- **File temporanei:** `<cartella del db>/caricamenti`, 0700 controllata all'avvio (non un link,
  permessi corretti), una cartella per richiesta, file 0600 con `O_EXCL` e nome non del browser,
  tolti sempre; nel registro mai nomi, percorsi o chiavi.
- **Nome del file:** `buste.PulisciNomeFile`, copia di `clean_name/1` (domanda 15, poi D-49).
- **Banco:** scenario `galleria` (HTML statico delle schede 1/26 e 2/26 con le foto della catena, e
  sul database vuoto il ritorno alla home).

## Differenze, dichiarate

- Il caricamento è un modulo vero, senza canale LiveView: senza JavaScript serve «Carica», con
  JavaScript parte da solo. Niente anteprima né barra di avanzamento.
- Il riquadro degli errori si vede una volta, dopo il redirect (nello standalone resta finché le voci
  non si annullano).
- Senza JavaScript, quando il corpo supera il limite dopo una violazione già vista, la voce interrotta
  diventa «Foto oltre il limite di 25 MiB.»; il browser potrebbe vedere la connessione chiusa invece
  del redirect (non misurato). Con JavaScript il caso non si presenta.
- Busta nel cestino o inesistente: si torna alla home con «Riparazione non trovata.», come le altre
  azioni della scheda (lo standalone, a pagina aperta, dà il messaggio dell'azione).
- Sessione revocata nella transazione dopo la normalizzazione: il Go va al login, come dice
  l'inventario; lo standalone dà «Foto rifiutata o non salvata.».
- Stile del visualizzatore in `app.css` invece che inline; `<p>` per errore invece di `<div>`.

## Da verificare al livello browser del banco (040)

- Con 3 foto, se «Massimo 2 foto per volta.» resta: il client annulla la terza (`entry.cancel()`),
  e se l'annullamento arriva al server `drop_entry` ricalcola gli errori e toglie `:too_many_files`.
- Invio automatico, preflight di `app.js`, conferma dell'eliminazione, frecce, swipe ed Esc.

## Prove

- **TDD:** 23 test rossi sugli stub prima dei gestori; nel giro delle correzioni 14 rossi. Tre test
  di CSRF e sessione passavano già sugli stub (la catena era scritta prima della misura), e due li ho
  scritti dopo il codice: il loro rosso è la mutazione uccisa.
- **Mutazioni:** implementazione 15 + 21 (correzioni), tutte compilano, tutte uccise. Review 52,
  tutte compilano, 36 uccise; delle 16 vive 5 equivalenti e 11 per test mancanti, coperte nel giro
  delle correzioni e uccise.
- **Misurato:** `TestCaricamentoPezziLenti` con un `http.Server` vero (`ReadTimeout` 300 ms): un
  corpo lento ma costante passa, un corpo fermo no; stabile su 8 giri.
- **Banco** (`fab-220`), rilanciato dalla sessione principale: **verde** con
  `-dati sintetici-import` e su database vuoto, 23 scenari e 318 passi, «AVANZI: nessuno». Il primo
  giro con i dati era rosso per un'attesa sbagliata dello scenario (la 2/26 è di un'oreficeria:
  «Orefice», le due parti d'accordo), corretta nello scenario e nel suo test.
- `scripts/check` verde: chiuse 26, parziali 45, rosse 0; **con l'esito del banco chiuse 51,
  parziali 20**, rosse 0.

## Review indipendente

Nessuna ALTA.
- **MEDIA**, tutte corrette: `ReadTimeout` su tutto il corpo (scadenza per pezzo); 413 grezzo oltre
  il limite (preflight in `app.js`, decisione leggendo senza JavaScript); più di 2 foto rifiutate
  tutte (ora le prime 2, come LiveView).
- **BASSA:** corpo e intestazioni limitati prima del token, nome ripulito, tutti gli errori, cartella
  dei caricamenti controllata all'avvio, test delle mutazioni vive e test-guardia contro le letture
  del modulo fuori dal CSRF → corretti; le differenze di esito → dichiarate sopra.

## Domande aperte nuove

In `docs/domande-aperte.md`: 15 (nome del file ripulito anche sulla scheda).
