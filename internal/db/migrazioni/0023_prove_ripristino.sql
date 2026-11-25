-- La prova di ripristino settimanale (slice 125, T5.2, D-15, risposta G del piano) e i media rovinati
-- (D-116). Solo Go (importer.TabelleSoloGo): lo standalone fa la prova su otto-vps, fuori dal pacchetto
-- (OPS-28), e non controlla il contenuto dei media contro il loro nome.
--
-- 1. prove_ripristino: una riga per ogni prova arrivata in fondo, mai modificata. Perché una tabella nuova e
--    non le due del backup:
--      - verifiche_backup (122) è il registro che sblocca le cancellazioni (D-11) e che la rotazione (123)
--        legge riga per riga: una seconda riga 'ok' per lo stesso archivio cambierebbe chi è «l'ultimo
--        verificato» e farebbe righe doppie nella rotazione; una prova fallita perché l'archivio è vecchio non
--        è una verifica di un archivio;
--      - esecuzioni_backup (124): il CHECK di avvio e il conteggio dei turni sono del backup; una prova non
--        scrive archivi, e cambiare il CHECK vorrebbe ricostruire la tabella.
--    Uno stop di serve a metà non lascia righe (la settimana resta da fare). Il ripristino (130) la svuota
--    con il registro dei backup.
--    Colonne:
--      - avvio: 'automatico' (il lavoro notturno) o 'a_mano' (gestrip prova-backup);
--      - settimana: la domenica (aaaa-mm-gg) del turno del backup a cui la prova appartiene; per «a mano»
--        lo stesso calcolo, ma conta solo per gli automatici;
--      - iniziato_il, finito_il: istanti UTC;
--      - esito: 'ok' o 'fallita'; errore solo per 'fallita', al massimo 1000 byte;
--      - archivio, sha256, catturato_il: l'archivio provato (l'ultimo verificato sulla chiavetta del
--        registro), se il registro ne aveva uno;
--      - media_rovinati: i media il cui contenuto non ha lo SHA-256 del loro nome (D-116), contati dalla
--        verifica: 0 se la verifica non è arrivata in fondo; se la verifica è passata e poi la prova è fallita
--        (per esempio SHA-256 o ora di cattura diversi dal registro) la riga 'fallita' porta il numero vero.
--        Non fanno fallire la prova.
CREATE TABLE prove_ripristino (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  avvio          TEXT    NOT NULL CHECK (avvio IN ('automatico', 'a_mano')),
  settimana      TEXT    NOT NULL CHECK (settimana GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]'),
  iniziato_il    TEXT    NOT NULL CHECK (iniziato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  finito_il      TEXT    NOT NULL CHECK (finito_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  esito          TEXT    NOT NULL CHECK (esito IN ('ok', 'fallita')),
  errore         TEXT    CHECK ((esito = 'ok') = (errore IS NULL) AND (errore IS NULL OR (errore <> '' AND length(CAST(errore AS BLOB)) <= 1000))),
  archivio       TEXT    CHECK (archivio IS NULL OR (length(archivio) > 1 AND substr(archivio, 1, 1) = '/')),
  sha256         TEXT    CHECK (sha256 IS NULL OR (length(sha256) = 64 AND sha256 NOT GLOB '*[^0-9a-f]*')),
  catturato_il   TEXT    CHECK (catturato_il IS NULL OR catturato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  media_rovinati INTEGER NOT NULL DEFAULT 0 CHECK (media_rovinati >= 0),
  CHECK (esito = 'fallita' OR (archivio IS NOT NULL AND sha256 IS NOT NULL AND catturato_il IS NOT NULL))
) STRICT;

CREATE INDEX prove_ripristino_settimana ON prove_ripristino (avvio, settimana);

-- 2. D-116 (Stefano, 25/11, scelta A): la verifica di ogni backup (121) controlla anche che il contenuto di
--    ogni media abbia lo SHA-256 del suo nome. Un media rovinato non fa fallire la verifica (la riga resta
--    'ok', vale per D-11 e per la rotazione): si annota qui, e la 126 ne fa banner e avvisi.
--      - media_rovinati: quanti;
--      - media_rovinati_nomi: i primi 100 nomi (le chiavi, 64 cifre esadecimali), un array JSON in ordine;
--        una riga di prima della 125 ha 0 e '[]'.
ALTER TABLE verifiche_backup ADD COLUMN media_rovinati INTEGER NOT NULL DEFAULT 0 CHECK (media_rovinati >= 0);
ALTER TABLE verifiche_backup ADD COLUMN media_rovinati_nomi TEXT NOT NULL DEFAULT '[]'
  CHECK (json_valid(media_rovinati_nomi) AND json_type(media_rovinati_nomi) = 'array'
         AND json_array_length(media_rovinati_nomi) <= media_rovinati AND json_array_length(media_rovinati_nomi) <= 100);
