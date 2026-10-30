-- Utenti: le regole di `users` dello standalone (migrazioni 20260905070000, 20260907000000,
-- 20260915090000, 20261002120000 a go-baseline), con i nomi del Go; l'import traduce owner e
-- staff in titolare e dipendente, gli istanti nella forma db.FormatoIstante e le email in
-- minuscolo (attenzione alle collisioni tra maiuscole e minuscole).
--   - AUTOINCREMENT: un id non si riusa mai (l'audit nomina gli utenti per numero); l'import
--     porta sqlite_sequence a max(seq dello standalone, max id), come rebuild_users;
--   - hash_password nullo finché l'invito non è accettato («in attesa»), mai vuoto;
--   - email unica solo tra gli account attivi (app_users_bp.py:41): chi è uscito si reinvita
--     e la riga vecchia resta. Le query la cercano con `attivo = 1` (così usano l'indice);
--   - versione_sessione: «chiudi tutte» la aumenta e le sessioni vecchie non valgono più.
-- Più rigido dello standalone, dal lato sicuro: email non vuota, già in minuscolo e senza spazi
-- ai lati (la normalizza il codice, come il changeset); istanti nella forma fissa, così
-- l'ordine del testo è l'ordine del tempo.
CREATE TABLE utenti (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  email             TEXT    NOT NULL CHECK (length(email) > 0 AND email = lower(email) AND email = trim(email)),
  hash_password     TEXT    CHECK (hash_password IS NULL OR length(hash_password) > 0),
  attivo            INTEGER NOT NULL DEFAULT 1 CHECK (attivo IN (0, 1)),
  ruolo             TEXT    NOT NULL DEFAULT 'dipendente' CHECK (ruolo IN ('titolare', 'dipendente')),
  versione_sessione INTEGER NOT NULL DEFAULT 0 CHECK (versione_sessione >= 0),
  nome_visibile     TEXT,
  ultimo_accesso_il TEXT    CHECK (ultimo_accesso_il IS NULL OR ultimo_accesso_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  disattivato_il    TEXT    CHECK (disattivato_il IS NULL OR disattivato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  creato_il         TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  aggiornato_il     TEXT    NOT NULL CHECK (aggiornato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE UNIQUE INDEX utenti_email_attivi ON utenti (email) WHERE attivo = 1;

-- Segreti del server, a parte da impostazioni: un export, una pagina che elenca le impostazioni
-- o un import di impostazioni non li tocca mai (db.TabelleSegrete). La chiave del server firma
-- sessioni e CSRF: chi ha una copia del database può firmarli, quindi il backup è una
-- credenziale, e un ripristino isolato o una macchina di prova la rinnova (RinnovaChiaveServer).
CREATE TABLE segreti (
  nome      TEXT PRIMARY KEY,
  valore    BLOB NOT NULL CHECK (length(valore) = 32 AND valore <> zeroblob(32)),
  creato_il TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;
