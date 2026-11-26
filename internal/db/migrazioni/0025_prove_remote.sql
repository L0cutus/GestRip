-- La prova mensile della copia remota (slice 129c, T5.2, OPS-28, risposta E del piano: «una volta al mese un
-- download completo di notte con la verifica intera»). Solo Go (importer.TabelleSoloGo): lo standalone controlla la
-- copia fuori sede su otto-vps (restic), fuori dal pacchetto.
--
-- prove_remote: una riga per ogni prova arrivata in fondo (riuscita o no), mai modificata. La prova scarica dal
-- server SFTP l'ultima copia remota del registro dei backup (riga «sftp» 'ok') in una cartella temporanea del volume
-- dei dati, ne confronta byte e SHA-256 con il registro e la verifica per intera (121). Uno stop di serve a metà non
-- lascia righe (il mese resta da fare). Perché una tabella nuova e non prove_ripristino (125):
--   - prove_ripristino ha una settimana, non un mese, e nessun codice fisso: la prova remota ne vuole uno (gli avvisi
--     della 127 fanno il testo dal codice, mai dal motivo grezzo, che qui potrebbe avere la cartella del server);
--   - mettere le due prove nella stessa tabella vorrebbe una colonna «tipo» e un filtro in ogni lettura della 125 e
--     della 126 (calendario, ultima prova, allarme «prova settimanale fallita»): una lettura dimenticata farebbe
--     spegnere l'allarme della chiavetta da una prova remota riuscita; e il CHECK della 0023 non si cambia senza
--     ricostruire la tabella.
-- Il ripristino (130) la svuota con il registro dei backup e copie_remote.
-- Colonne:
--   - avvio: 'automatico' (il lavoro notturno) o 'a_mano' (gestrip prova-backup -remota);
--   - mese: il mese (aaaa-mm) della domenica della settimana del turno del backup (backup.MeseDi): la prova del mese
--     è dovuta dalla prima domenica del mese;
--   - turno: il giorno del negozio (aaaa-mm-gg) del turno del backup della prova: un mese non ancora riuscito si
--     riprova ogni notte, con al più tre tentativi per turno (revisione della 129c);
--   - iniziato_il, finito_il: istanti UTC;
--   - esito: 'ok' o 'fallita'; codice: perché non è 'ok', una parola fissa (backup.CodiceRemoto…); errore: il motivo
--     grezzo, al più 1000 byte, solo per la pagina Backup;
--   - archivio, sha256, byte, catturato_il: la copia remota provata (percorso sul server), se il registro ne aveva una
--     (in una prova riuscita sempre);
--   - media_rovinati: i media rovinati (D-116) trovati dalla verifica, 0 se non è arrivata in fondo.
CREATE TABLE prove_remote (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  avvio          TEXT    NOT NULL CHECK (avvio IN ('automatico', 'a_mano')),
  mese           TEXT    NOT NULL CHECK (mese GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]'),
  turno          TEXT    NOT NULL CHECK (turno GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  iniziato_il    TEXT    NOT NULL CHECK (iniziato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  finito_il      TEXT    NOT NULL CHECK (finito_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  esito          TEXT    NOT NULL CHECK (esito IN ('ok', 'fallita')),
  codice         TEXT    CHECK ((esito = 'ok') = (codice IS NULL) AND (codice IS NULL OR codice IN ('configurazione',
                   'chiave', 'rete', 'impronta', 'accesso', 'cartella', 'spazio', 'controllo', 'altro', 'nessuna',
                   'vecchia', 'orologio', 'assente', 'scaricamento', 'verifica'))),
  errore         TEXT    CHECK ((esito = 'ok') = (errore IS NULL) AND (errore IS NULL OR (errore <> '' AND length(CAST(errore AS BLOB)) <= 1000))),
  archivio       TEXT    CHECK (archivio IS NULL OR (length(archivio) > 1 AND substr(archivio, 1, 1) = '/')),
  sha256         TEXT    CHECK (sha256 IS NULL OR (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
  byte           INTEGER CHECK (byte IS NULL OR byte > 0),
  catturato_il   TEXT    CHECK (catturato_il IS NULL OR catturato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  media_rovinati INTEGER NOT NULL DEFAULT 0 CHECK (media_rovinati >= 0),
  CHECK ((archivio IS NULL) = (sha256 IS NULL) AND (archivio IS NULL) = (byte IS NULL) AND (archivio IS NULL) = (catturato_il IS NULL)),
  CHECK (esito = 'fallita' OR archivio IS NOT NULL)
) STRICT;

CREATE INDEX prove_remote_mese ON prove_remote (avvio, mese, turno);
