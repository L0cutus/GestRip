-- Il registro dei backup verificati (slice 122, T5.2, D-11, OPS-17). Il PIANO (§2.5) lo chiamava
-- backup_verifications: qui ha un nome italiano come tutte le altre tabelle del Go. Solo Go
-- (importer.TabelleSoloGo): lo standalone non ha una tabella, scrive accanto a ogni archivio una prova
-- firmata HMAC (<id>.tar.age.json, verified_backup.ex).
--
-- Una riga per ogni verifica di un archivio appena scritto da gestrip backup, riuscita ('ok') o no
-- ('fallita', con il motivo in errore). Le righe non si modificano; il ripristino (130) svuota la
-- tabella, perché gli archivi registrati sono quelli della macchina vecchia.
--
-- Chi legge (db.BackupVerificatoDopo, db.BloccatoDopoImport) prende solo l'ultima riga 'ok' su una
-- destinazione locale (cartella o chiavetta) e vuole l'archivio ancora al suo posto con gli stessi byte
-- e lo stesso SHA-256; gli istanti strani (nel futuro, verifica prima della cattura) si controllano lì,
-- non con un CHECK, così anche una verifica fallita con l'orologio storto resta registrata.
--
-- Colonne:
--   - esito: 'ok' o 'fallita'; errore pieno solo per 'fallita';
--   - destinazione: 'cartella' (gestrip backup -cartella), 'chiavetta' (123) o 'sftp' (128, che da sola
--     non sblocca niente: risposta F del piano della T5.2);
--   - archivio: il percorso assoluto del file cifrato; sha256 e byte quelli letti dalla verifica;
--   - catturato_il: l'ora della copia del database (snapshot.json); verificato_il: la fine della verifica;
--   - conteggi: le righe per tabella della fotografia (snapshot.json), un oggetto JSON.
CREATE TABLE verifiche_backup (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  esito         TEXT    NOT NULL CHECK (esito IN ('ok', 'fallita')),
  destinazione  TEXT    NOT NULL CHECK (destinazione IN ('cartella', 'chiavetta', 'sftp')),
  archivio      TEXT    NOT NULL CHECK (length(archivio) > 1 AND substr(archivio, 1, 1) = '/'),
  sha256        TEXT    NOT NULL CHECK (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*'),
  byte          INTEGER NOT NULL CHECK (byte > 0),
  catturato_il  TEXT    NOT NULL CHECK (catturato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  verificato_il TEXT    NOT NULL CHECK (verificato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  conteggi      TEXT    NOT NULL CHECK (json_valid(conteggi) AND json_type(conteggi) = 'object'),
  errore        TEXT    CHECK ((esito = 'ok') = (errore IS NULL) AND (errore IS NULL OR errore <> ''))
) STRICT;

CREATE INDEX verifiche_backup_catturato ON verifiche_backup (esito, catturato_il);
