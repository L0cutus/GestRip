-- Gli invii riusciti della coda delle foto di «Nuova riparazione» (slice 044b, D-58). La coda tiene
-- le foto nel browser (IndexedDB) e le manda da qualunque pagina interna, anche dopo una ricarica:
-- una foto salita mentre la pagina cambiava può partire di nuovo. Ogni foto della coda ha un
-- identificativo casuale (16 byte, 22 caratteri base64url, intestazione x-upload-id): qui la busta,
-- l'identificativo e la foto che il primo invio riuscito ha salvato, scritti nella stessa transazione
-- della foto. Un nuovo invio con lo stesso identificativo per la stessa busta risponde con quella
-- foto, senza crearne un'altra né scrivere l'audit.
--
-- La traccia resta quando la foto si toglie (ON DELETE SET NULL, foto_id vuoto): un invio la cui
-- risposta si è persa e la cui foto è stata poi tolta dalla scheda risponde «già fatto» (201 con
-- photo_id null) e la foto non torna. Se ne va con la busta (cestino definitivo). Solo del Go: lo
-- standalone non ha la tabella, l'import la lascia vuota (TabelleSoloGo).
CREATE TABLE invii_foto (
  busta_id       INTEGER NOT NULL REFERENCES buste (id) ON DELETE CASCADE,
  identificativo TEXT    NOT NULL CHECK (length(identificativo) = 22 AND identificativo NOT GLOB '*[^A-Za-z0-9_-]*'),
  foto_id        INTEGER REFERENCES foto_buste (id) ON DELETE SET NULL,
  creato_il      TEXT    NOT NULL CHECK (creato_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  PRIMARY KEY (busta_id, identificativo)
) STRICT;

CREATE INDEX invii_foto_foto ON invii_foto (foto_id);
