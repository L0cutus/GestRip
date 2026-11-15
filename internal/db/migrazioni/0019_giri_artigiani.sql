-- I giri dall'artigiano (slice 081, ART-19, tema H). Lo standalone non li tiene in una tabella:
-- HandoverHistory.cycles/1 li ricostruisce a ogni lettura dall'audit (un giro passato = una riga
-- repair.returned_from_artisan o repair.returned_from_artisan_auto; la sua uscita = l'ultima
-- handover.confirmed della stessa consegna che nomina la busta in repair_ids e non è dopo il rientro;
-- se manca, l'uscita è sconosciuta). Nel Go l'audit si cancella dopo 5 anni (D-14), e con lui
-- sparirebbero i giri vecchi: qui la stessa ricostruzione diventa una tabella, scritta da chi scrive
-- quelle righe d'audit (conferma della consegna, rientro, rientro automatico dal QR).
--
-- Attenzione: questo file svuota giri_artigiani e la rifà dall'audit. Rieseguirlo a mano è sicuro solo
-- prima di qualsiasi pulizia dell'audit (D-14): dopo, cancellerebbe per sempre i giri vecchi che la
-- tabella deve conservare. Migra non lo riesegue (schema_migrations).
--
-- Righe:
--   - un giro chiuso per ogni rientro (rientro_il pieno): consegna e nota presente come nella riga
--     d'audit (la consegna è un numero e basta, senza chiave esterna: resta anche quando la consegna
--     esce dallo storico, come nell'audit); sganciata = il titolare l'ha tolta a mano da quella
--     consegna chiusa (handover.bag_detached), per le «riaperte» della 082;
--   - un giro aperto (rientro_il vuoto) per ogni busta timbrata da una conferma: la sua uscita è
--     quella che il rientro copierà. Il giro in corso sulla scheda resta quello delle colonne della
--     busta, come nello standalone; i giri aperti non si mostrano mai (uno rimasto da una busta
--     riaperta prima del rientro non è un giro, come nello standalone che non ha la riga di rientro).
-- Solo del Go (TabelleSoloGo): l'import la ricostruisce dopo aver copiato audit e buste
-- (buste.RicostruisciGiri, con queste stesse istruzioni).
--
-- La migrazione si può rieseguire: tabella e indici solo se mancano, e la ricostruzione parte da
-- vuota. Su un database nuovo non trova niente; su uno già importato o già in uso legge l'audit.
CREATE TABLE IF NOT EXISTS giri_artigiani (
  id                    INTEGER PRIMARY KEY AUTOINCREMENT,
  busta_id              INTEGER NOT NULL REFERENCES buste (id) ON DELETE CASCADE,
  consegna_artigiano_id INTEGER,
  uscita_il             TEXT    CHECK (uscita_il IS NULL OR uscita_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  rientro_il            TEXT    CHECK (rientro_il IS NULL OR rientro_il GLOB '[0-9][0-9][0-9][0-9]-[0-1][0-9]-[0-3][0-9]T[0-2][0-9]:[0-5][0-9]:[0-6][0-9].[0-9][0-9][0-9][0-9][0-9][0-9]Z'),
  nota_presente         INTEGER NOT NULL DEFAULT 0 CHECK (nota_presente IN (0, 1)),
  sganciata             INTEGER NOT NULL DEFAULT 0 CHECK (sganciata IN (0, 1)),
  CHECK (uscita_il IS NULL OR rientro_il IS NULL OR uscita_il <= rientro_il),
  CHECK (rientro_il IS NOT NULL OR (nota_presente = 0 AND sganciata = 0))
) STRICT;

CREATE INDEX IF NOT EXISTS giri_artigiani_busta ON giri_artigiani (busta_id);
CREATE INDEX IF NOT EXISTS giri_artigiani_consegna ON giri_artigiani (consegna_artigiano_id);

DELETE FROM giri_artigiani;

-- I giri chiusi, uno per rientro, nell'ordine dell'audit (= buste.sqlRientriNeiGiri + l'ordine).
INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il, rientro_il, nota_presente, sganciata)
SELECT r.busta_id, r.consegna_id,
  coalesce(
    (SELECT g.uscita_il FROM giri_artigiani g
      WHERE g.busta_id = r.busta_id AND g.rientro_il IS NULL AND g.consegna_artigiano_id IS r.consegna_id
        AND g.uscita_il <= r.creato_il
      ORDER BY g.uscita_il DESC, g.id DESC LIMIT 1),
    (SELECT max(c.creato_il) FROM eventi_audit c, json_each(c.dati, '$.repair_ids') j
      WHERE c.azione = 'handover.confirmed' AND c.risorsa = 'handover' AND c.id_risorsa = CAST(r.consegna_id AS TEXT)
        AND json_type(c.dati, '$.repair_ids') = 'array' AND j.type = 'integer' AND j.value = r.busta_id
        AND c.creato_il <= r.creato_il)),
  r.creato_il, r.nota,
  EXISTS (SELECT 1 FROM eventi_audit s
    WHERE s.azione = 'handover.bag_detached' AND s.risorsa = 'handover' AND s.id_risorsa = CAST(r.consegna_id AS TEXT)
      AND json_type(s.dati, '$.repair_id') = 'integer' AND json_extract(s.dati, '$.repair_id') = r.busta_id)
FROM (SELECT e.id, e.creato_il, b.id AS busta_id,
    CASE WHEN json_type(e.dati, '$.handover_id') = 'integer' THEN json_extract(e.dati, '$.handover_id') END AS consegna_id,
    json_type(e.dati, '$.artisan_notes') IS 'true' AS nota
  FROM eventi_audit e JOIN buste b ON b.id = CAST(e.id_risorsa AS INTEGER) AND CAST(b.id AS TEXT) = e.id_risorsa
  WHERE e.azione IN ('repair.returned_from_artisan', 'repair.returned_from_artisan_auto') AND e.risorsa = 'repair'
) r ORDER BY r.creato_il, r.id;

-- I giri aperti delle buste fuori adesso (= buste.sqlGiriAperti): l'uscita che il loro rientro copierà.
INSERT INTO giri_artigiani (busta_id, consegna_artigiano_id, uscita_il)
SELECT b.id, b.consegna_artigiano_id,
  (SELECT max(c.creato_il) FROM eventi_audit c, json_each(c.dati, '$.repair_ids') j
    WHERE c.azione = 'handover.confirmed' AND c.risorsa = 'handover' AND c.id_risorsa = CAST(b.consegna_artigiano_id AS TEXT)
      AND json_type(c.dati, '$.repair_ids') = 'array' AND j.type = 'integer' AND j.value = b.id)
FROM buste b WHERE b.consegnata_artigiano_il IS NOT NULL AND b.rientrata_artigiano_il IS NULL ORDER BY b.id;
