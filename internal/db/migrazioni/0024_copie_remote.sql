-- La copia remota dei backup su SFTP (slice 128, T5.2, OPS-16 destinazione remota, §8.2 n. 15, risposte E e F del
-- piano). Solo Go (importer.TabelleSoloGo): lo standalone copia i backup fuori sede con restic da otto-vps, fuori
-- dal pacchetto.
--
-- copie_remote: una riga per ogni copia remota arrivata in fondo (riuscita o no), mai modificata. La scrive il giro
-- del backup (backup.Giro) dopo un backup riuscito sulla chiavetta, con la copia remota configurata (impostazione
-- backup.remoto). Uno stop di serve a metà non lascia righe. Perché una tabella nuova e non le altre del backup:
--   - verifiche_backup (122) ha una riga solo per un archivio scritto e controllato: una copia che non parte
--     (server che non risponde, impronta diversa) non ha un archivio remoto da registrare, ma lo stato dei backup
--     (126) e gli avvisi (127) devono saperlo;
--   - esecuzioni_backup (124) non la scrive il comando gestrip backup, e il suo esito è quello del backup sulla
--     chiavetta: un fallimento remoto non deve farlo sembrare un backup fallito.
-- Il ripristino (130) la svuota con il registro dei backup e spegne la copia remota (risposta I).
-- Colonne:
--   - avvio: 'automatico' (il lavoro notturno) o 'a_mano' («Esegui adesso» e il comando gestrip backup);
--   - iniziato_il, finito_il: istanti UTC;
--   - esito: 'ok' (caricata, controllata, registrata e ruotata), 'avviso' (copia buona e registrata, rotazione
--     remota non riuscita), 'fallita';
--   - codice: perché non è 'ok', una parola fissa (backup.Codice…: gli avvisi della 127 ne fanno il testo, mai
--     dal motivo grezzo); errore: il motivo grezzo, al più 1000 byte, solo per la pagina Backup;
--   - archivio: il percorso dell'archivio sul server, se il caricamento è arrivato alla rinomina.
CREATE TABLE copie_remote (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  avvio       TEXT    NOT NULL CHECK (avvio IN ('automatico', 'a_mano')),
  iniziato_il TEXT    NOT NULL CHECK (iniziato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  finito_il   TEXT    NOT NULL CHECK (finito_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  esito       TEXT    NOT NULL CHECK (esito IN ('ok', 'avviso', 'fallita')),
  codice      TEXT    CHECK ((esito = 'ok') = (codice IS NULL) AND (codice IS NULL OR codice IN ('configurazione',
                'chiave', 'rete', 'impronta', 'accesso', 'cartella', 'spazio', 'esistente', 'caricamento', 'controllo',
                'registro', 'rotazione', 'altro'))),
  errore      TEXT    CHECK ((esito = 'ok') = (errore IS NULL) AND (errore IS NULL OR (errore <> '' AND length(CAST(errore AS BLOB)) <= 1000))),
  archivio    TEXT    CHECK (archivio IS NULL OR (length(archivio) > 1 AND substr(archivio, 1, 1) = '/'))
) STRICT;
