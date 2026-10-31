# 024 — T3a: la catena dei dati del banco (cattura Flask sintetica → standalone → import Go)

- **Fonte:** T3 (PIANO §4.2): stessi dati sintetici dalle due parti, per costruzione. Standalone a
  `go-baseline` = `6eb93ed`: `test/fixtures/legacy_import/synthetic_capture.py`,
  `LegacyImport.Release`, `deploy/compose.import.yaml`, `config/runtime.exs`, il Dockerfile.
- **Voci:** nessuna (T3 non chiude voci di prodotto).
- **Esito:** la catena gira fino all'import Go, che **si blocca su un bug dello standalone**
  (domanda 7 in `docs/domande-aperte.md`). Il banco con i dati importati quindi non arriva ancora al
  verde; il banco di sempre resta verde. Gli scenari sui dati importati (T3b) aspettano la risposta.
- **Lavoro:** implementazione e correzioni di un sottoagente, review di un sottoagente diverso; la
  sessione principale ha verificato nel codice dello standalone la causa del blocco e rilanciato
  `scripts/check`, il banco e la catena.

## Cosa c'è

- **`banco su … -dati sintetici-import`** (`tools/banco/catena.go`). Ordine: bootstrap e dipendente
  dello standalone → cattura → legacy import → decifratura → import Go → app, caddy e Go. Ogni passo
  in un container `--rm`, `--network none`, utente 1000, sola lettura, `cap-drop ALL`,
  `no-new-privileges`, limiti, montaggi solo dentro l'istanza:
  1. **cattura:** `synthetic_capture.py` letto con `git show go-baseline:` a ogni giro, eseguito
     con `python3 -I` dentro l'immagine dello standalone;
  2. **legacy import** dello standalone in modalità sintetica (`Release.main()`), con una sola riga
     `GR_IMPORT` riconciliata e sintetica pretesa;
  3. **copia** del database cifrato e dei media (mai un `-journal`; la copia non deve essere in
     chiaro);
  4. **decifratura** con il SQLCipher dell'immagine (`ATTACH … KEY ''`, `sqlcipher_export`),
     **niente sqlcipher sull'host**; il container riceve solo la chiave SQLCipher e la
     `secret_key_base` che `runtime.exs` pretende;
  5. **`gestrip import -sintetico`**: il banco accetta solo un report riconciliato, con la stessa
     impronta e dimensione della sorgente, file e byte dei media uguali a quelli copiati, nessun
     file in più. Solo allora parte il Go, senza semina.
- Dopo un import riconciliato le copie in chiaro e cifrate della catena si tolgono; dopo un errore
  restano nell'istanza (0600/0700) per la diagnosi. Se la catena si ferma non resta acceso nessun
  container. `banco giu` toglie anche i container della catena (una lista sola dei nomi).
- I file scritti dai container si aprono senza seguire collegamenti.

## Il blocco (domanda 7)

Il legacy import dello standalone (`legacy_import/media.ex`, `store_document`) salva
`historical_documents.sha256` come TEXT: un `Repo.query` scritto a mano passa i 32 byte come
binario semplice. La sessione principale l'ha letto nel codice al tag. Sulla catena: `typeof` =
`text`, `hex()` giusto, file con l'impronta giusta. Il Go blocca (`tipo/blob_atteso`,
`media/hash_diverso`, `riconciliazione/conteggio`; 45 righe lette, 44 scritte). **Non è stato
corretto né nel Go né nello standalone.**

Diagnosi su una copia, fuori dal banco: convertendo la colonna in BLOB, l'import Go è riconciliato
(45 righe, 5 file, 141 byte), e `banco esegui` sui dati importati dà 13 scenari, 156 passi e **una
differenza**: lo scenario `utenti` (passo 2) si aspetta un database senza account in attesa,
mentre gli utenti importati da Flask compaiono come «Invito da consegnare» **da tutte e due le
parti**. Non è una differenza fra le app: lo scenario va adattato ai dati importati in T3b.

## Prove

- **TDD:** prima giro: parti pure di `tools/banco`, 12 test rossi su stub; correzioni della review:
  rosso in tre tempi (compilazione, stub, verde). Dichiarati alcuni test scritti dopo il refactor.
- **Test:** costruzione dei comandi, percorsi dentro l'istanza, lettura dell'uscita del legacy
  import e del report Go, copie senza collegamenti, e `cmdSu`/`cmdGiu` su un docker finto: ogni
  ramo in cui la catena deve fermarsi (riga mancante, verdetto bloccato, decifratura mancante, file
  decifrato collegamento, import Go con errore o bloccato o senza report, report con 0 file) senza
  container accesi.
- **Mutazioni:** 45, **39 uccise**, 6 vive (difese doppie equivalenti).
- **Giri dal vivo** (rifatti dalla sessione principale): banco senza opzione verde (13 scenari, 156
  passi), «AVANZI: nessuno»; catena fermata al blocco noto in circa 5 s, istanza da 2,5 MB, nessun
  container rimasto, «AVANZI: nessuno». `scripts/check` verde.

## Review indipendente

Nessuna ALTA: isolamento, montaggi e chiavi reggono (nessun segreto nell'uscita, nemmeno con una
decifratura fatta fallire apposta); il percorso senza `-dati` è identico. Corretti:
- **MEDIA:** i controlli che fermano la catena non avevano test; i nomi dei container tolti da
  `giu` non avevano test.
- **BASSA:** collegamenti seguiti sui file scritti dai container; report Go non confrontato con
  quanto copiato; parte della catena girava con l'app già accesa; copie in chiaro tenute anche dopo
  il riconciliato; ambiente della decifratura più largo del necessario.

Dichiarato: restano nell'istanza la cattura sintetica e l'uscita del legacy import (dati inventati)
per la diagnosi; `banco giu` le toglie.

## Ricevuta

- **Data:** 31/10/2026, sessione Claude Code su pc-fisso. Riferimento `go-baseline` = `6eb93ed`.
- **Commit:** `105727c`, dopo `99e8c79`.
