-- Registro delle azioni (ADM-17): le regole di audit_events dello standalone (migrazione
-- 20260905070000 e AuditEvent.changeset a go-baseline), con i nomi del Go:
--   - attore, azione, risorsa, id della risorsa ed esito obbligatori e non vuoti
--     (validate_required), con le lunghezze massime di validate_length;
--   - dati: un oggetto JSON, {} di default (field :metadata, :map, default: %{});
--   - ip e user agent facoltativi, al massimo 64 e 512 caratteri;
--   - AUTOINCREMENT: un id non si riusa mai, anche dopo la pulizia dei 5 anni (D-14).
-- Più rigido dal lato sicuro: istante nella forma fissa, così l'ordine del testo è quello del tempo.
CREATE TABLE eventi_audit (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  attore      TEXT NOT NULL CHECK (length(attore) BETWEEN 1 AND 255),
  azione      TEXT NOT NULL CHECK (length(azione) BETWEEN 1 AND 100),
  risorsa     TEXT NOT NULL CHECK (length(risorsa) BETWEEN 1 AND 100),
  id_risorsa  TEXT NOT NULL CHECK (length(id_risorsa) BETWEEN 1 AND 100),
  esito       TEXT NOT NULL CHECK (length(esito) BETWEEN 1 AND 40),
  dati        TEXT NOT NULL DEFAULT '{}' CHECK (json_valid(dati) AND json_type(dati) = 'object'),
  ip          TEXT CHECK (ip IS NULL OR length(ip) <= 64),
  user_agent  TEXT CHECK (user_agent IS NULL OR length(user_agent) <= 512),
  creato_il   TEXT NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z')
) STRICT;

CREATE INDEX eventi_audit_risorsa ON eventi_audit (risorsa, id_risorsa);
CREATE INDEX eventi_audit_creato ON eventi_audit (creato_il);
