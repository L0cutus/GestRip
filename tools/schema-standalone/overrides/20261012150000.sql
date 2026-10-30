CREATE TABLE "telegram_subscriptions" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "telegram_subscriptions_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "chat_id" INTEGER NOT NULL, "consent" INTEGER DEFAULT 0 NOT NULL, "linked_at" TEXT NOT NULL, "consent_at" TEXT, "stopped_at" TEXT, "stop_reason" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);
CREATE UNIQUE INDEX "telegram_subscriptions_chat_id_repair_id_index" ON telegram_subscriptions(chat_id, repair_id);
CREATE INDEX "telegram_subscriptions_repair_id_index" ON telegram_subscriptions(repair_id);
CREATE INDEX "telegram_subscriptions_stopped_at_index" ON telegram_subscriptions(stopped_at);
CREATE TABLE "telegram_link_tokens" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "telegram_link_tokens_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "token_hash" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "inserted_at" TEXT NOT NULL);
CREATE UNIQUE INDEX "telegram_link_tokens_token_hash_index" ON telegram_link_tokens(token_hash);
CREATE INDEX "telegram_link_tokens_repair_id_index" ON telegram_link_tokens(repair_id);
CREATE TRIGGER telegram_subscriptions_off_when_closed
    AFTER UPDATE OF status ON repairs
    WHEN NEW.status IN ('consegnata', 'annullata', 'chiusa')
    BEGIN
      INSERT INTO audit_events (actor, action, resource, resource_id, outcome, metadata, inserted_at)
      SELECT 'system', 'repair.telegram_switched_off', 'repair', CAST(NEW.id AS TEXT), 'ok',
             json_object('bag_number', NEW.bag_number, 'subscription_id', subscription.id,
                         'reason', NEW.status),
             strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
      FROM telegram_subscriptions subscription
      WHERE subscription.repair_id = NEW.id AND subscription.stopped_at IS NULL;

      UPDATE telegram_subscriptions
      SET consent = 0, stopped_at = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z', stop_reason = NEW.status, updated_at = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
      WHERE repair_id = NEW.id AND stopped_at IS NULL;

      DELETE FROM telegram_link_tokens WHERE repair_id = NEW.id;
    END;
CREATE TRIGGER telegram_subscriptions_off_when_trashed
    AFTER UPDATE OF deleted_at ON repairs
    WHEN NEW.deleted_at IS NOT NULL
    BEGIN
      INSERT INTO audit_events (actor, action, resource, resource_id, outcome, metadata, inserted_at)
      SELECT 'system', 'repair.telegram_switched_off', 'repair', CAST(NEW.id AS TEXT), 'ok',
             json_object('bag_number', NEW.bag_number, 'subscription_id', subscription.id,
                         'reason', 'deleted'),
             strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
      FROM telegram_subscriptions subscription
      WHERE subscription.repair_id = NEW.id AND subscription.stopped_at IS NULL;

      UPDATE telegram_subscriptions
      SET consent = 0, stopped_at = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z', stop_reason = 'deleted', updated_at = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
      WHERE repair_id = NEW.id AND stopped_at IS NULL;

      DELETE FROM telegram_link_tokens WHERE repair_id = NEW.id;
    END;
CREATE TRIGGER telegram_subscriptions_off_when_b2b
    AFTER UPDATE OF oreficeria_id ON repairs
    WHEN NEW.oreficeria_id > 1
    BEGIN
      INSERT INTO audit_events (actor, action, resource, resource_id, outcome, metadata, inserted_at)
      SELECT 'system', 'repair.telegram_switched_off', 'repair', CAST(NEW.id AS TEXT), 'ok',
             json_object('bag_number', NEW.bag_number, 'subscription_id', subscription.id,
                         'reason', 'oreficeria'),
             strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
      FROM telegram_subscriptions subscription
      WHERE subscription.repair_id = NEW.id AND subscription.stopped_at IS NULL;

      UPDATE telegram_subscriptions
      SET consent = 0, stopped_at = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z', stop_reason = 'oreficeria', updated_at = strftime('%Y-%m-%dT%H:%M:%S', 'now') || '.' || substr(strftime('%f', 'now'), 4) || '000Z'
      WHERE repair_id = NEW.id AND stopped_at IS NULL;

      DELETE FROM telegram_link_tokens WHERE repair_id = NEW.id;
    END;
