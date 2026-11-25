-- Lo storico delle esecuzioni dei backup di gestrip serve (slice 124, T5.2, D-115): il lavoro notturno e
-- «Esegui adesso» del titolare. Solo Go (importer.TabelleSoloGo): lo standalone fa il backup fuori dal
-- pacchetto (OPS-28, su otto-vps) e non ha uno storico.
--
-- Perché una tabella nuova e non le due che ci sono già:
--   - verifiche_backup (slice 122) ha una riga solo quando un archivio è stato scritto e verificato: un
--     backup che non parte (chiavetta tolta, chiave dei backup che manca, spazio) non lascerebbe traccia, e il
--     lavoro notturno non saprebbe di averci già provato (riproverebbe ogni minuto);
--   - esecuzioni_lavori (slice 094) viene dallo standalone (job_runs), il suo CHECK ammette solo i tre lavori
--     dello standalone e la pagina «Promemoria e job», confrontata dal banco, mostra le sue righe.
--
-- Una riga per esecuzione arrivata in fondo, mai modificata; uno stop di serve a metà non lascia righe (il
-- turno resta da fare). Il ripristino (130) la svuota con il registro dei backup.
--
-- Colonne:
--   - avvio: 'automatico' (il lavoro notturno) o 'a_mano' («Esegui adesso»);
--   - turno: il giorno del negozio dell'ora del backup a cui l'esecuzione appartiene (aaaa-mm-gg): alle 01:00
--     è ancora il turno di ieri; per «a mano» lo stesso calcolo, ma conta solo per gli automatici;
--   - iniziato_il, finito_il: istanti UTC;
--   - esito: 'ok' (verificato e registrato), 'avviso' (il backup è buono ma la rotazione non è andata) o
--     'fallito'; errore solo per 'avviso' e 'fallito', al massimo 1000 byte;
--   - archivio: il file scritto, se Cifra è arrivata in fondo.
CREATE TABLE esecuzioni_backup (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  avvio       TEXT    NOT NULL CHECK (avvio IN ('automatico', 'a_mano')),
  turno       TEXT    NOT NULL CHECK (turno GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  iniziato_il TEXT    NOT NULL CHECK (iniziato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  finito_il   TEXT    NOT NULL CHECK (finito_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  esito       TEXT    NOT NULL CHECK (esito IN ('ok', 'avviso', 'fallito')),
  errore      TEXT    CHECK ((esito = 'ok') = (errore IS NULL) AND (errore IS NULL OR (errore <> '' AND length(CAST(errore AS BLOB)) <= 1000))),
  archivio    TEXT    CHECK (archivio IS NULL OR (length(archivio) > 1 AND substr(archivio, 1, 1) = '/'))
) STRICT;

CREATE INDEX esecuzioni_backup_turno ON esecuzioni_backup (avvio, turno);
