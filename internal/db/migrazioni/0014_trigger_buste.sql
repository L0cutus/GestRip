-- I trigger della tabella buste che lo standalone ha e il Go porta con il cambio di stato (slice 030).

-- Il contatore dei cambi di stato di una busta (RIP-S-13), come il trigger
-- repair_status_versions_bump dello standalone (20261013120000): lo tiene il database, così conta
-- chiunque scriva lo stato (la scheda, la lista, il QR, un lotto, una riapertura), e una scrittura
-- che lascia lo stato com'è non conta. Una riga per busta cambiata almeno una volta; nessuna riga
-- vale 0. L'import porta le righe come dati e scrive le buste con INSERT, che il trigger non vede.
-- Una migrazione che ricostruisce la tabella buste deve rifare il trigger.
CREATE TRIGGER versioni_stato_buste_conta
AFTER UPDATE OF stato ON buste
WHEN NEW.stato IS NOT OLD.stato
BEGIN
  INSERT INTO versioni_stato_buste (busta_id, versione) VALUES (NEW.id, 1)
  ON CONFLICT (busta_id) DO UPDATE SET versione = versione + 1;
END;

-- Lo spegnimento delle iscrizioni Telegram, come i tre trigger della migrazione 20261012150000
-- dello standalone (telegram_subscriptions_off_when_closed, _trashed, _b2b), con i nomi del Go:
-- una busta consegnata, annullata o chiusa, messa nel cestino o passata a un'oreficeria esterna
-- (id > 1) da qualunque scrittura spegne le sue iscrizioni ancora attive, toglie i token non
-- usati del link t.me e scrive una riga d'audit per iscrizione con la busta e il motivo, mai la
-- chat. Non tocca la coda in uscita. L'istante è quello di SQLite, come nello standalone, nella
-- forma di db.FormatoIstante. L'import scrive con INSERT, che questi trigger non vedono.
CREATE TRIGGER iscrizioni_telegram_spente_alla_chiusura
AFTER UPDATE OF stato ON buste
WHEN NEW.stato IN ('consegnata', 'annullata', 'chiusa')
BEGIN
  INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il)
  SELECT 'system', 'repair.telegram_switched_off', 'repair', CAST(NEW.id AS TEXT), 'ok',
         json_object('bag_number', NEW.numero, 'subscription_id', iscrizione.id,
                     'reason', NEW.stato),
         strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
  FROM iscrizioni_telegram iscrizione
  WHERE iscrizione.busta_id = NEW.id AND iscrizione.fermata_il IS NULL;

  UPDATE iscrizioni_telegram
  SET consenso = 0, fermata_il = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z',
      motivo_fermata = NEW.stato,
      aggiornato_il = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
  WHERE busta_id = NEW.id AND fermata_il IS NULL;

  DELETE FROM token_telegram WHERE busta_id = NEW.id;
END;

CREATE TRIGGER iscrizioni_telegram_spente_nel_cestino
AFTER UPDATE OF cancellata_il ON buste
WHEN NEW.cancellata_il IS NOT NULL
BEGIN
  INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il)
  SELECT 'system', 'repair.telegram_switched_off', 'repair', CAST(NEW.id AS TEXT), 'ok',
         json_object('bag_number', NEW.numero, 'subscription_id', iscrizione.id,
                     'reason', 'deleted'),
         strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
  FROM iscrizioni_telegram iscrizione
  WHERE iscrizione.busta_id = NEW.id AND iscrizione.fermata_il IS NULL;

  UPDATE iscrizioni_telegram
  SET consenso = 0, fermata_il = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z',
      motivo_fermata = 'deleted',
      aggiornato_il = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
  WHERE busta_id = NEW.id AND fermata_il IS NULL;

  DELETE FROM token_telegram WHERE busta_id = NEW.id;
END;

-- Oreficeria.is_b2b/1: un id oltre 1 è un'oreficeria, 1 il negozio e NULL il cliente.
CREATE TRIGGER iscrizioni_telegram_spente_per_oreficeria
AFTER UPDATE OF oreficeria_id ON buste
WHEN NEW.oreficeria_id > 1
BEGIN
  INSERT INTO eventi_audit (attore, azione, risorsa, id_risorsa, esito, dati, creato_il)
  SELECT 'system', 'repair.telegram_switched_off', 'repair', CAST(NEW.id AS TEXT), 'ok',
         json_object('bag_number', NEW.numero, 'subscription_id', iscrizione.id,
                     'reason', 'oreficeria'),
         strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
  FROM iscrizioni_telegram iscrizione
  WHERE iscrizione.busta_id = NEW.id AND iscrizione.fermata_il IS NULL;

  UPDATE iscrizioni_telegram
  SET consenso = 0, fermata_il = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z',
      motivo_fermata = 'oreficeria',
      aggiornato_il = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
  WHERE busta_id = NEW.id AND fermata_il IS NULL;

  DELETE FROM token_telegram WHERE busta_id = NEW.id;
END;
