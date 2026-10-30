-- Schema finale dello standalone a go-baseline (6eb93ed), senza dati.
-- Generato da tools/schema-standalone/rigenera.sh: non modificare a mano.
-- schema_migrations la crea Ecto, non una migrazione: è aggiunta qui con la forma di ecto_sqlite3.

CREATE TABLE "schema_migrations" ("version" INTEGER PRIMARY KEY, "inserted_at" TEXT);

CREATE TABLE "artisan_reminders" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "artisan_reminders_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE RESTRICT, "artisan_id" INTEGER NOT NULL CONSTRAINT "artisan_reminders_artisan_id_fkey" REFERENCES "artisans"("id") ON DELETE RESTRICT, "handover_id" INTEGER CONSTRAINT "artisan_reminders_handover_id_fkey" REFERENCES "handovers"("id") ON DELETE RESTRICT, "actor" TEXT, "days_out" INTEGER NOT NULL, "inserted_at" TEXT NOT NULL);

CREATE TABLE "artisans" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "name" TEXT NOT NULL, "phone" TEXT);

CREATE TABLE "audit_events" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "actor" TEXT NOT NULL, "action" TEXT NOT NULL, "resource" TEXT NOT NULL, "resource_id" TEXT NOT NULL, "outcome" TEXT NOT NULL, "metadata" TEXT DEFAULT '{}' NOT NULL, "ip" TEXT, "user_agent" TEXT, "inserted_at" TEXT NOT NULL);

CREATE TABLE "bag_sequences" ("year" INTEGER, "last_number" INTEGER NOT NULL, PRIMARY KEY ("year"));

CREATE TABLE branding_assets (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      kind TEXT NOT NULL CONSTRAINT valid_branding_kind CHECK (kind IN ('logo', 'icon_192', 'icon_512')),
      storage_key TEXT NOT NULL CONSTRAINT valid_branding_storage_key CHECK (
        length(storage_key) = 64 AND storage_key NOT GLOB '*[^0-9a-f]*'
      ),
      original_filename TEXT NOT NULL CONSTRAINT nonempty_branding_filename CHECK (length(trim(original_filename)) > 0),
      mime_type TEXT NOT NULL CONSTRAINT valid_branding_mime CHECK (mime_type IN ('image/jpeg', 'image/png')),
      byte_size INTEGER NOT NULL CONSTRAINT valid_branding_size CHECK (byte_size BETWEEN 1 AND 16777216),
      sha256 BLOB NOT NULL CONSTRAINT valid_branding_digest CHECK (length(sha256) = 32),
      imported_at TEXT NOT NULL,
      CONSTRAINT branding_content_address CHECK (storage_key = lower(hex(sha256)))
    );

CREATE TABLE "courtesy_document_sequences" ("year" INTEGER CONSTRAINT "positive_courtesy_sequence_year" CHECK (year > 0), "last_number" INTEGER NOT NULL CONSTRAINT "positive_courtesy_sequence" CHECK (last_number > 0), PRIMARY KEY ("year"));

CREATE TABLE "customers" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "name" TEXT NOT NULL, "phone" TEXT NOT NULL, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);

CREATE TABLE delivery_signature_events (
      id           INTEGER PRIMARY KEY AUTOINCREMENT,
      repair_id    INTEGER NOT NULL CONSTRAINT delivery_signature_events_repair_id_fkey REFERENCES repairs(id) ON DELETE RESTRICT,
      event        TEXT NOT NULL CONSTRAINT valid_signature_event CHECK (event IN ('captured', 'reset')),
      storage_key  TEXT NOT NULL CONSTRAINT signature_event_names_an_object CHECK (length(storage_key) = 64),
      collected_by TEXT,
      actor        TEXT,
      occurred_at  TEXT NOT NULL,
      inserted_at  TEXT NOT NULL,
      updated_at   TEXT NOT NULL
    );

CREATE TABLE "dettati_addestramento" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "dettati_addestramento_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "testo" TEXT NOT NULL, "proposta" TEXT NOT NULL, "modello" TEXT, "inserted_at" TEXT NOT NULL);

CREATE TABLE "dettati_esclusioni" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "customer_id" INTEGER NOT NULL CONSTRAINT "dettati_esclusioni_customer_id_fkey" REFERENCES "customers"("id") ON DELETE CASCADE, "inserted_at" TEXT NOT NULL);

CREATE TABLE emergency_entries (
      id                 INTEGER PRIMARY KEY AUTOINCREMENT,
      emergency_event_id INTEGER NOT NULL CONSTRAINT emergency_entries_event_id_fkey REFERENCES emergency_events(id) ON DELETE RESTRICT,
      document_year      INTEGER NOT NULL,
      number             INTEGER NOT NULL,
      description        TEXT NOT NULL CONSTRAINT emergency_entry_has_description CHECK (length(trim(description)) > 0),
      gross_cents        INTEGER NOT NULL,
      vat_rate_percent   INTEGER NOT NULL,
      taxable_cents      INTEGER NOT NULL,
      vat_cents          INTEGER NOT NULL,
      payment_method     TEXT NOT NULL,
      customer_name      TEXT,
      repair_id          INTEGER CONSTRAINT emergency_entries_repair_id_fkey REFERENCES repairs(id) ON DELETE RESTRICT,
      reversal_of_id     INTEGER CONSTRAINT emergency_entries_reversal_of_id_fkey REFERENCES emergency_entries(id) ON DELETE RESTRICT,
      actor              TEXT,
      inserted_at        TEXT NOT NULL, charge_round INTEGER NOT NULL DEFAULT 0,
      CONSTRAINT emergency_entry_split_adds_up CHECK (taxable_cents + vat_cents = gross_cents)
    );

CREATE TABLE emergency_events (
      id              INTEGER PRIMARY KEY AUTOINCREMENT,
      started_at      TEXT NOT NULL,
      closed_at       TEXT,
      reason          TEXT NOT NULL,
      recorder_serial TEXT,
      notes           TEXT,
      opened_by       TEXT,
      closed_by       TEXT,
      open_lock       INTEGER NOT NULL DEFAULT 1,
      inserted_at     TEXT NOT NULL,
      updated_at      TEXT NOT NULL
    );

CREATE TABLE "handovers" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "artisan_id" INTEGER NOT NULL CONSTRAINT "handovers_artisan_id_fkey" REFERENCES "artisans"("id") ON DELETE RESTRICT, "closed_at" TEXT, "notes" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);

CREATE TABLE historical_documents (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      source_filename TEXT NOT NULL,
      storage_key TEXT NOT NULL,
      byte_size INTEGER NOT NULL CONSTRAINT positive_historical_document_size CHECK (byte_size > 0),
      sha256 BLOB NOT NULL,
      inserted_at TEXT NOT NULL
    );

CREATE TABLE "job_runs" (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  job           TEXT NOT NULL CONSTRAINT valid_job CHECK (job IN ('overdue', 'chases', 'briefing')),
  trigger       TEXT NOT NULL CONSTRAINT valid_trigger CHECK (trigger IN ('scheduled', 'manual')),
  outcome       TEXT NOT NULL CONSTRAINT valid_outcome CHECK (outcome IN ('ok', 'skipped', 'failed')),
  ran_at        TEXT NOT NULL,
  scheduled_for TEXT NOT NULL,
  notices       INTEGER NOT NULL DEFAULT 0 CONSTRAINT nonnegative_notices CHECK (notices >= 0),
  messages      INTEGER NOT NULL DEFAULT 0 CONSTRAINT nonnegative_messages CHECK (messages >= 0),
  detail        TEXT
);

CREATE TABLE legacy_import_captures (
      id INTEGER PRIMARY KEY CONSTRAINT singleton_legacy_import_capture CHECK (id = 1),
      source_size INTEGER NOT NULL CONSTRAINT positive_legacy_capture_size CHECK (source_size > 0),
      source_sha256 BLOB NOT NULL CONSTRAINT valid_legacy_capture_source_sha256 CHECK (length(source_sha256) = 32),
      sidecars_json TEXT NOT NULL CONSTRAINT nonempty_legacy_capture_sidecars CHECK (length(sidecars_json) > 0),
      capture_sha256 BLOB NOT NULL CONSTRAINT valid_legacy_capture_sha256 CHECK (length(capture_sha256) = 32),
      inserted_at TEXT NOT NULL
    );

CREATE TABLE legacy_import_mappings (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      source_table TEXT NOT NULL CONSTRAINT nonempty_legacy_source_table CHECK (length(trim(source_table)) > 0),
      source_id TEXT NOT NULL CONSTRAINT nonempty_legacy_source_id CHECK (length(trim(source_id)) > 0),
      target_table TEXT NOT NULL CONSTRAINT nonempty_legacy_target_table CHECK (length(trim(target_table)) > 0),
      target_id INTEGER NOT NULL CONSTRAINT positive_legacy_target_id CHECK (target_id > 0),
      inserted_at TEXT NOT NULL
    );

CREATE TABLE "media_deletion_jobs" ("storage_key" TEXT NOT NULL, "inserted_at" TEXT NOT NULL, PRIMARY KEY ("storage_key"));

CREATE TABLE "oreficerie" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "name" TEXT NOT NULL, "phone" TEXT);

CREATE TABLE "outgoing_messages" (
  id                 INTEGER PRIMARY KEY AUTOINCREMENT,
  repair_id          INTEGER REFERENCES repairs(id) ON DELETE SET NULL,
  channel            TEXT NOT NULL CONSTRAINT valid_channel CHECK (channel IN ('email', 'telegram', 'whatsapp')),
  party              TEXT NOT NULL CONSTRAINT valid_party CHECK (party IN ('shop', 'customer', 'oreficeria', 'artisan')),
  address            TEXT NOT NULL,
  subject            TEXT,
  body               TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'queued' CONSTRAINT valid_status CHECK (status IN ('queued', 'accepted', 'failed')),
  attempts           INTEGER NOT NULL DEFAULT 0 CONSTRAINT nonnegative_attempts CHECK (attempts >= 0),
  next_attempt_at    TEXT NOT NULL,
  queued_at          TEXT NOT NULL,
  accepted_at        TEXT,
  failed_at          TEXT,
  provider_reference TEXT,
  last_error         TEXT, "parse_mode" TEXT, "reply_markup" TEXT,
  CONSTRAINT handoff_is_accepted CHECK (channel <> 'whatsapp' OR status = 'accepted')
);

CREATE TABLE "password_resets" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL CONSTRAINT "password_resets_user_id_fkey" REFERENCES "users"("id") ON DELETE CASCADE, "token_hash" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "used_at" TEXT, "revoked_at" TEXT, "ip" TEXT, "channel" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);

CREATE TABLE "permanent_deletion_permits" ("family" TEXT NOT NULL, "resource_id" INTEGER NOT NULL, PRIMARY KEY ("family","resource_id"));

CREATE TABLE quote_decision_events (
      id          INTEGER PRIMARY KEY AUTOINCREMENT,
      repair_id   INTEGER NOT NULL CONSTRAINT quote_decision_events_repair_id_fkey REFERENCES repairs(id) ON DELETE RESTRICT,
      event       TEXT NOT NULL CONSTRAINT valid_quote_event CHECK (event IN ('approved', 'rejected', 'approval_revoked', 'rejection_revoked')),
      party       TEXT NOT NULL CONSTRAINT valid_quote_party CHECK (party IN ('customer', 'oreficeria', 'shop')),
      reason      TEXT,
      actor       TEXT,
      occurred_at TEXT NOT NULL,
      inserted_at TEXT NOT NULL,
      updated_at  TEXT NOT NULL,
      CONSTRAINT reason_belongs_to_a_refusal CHECK (event = 'rejected' OR reason IS NULL)
    );

CREATE TABLE ready_lots (
      id            INTEGER PRIMARY KEY AUTOINCREMENT,
      oreficeria_id INTEGER NOT NULL CONSTRAINT ready_lots_oreficeria_id_fkey REFERENCES oreficerie(id) ON DELETE RESTRICT,
      token         TEXT NOT NULL,
      sent_at       TEXT,
      closed_at     TEXT,
      inserted_at   TEXT NOT NULL,
      updated_at    TEXT NOT NULL, "public_access_disabled_at" TEXT,
      CONSTRAINT closed_requires_sent CHECK (closed_at IS NULL OR sent_at IS NOT NULL)
    );

CREATE TABLE ready_notice_events (
      id                  INTEGER PRIMARY KEY AUTOINCREMENT,
      repair_id           INTEGER NOT NULL CONSTRAINT ready_notice_events_repair_id_fkey REFERENCES repairs(id) ON DELETE RESTRICT,
      event               TEXT NOT NULL CONSTRAINT valid_event CHECK (event IN ('notified', 'reset')),
      channel             TEXT,
      outgoing_message_id INTEGER REFERENCES outgoing_messages(id) ON DELETE SET NULL,
      actor               TEXT,
      occurred_at         TEXT NOT NULL,
      inserted_at         TEXT NOT NULL,
      updated_at          TEXT NOT NULL,
      CONSTRAINT reset_carries_no_message CHECK (event = 'notified' OR outgoing_message_id IS NULL)
    );

CREATE TABLE "repair_messages" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "repair_messages_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE RESTRICT, "author" TEXT NOT NULL, "channel" TEXT, "body" TEXT NOT NULL, "sent_at" TEXT NOT NULL, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);

CREATE TABLE "repair_photos" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "repair_photos_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "position" INTEGER NOT NULL CONSTRAINT "positive_repair_photo_position" CHECK (position > 0), "storage_key" TEXT NOT NULL, "original_filename" TEXT NOT NULL, "mime_type" TEXT DEFAULT 'image/png' NOT NULL, "byte_size" INTEGER NOT NULL CONSTRAINT "positive_repair_photo_size" CHECK (byte_size > 0), "sha256" BLOB NOT NULL, "is_bag" INTEGER DEFAULT 0 NOT NULL, "vis_customer" INTEGER NOT NULL, "vis_artisan" INTEGER NOT NULL, "vis_orefice" INTEGER NOT NULL, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL, "retention_compressed_at" TEXT);

CREATE TABLE "repair_status_versions" ("repair_id" INTEGER CONSTRAINT "repair_status_versions_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "version" INTEGER NOT NULL, PRIMARY KEY ("repair_id"));

CREATE TABLE "repairs" (
  "id" INTEGER PRIMARY KEY AUTOINCREMENT,
  "customer_id" INTEGER NOT NULL CONSTRAINT "repairs_customer_id_fkey" REFERENCES "customers"("id") ON DELETE RESTRICT,
  "customer_name_snapshot" TEXT NOT NULL,
  "customer_phone_snapshot" TEXT NOT NULL,
  "bag_number" TEXT NOT NULL,
  "accepted_on" TEXT NOT NULL,
  "expected_on" TEXT,
  "ready_on" TEXT,
  "delivered_on" TEXT,
  "description" TEXT NOT NULL,
  "public_notes" TEXT,
  "internal_notes" TEXT,
  "status" TEXT DEFAULT 'in_lavorazione' NOT NULL,
  "estimate_cents" INTEGER CONSTRAINT nonnegative_estimate CHECK (estimate_cents IS NULL OR estimate_cents >= 0),
  "final_price_cents" INTEGER CONSTRAINT nonnegative_final_price CHECK (final_price_cents IS NULL OR final_price_cents >= 0),
  "public_token_hash" BLOB NOT NULL,
  "public_token_expires_at" TEXT NOT NULL,
  "public_token_revoked_at" TEXT,
  "inserted_at" TEXT NOT NULL,
  "updated_at" TEXT NOT NULL,
  "oreficeria_id" INTEGER CONSTRAINT "repairs_oreficeria_id_fkey" REFERENCES "oreficerie"("id") ON DELETE RESTRICT,
  "pinned_at" TEXT,
  "estimate_text" TEXT,
  "final_price_text" TEXT,
  "expected_on_text" TEXT,
  "compro_oro" INTEGER DEFAULT 0 NOT NULL, "attesa_cliente" INTEGER DEFAULT 0 NOT NULL, "attesa_dal" TEXT, "attesa_motivo" TEXT, "pinned_at_legacy_text" TEXT, "qr_code" TEXT, "artisan_id" INTEGER CONSTRAINT "repairs_artisan_id_fkey" REFERENCES "artisans"("id") ON DELETE RESTRICT, "handover_id" INTEGER CONSTRAINT "repairs_handover_id_fkey" REFERENCES "handovers"("id") ON DELETE RESTRICT, "handed_over_at" TEXT, "returned_from_artisan_at" TEXT, "artisan_notes" TEXT, "last_message_at" TEXT, "messages_read_at" TEXT, "messages_legacy_text" TEXT, "ready_notified_at" TEXT, "quote_approved_at" TEXT, "quote_rejected_at" TEXT, "quote_rejected_reason" TEXT, "ready_lot_id" INTEGER CONSTRAINT "repairs_ready_lot_id_fkey" REFERENCES "ready_lots"("id") ON DELETE RESTRICT, "signature_storage_key" TEXT, "signature_at" TEXT, "signature_collected_by" TEXT, "signature_byte_size" INTEGER, "overdue_notified_at" TEXT, "chase_notified_at" TEXT, "deleted_at" TEXT, "retention_started_at" TEXT, "legacy_public_code_hash" BLOB, "public_token" TEXT, "link_inviato_at" TEXT, "thread_visible_from" TEXT, "customer_email" TEXT, "email_confirmed_at" TEXT, "email_ready_sent_at" TEXT, "quote_approved_read_at" TEXT, "quote_rejected_read_at" TEXT, "legacy_public_revoked_at" TEXT, "extra_days" INTEGER DEFAULT 0 NOT NULL,
  CONSTRAINT valid_status CHECK (
    (compro_oro = 0 AND status IN ('in_lavorazione', 'preventivo', 'pronta', 'consegnata', 'in_attesa_materiali', 'annullata'))
    OR (compro_oro = 1 AND status IN ('in_valutazione', 'valutata', 'perfezionata', 'chiusa', 'annullata'))
  ),
  CONSTRAINT valid_compro_oro CHECK (compro_oro IN (0, 1))
);

CREATE TABLE sale_photos (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      sale_id INTEGER NOT NULL REFERENCES sales(id) ON DELETE CASCADE,
      position INTEGER NOT NULL CONSTRAINT positive_sale_photo_position CHECK (position > 0),
      storage_key TEXT NOT NULL,
      original_filename TEXT NOT NULL,
      mime_type TEXT NOT NULL,
      byte_size INTEGER NOT NULL CONSTRAINT positive_sale_photo_size CHECK (byte_size > 0),
      sha256 BLOB NOT NULL,
      inserted_at TEXT NOT NULL
    );

CREATE TABLE sale_revision_photos (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      sale_revision_id INTEGER NOT NULL REFERENCES sale_revisions(id) ON DELETE RESTRICT,
      position INTEGER NOT NULL CONSTRAINT positive_sale_revision_photo_position CHECK (position > 0),
      storage_key TEXT NOT NULL,
      original_filename TEXT NOT NULL,
      mime_type TEXT NOT NULL,
      byte_size INTEGER NOT NULL CONSTRAINT positive_sale_revision_photo_size CHECK (byte_size > 0),
      sha256 BLOB NOT NULL,
      inserted_at TEXT NOT NULL
    );

CREATE TABLE "sale_revision_revocations" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "sale_revision_id" INTEGER NOT NULL CONSTRAINT "sale_revision_revocations_sale_revision_id_fkey" REFERENCES "sale_revisions"("id") ON DELETE RESTRICT, "reason" TEXT NOT NULL, "revoked_at" TEXT NOT NULL);

CREATE TABLE "sale_revision_seals" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "sale_revision_id" INTEGER NOT NULL CONSTRAINT "sale_revision_seals_sale_revision_id_fkey" REFERENCES "sale_revisions"("id") ON DELETE RESTRICT, "sealed_at" TEXT NOT NULL);

CREATE TABLE "sale_revisions" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "sale_id" INTEGER NOT NULL CONSTRAINT "sale_revisions_sale_id_fkey" REFERENCES "sales"("id") ON DELETE RESTRICT, "previous_revision_id" INTEGER CONSTRAINT "sale_revisions_previous_revision_id_fkey" REFERENCES "sale_revisions"("id") ON DELETE RESTRICT, "revision_number" INTEGER NOT NULL CONSTRAINT "positive_sale_revision" CHECK (revision_number > 0), "document_year" INTEGER NOT NULL, "document_number" INTEGER NOT NULL CONSTRAINT "positive_sale_document_number" CHECK (document_number > 0), "sale_on" TEXT NOT NULL CONSTRAINT "valid_revision_sale_date_range" CHECK (sale_on BETWEEN '0001-01-01' AND '9987-12-31'), "channel" TEXT NOT NULL CONSTRAINT "valid_revision_channel" CHECK (channel IN ('eBay', 'Vinted', 'Etsy', 'Subito', 'Sito', 'Altro')), "item_description" TEXT NOT NULL, "serial_number" TEXT, "price_cents" INTEGER CONSTRAINT "nonnegative_revision_price" CHECK (price_cents IS NULL OR price_cents >= 0), "warranty_months" INTEGER NOT NULL CONSTRAINT "valid_revision_warranty" CHECK (warranty_months BETWEEN 0 AND 120), "warranty_start_on" TEXT NOT NULL, "warranty_end_on" TEXT NOT NULL CONSTRAINT "valid_revision_warranty_dates" CHECK (warranty_end_on >= warranty_start_on), "buyer_display_name" TEXT NOT NULL, "buyer_address" TEXT, "buyer_postal_code" TEXT, "buyer_city" TEXT, "buyer_province" TEXT, "buyer_email" TEXT, "buyer_phone" TEXT, "buyer_fiscal_id" TEXT, "seller_shop_name" TEXT NOT NULL, "seller_legal_name" TEXT, "seller_address" TEXT, "seller_postal_code" TEXT, "seller_city" TEXT, "seller_province" TEXT, "seller_email" TEXT, "seller_sales_email" TEXT, "seller_phone" TEXT, "seller_fiscal_id" TEXT, "warranty_terms" TEXT NOT NULL, "public_token_hash" BLOB NOT NULL, "public_token_expires_at" TEXT NOT NULL, "issued_at" TEXT NOT NULL, "inserted_at" TEXT NOT NULL, "price_text" TEXT);

CREATE TABLE "sales" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "sale_on" TEXT NOT NULL CONSTRAINT "valid_sale_date_range" CHECK (sale_on BETWEEN '0001-01-01' AND '9987-12-31'), "channel" TEXT NOT NULL CONSTRAINT "valid_sale_channel" CHECK (channel IN ('eBay', 'Vinted', 'Etsy', 'Subito', 'Sito', 'Altro')), "item_description" TEXT NOT NULL, "serial_number" TEXT, "price_cents" INTEGER CONSTRAINT "nonnegative_sale_price" CHECK (price_cents IS NULL OR price_cents >= 0), "warranty_months" INTEGER DEFAULT 24 NOT NULL CONSTRAINT "valid_warranty_months" CHECK (warranty_months BETWEEN 0 AND 120), "buyer_display_name" TEXT NOT NULL, "buyer_address" TEXT, "buyer_postal_code" TEXT, "buyer_city" TEXT, "buyer_province" TEXT, "buyer_email" TEXT, "buyer_phone" TEXT, "buyer_fiscal_id" TEXT, "internal_notes" TEXT, "status" TEXT DEFAULT 'draft' NOT NULL CONSTRAINT "valid_sale_status" CHECK (status IN ('draft', 'issued', 'returned', 'refunded', 'annulled')), "document_year" INTEGER CONSTRAINT "complete_document_identity" CHECK ((document_year IS NULL AND document_number IS NULL) OR (document_year IS NOT NULL AND document_number IS NOT NULL AND document_year > 0 AND document_number > 0)), "document_number" INTEGER, "issued_at" TEXT, "closed_on" TEXT CONSTRAINT "valid_sale_lifecycle" CHECK ((status = 'draft' AND document_year IS NULL AND document_number IS NULL AND issued_at IS NULL AND closed_on IS NULL AND closed_reason IS NULL) OR (status = 'issued' AND document_year IS NOT NULL AND document_number IS NOT NULL AND issued_at IS NOT NULL AND closed_on IS NULL AND closed_reason IS NULL) OR (status IN ('returned', 'refunded', 'annulled') AND document_year IS NOT NULL AND document_number IS NOT NULL AND issued_at IS NOT NULL AND closed_on IS NOT NULL AND closed_reason IS NOT NULL)), "closed_reason" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL, "legacy_id" INTEGER, "deleted_at" TEXT, "legacy_access_disabled_at" TEXT, "legacy_source_status" TEXT, "price_text" TEXT);

CREATE TABLE "settings" ("key" TEXT, "value" TEXT NOT NULL, "updated_at" TEXT NOT NULL, PRIMARY KEY ("key"));

CREATE TABLE "shop_profiles" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "scope" TEXT DEFAULT 'default' NOT NULL CONSTRAINT "shop_profiles_singleton_scope" CHECK (scope = 'default'), "shop_name" TEXT NOT NULL, "legal_name" TEXT, "address" TEXT, "postal_code" TEXT, "city" TEXT, "province" TEXT, "email" TEXT, "sales_email" TEXT, "phone" TEXT, "fiscal_id" TEXT, "warranty_terms" TEXT NOT NULL, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL, "subtitle" TEXT, "opening_hours" TEXT, "instagram" TEXT, "facebook" TEXT, "website" TEXT, "tax_code" TEXT);

CREATE TABLE "telegram_link_tokens" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "telegram_link_tokens_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "token_hash" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "inserted_at" TEXT NOT NULL);

CREATE TABLE "telegram_subscriptions" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "repair_id" INTEGER NOT NULL CONSTRAINT "telegram_subscriptions_repair_id_fkey" REFERENCES "repairs"("id") ON DELETE CASCADE, "chat_id" INTEGER NOT NULL, "consent" INTEGER DEFAULT 0 NOT NULL, "linked_at" TEXT NOT NULL, "consent_at" TEXT, "stopped_at" TEXT, "stop_reason" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);

CREATE TABLE "user_invitations" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL CONSTRAINT "user_invitations_user_id_fkey" REFERENCES "users"("id") ON DELETE CASCADE,
 "token_hash" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "accepted_at" TEXT, "revoked_at" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);

CREATE TABLE "users" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "email" TEXT NOT NULL, "password_hash" TEXT, "active" INTEGER DEFAULT 1 NOT NULL,
 "role" TEXT DEFAULT 'staff' NOT NULL CONSTRAINT "valid_user_role" CHECK (role IN ('owner', 'staff')),
 "display_name" TEXT, "last_login_at" TEXT, "deactivated_at" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL, "session_version" INTEGER DEFAULT 0 NOT NULL);

CREATE INDEX "artisan_reminders_artisan_id_index" ON "artisan_reminders" ("artisan_id");

CREATE INDEX "artisan_reminders_repair_id_index" ON "artisan_reminders" ("repair_id");

CREATE UNIQUE INDEX "artisans_name_index" ON "artisans" ("name");

CREATE INDEX "audit_events_inserted_at_index" ON "audit_events" ("inserted_at");

CREATE INDEX "audit_events_resource_resource_id_index" ON "audit_events" ("resource", "resource_id");

CREATE UNIQUE INDEX "branding_assets_kind_index" ON "branding_assets" ("kind");

CREATE UNIQUE INDEX "branding_assets_storage_key_index" ON "branding_assets" ("storage_key");

CREATE INDEX "customers_phone_index" ON "customers" ("phone");

CREATE UNIQUE INDEX "dettati_addestramento_repair_id_index" ON "dettati_addestramento" ("repair_id");

CREATE UNIQUE INDEX "dettati_esclusioni_customer_id_index" ON "dettati_esclusioni" ("customer_id");

CREATE UNIQUE INDEX emergency_entries_document_year_number_index
      ON emergency_entries(document_year, number);

CREATE INDEX emergency_entries_emergency_event_id_number_index
      ON emergency_entries(emergency_event_id, number);

CREATE UNIQUE INDEX emergency_entries_repair_id_charge_round_index
      ON emergency_entries(repair_id, charge_round)
      WHERE repair_id IS NOT NULL AND reversal_of_id IS NULL;

CREATE UNIQUE INDEX emergency_entries_reversal_of_id_index
      ON emergency_entries(reversal_of_id)
      WHERE reversal_of_id IS NOT NULL;

CREATE UNIQUE INDEX emergency_events_open_lock_index
      ON emergency_events(open_lock) WHERE closed_at IS NULL;

CREATE INDEX "handovers_artisan_id_index" ON "handovers" ("artisan_id");

CREATE UNIQUE INDEX historical_documents_source_filename_index ON historical_documents(source_filename);

CREATE INDEX historical_documents_storage_key_index ON historical_documents(storage_key);

CREATE INDEX idx_delivery_signature_events_repair
      ON delivery_signature_events(repair_id, id);

CREATE INDEX idx_job_runs_scheduled ON job_runs(job, trigger, scheduled_for);

CREATE INDEX idx_outgoing_messages_pending ON outgoing_messages(status, next_attempt_at);

CREATE INDEX idx_quote_decision_events_repair
      ON quote_decision_events(repair_id, id);

CREATE INDEX idx_ready_lots_oreficeria ON ready_lots(oreficeria_id, id);

CREATE INDEX idx_ready_notice_events_repair
      ON ready_notice_events(repair_id, id);

CREATE UNIQUE INDEX "legacy_import_mappings_source_table_source_id_index" ON "legacy_import_mappings" ("source_table","source_id");

CREATE UNIQUE INDEX "legacy_import_mappings_target_table_target_id_index" ON "legacy_import_mappings" ("target_table","target_id");

CREATE UNIQUE INDEX "oreficerie_name_index" ON "oreficerie" ("name");

CREATE INDEX "outgoing_messages_telegram_reply_map_index" ON "outgoing_messages" ("provider_reference", "address") WHERE channel = 'telegram' AND party = 'customer';

CREATE INDEX "password_resets_inserted_at_index" ON "password_resets" ("inserted_at");

CREATE UNIQUE INDEX "password_resets_token_hash_index" ON "password_resets" ("token_hash");

CREATE UNIQUE INDEX ready_lots_token_index ON ready_lots(token);

CREATE INDEX "repair_messages_repair_id_sent_at_index" ON "repair_messages" ("repair_id", "sent_at");

CREATE UNIQUE INDEX "repair_photos_repair_id_index" ON "repair_photos" ("repair_id") WHERE is_bag = 1;

CREATE UNIQUE INDEX "repair_photos_repair_id_position_index" ON "repair_photos" ("repair_id", "position");

CREATE INDEX "repair_photos_storage_key_index" ON "repair_photos" ("storage_key");

CREATE INDEX "repairs_artisan_id_index" ON "repairs" ("artisan_id");

CREATE UNIQUE INDEX "repairs_bag_number_index" ON "repairs" ("bag_number");

CREATE INDEX "repairs_customer_id_index" ON "repairs" ("customer_id");

CREATE INDEX "repairs_deleted_at_index" ON "repairs" ("deleted_at") WHERE deleted_at IS NOT NULL;

CREATE INDEX "repairs_handover_id_index" ON "repairs" ("handover_id");

CREATE INDEX "repairs_oreficeria_id_index" ON "repairs" ("oreficeria_id");

CREATE INDEX "repairs_pinned_at_index" ON "repairs" ("pinned_at");

CREATE UNIQUE INDEX "repairs_public_token_hash_index" ON "repairs" ("public_token_hash");

CREATE INDEX "repairs_ready_lot_id_index" ON "repairs" ("ready_lot_id");

CREATE INDEX "repairs_status_index" ON "repairs" ("status");

CREATE UNIQUE INDEX sale_photos_sale_id_position_index ON sale_photos(sale_id, position);

CREATE INDEX sale_photos_storage_key_index ON sale_photos(storage_key);

CREATE UNIQUE INDEX sale_revision_photos_sale_revision_id_position_index ON sale_revision_photos(sale_revision_id, position);

CREATE UNIQUE INDEX "sale_revision_revocations_sale_revision_id_index" ON "sale_revision_revocations" ("sale_revision_id");

CREATE UNIQUE INDEX "sale_revision_seals_sale_revision_id_index" ON "sale_revision_seals" ("sale_revision_id");

CREATE INDEX "sale_revisions_previous_revision_id_index" ON "sale_revisions" ("previous_revision_id");

CREATE UNIQUE INDEX "sale_revisions_public_token_hash_index" ON "sale_revisions" ("public_token_hash");

CREATE UNIQUE INDEX "sale_revisions_sale_id_revision_number_index" ON "sale_revisions" ("sale_id", "revision_number");

CREATE INDEX "sales_channel_index" ON "sales" ("channel");

CREATE INDEX "sales_deleted_at_index" ON "sales" ("deleted_at") WHERE deleted_at IS NOT NULL;

CREATE UNIQUE INDEX "sales_document_year_document_number_index" ON "sales" ("document_year", "document_number");

CREATE UNIQUE INDEX "sales_legacy_id_index" ON "sales" ("legacy_id") WHERE legacy_id IS NOT NULL;

CREATE INDEX "sales_sale_on_index" ON "sales" ("sale_on");

CREATE INDEX "sales_status_index" ON "sales" ("status");

CREATE UNIQUE INDEX "shop_profiles_scope_index" ON "shop_profiles" ("scope");

CREATE INDEX "telegram_link_tokens_repair_id_index" ON telegram_link_tokens(repair_id);

CREATE UNIQUE INDEX "telegram_link_tokens_token_hash_index" ON telegram_link_tokens(token_hash);

CREATE UNIQUE INDEX "telegram_subscriptions_chat_id_repair_id_index" ON telegram_subscriptions(chat_id, repair_id);

CREATE INDEX "telegram_subscriptions_repair_id_index" ON telegram_subscriptions(repair_id);

CREATE INDEX "telegram_subscriptions_stopped_at_index" ON telegram_subscriptions(stopped_at);

CREATE UNIQUE INDEX "user_invitations_token_hash_index" ON "user_invitations" ("token_hash");

CREATE INDEX "user_invitations_user_id_index" ON "user_invitations" ("user_id");

CREATE UNIQUE INDEX "users_email_index" ON "users" ("email") WHERE active = 1;

CREATE TRIGGER historical_documents_no_delete
    BEFORE DELETE ON historical_documents
    BEGIN
      SELECT RAISE(ABORT, 'historical documents are immutable');
    END;

CREATE TRIGGER historical_documents_no_update
    BEFORE UPDATE ON historical_documents
    BEGIN
      SELECT RAISE(ABORT, 'historical documents are immutable');
    END;

CREATE TRIGGER historical_documents_require_content_address
    BEFORE INSERT ON historical_documents
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'historical document storage key must match sha256');
    END;

CREATE TRIGGER legacy_import_capture_immutable_delete
    BEFORE DELETE ON legacy_import_captures
    BEGIN
      SELECT RAISE(ABORT, 'legacy import capture is immutable');
    END;

CREATE TRIGGER legacy_import_capture_immutable_update
    BEFORE UPDATE ON legacy_import_captures
    BEGIN
      SELECT RAISE(ABORT, 'legacy import capture is immutable');
    END;

CREATE TRIGGER repair_photos_keep_content_address
      BEFORE UPDATE ON repair_photos
      WHEN NEW.storage_key != lower(hex(NEW.sha256))
      BEGIN
        SELECT RAISE(ABORT, 'repair photo storage key must match sha256');
      END;

CREATE TRIGGER repair_photos_require_content_address
      BEFORE INSERT ON repair_photos
      WHEN NEW.storage_key != lower(hex(NEW.sha256))
      BEGIN
        SELECT RAISE(ABORT, 'repair photo storage key must match sha256');
      END;

CREATE TRIGGER repair_status_versions_bump
    AFTER UPDATE OF status ON repairs
    WHEN NEW.status IS NOT OLD.status
    BEGIN
      INSERT INTO repair_status_versions (repair_id, version) VALUES (NEW.id, 1)
      ON CONFLICT (repair_id) DO UPDATE SET version = version + 1;
    END;

CREATE TRIGGER repairs_legacy_public_code_hash_size_insert
    BEFORE INSERT ON repairs
    WHEN NEW.legacy_public_code_hash IS NOT NULL
      AND length(CAST(NEW.legacy_public_code_hash AS BLOB)) != 32
    BEGIN
      SELECT RAISE(ABORT, 'legacy public repair code hash must be 32 bytes');
    END;

CREATE TRIGGER repairs_legacy_public_code_hash_size_update
    BEFORE UPDATE OF legacy_public_code_hash ON repairs
    WHEN NEW.legacy_public_code_hash IS NOT NULL
      AND length(CAST(NEW.legacy_public_code_hash AS BLOB)) != 32
    BEGIN
      SELECT RAISE(ABORT, 'legacy public repair code hash must be 32 bytes');
    END;

CREATE TRIGGER sale_photos_keep_content_address
    BEFORE UPDATE ON sale_photos
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'sale photo storage key must match sha256');
    END;

CREATE TRIGGER sale_photos_require_content_address
    BEFORE INSERT ON sale_photos
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'sale photo storage key must match sha256');
    END;

CREATE TRIGGER sale_revision_photos_no_delete
    BEFORE DELETE ON sale_revision_photos
    WHEN NOT EXISTS (
      SELECT 1
      FROM sale_revisions revision
      JOIN permanent_deletion_permits permit
        ON permit.family = 'sale' AND permit.resource_id = revision.sale_id
      WHERE revision.id = OLD.sale_revision_id
    )
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photos are immutable');
    END;

CREATE TRIGGER sale_revision_photos_no_update
    BEFORE UPDATE ON sale_revision_photos
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photos are immutable');
    END;

CREATE TRIGGER sale_revision_photos_require_content_address
    BEFORE INSERT ON sale_revision_photos
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photo storage key must match sha256');
    END;

CREATE TRIGGER sale_revision_photos_require_open_revision
    BEFORE INSERT ON sale_revision_photos
    WHEN EXISTS (
      SELECT 1 FROM sale_revision_seals seal
      WHERE seal.sale_revision_id = NEW.sale_revision_id
    )
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photos are sealed');
    END;

CREATE TRIGGER sale_revision_revocations_no_delete BEFORE DELETE ON sale_revision_revocations
WHEN NOT EXISTS (SELECT 1 FROM sale_revisions revision JOIN permanent_deletion_permits permit ON permit.family = 'sale' AND permit.resource_id = revision.sale_id WHERE revision.id = OLD.sale_revision_id)
BEGIN SELECT RAISE(ABORT, 'sale revision revocations are immutable'); END;

CREATE TRIGGER sale_revision_revocations_no_update
      BEFORE UPDATE ON sale_revision_revocations
      BEGIN
        SELECT RAISE(ABORT, 'sale revision revocations are immutable');
      END;

CREATE TRIGGER sale_revision_seals_no_delete BEFORE DELETE ON sale_revision_seals
WHEN NOT EXISTS (SELECT 1 FROM sale_revisions revision JOIN permanent_deletion_permits permit ON permit.family = 'sale' AND permit.resource_id = revision.sale_id WHERE revision.id = OLD.sale_revision_id)
BEGIN SELECT RAISE(ABORT, 'sale revision seals are immutable'); END;

CREATE TRIGGER sale_revision_seals_no_update
      BEFORE UPDATE ON sale_revision_seals
      BEGIN
        SELECT RAISE(ABORT, 'sale revision seals are immutable');
      END;

CREATE TRIGGER sale_revisions_no_delete BEFORE DELETE ON sale_revisions
WHEN NOT EXISTS (SELECT 1 FROM permanent_deletion_permits permit WHERE permit.family = 'sale' AND permit.resource_id = OLD.sale_id)
BEGIN SELECT RAISE(ABORT, 'sale revisions are immutable'); END;

CREATE TRIGGER sale_revisions_no_update
    BEFORE UPDATE ON sale_revisions
    WHEN NOT EXISTS (
      SELECT 1 FROM permanent_deletion_permits permit
      WHERE permit.family = 'sale' AND permit.resource_id = OLD.sale_id
    )
    BEGIN
      SELECT RAISE(ABORT, 'sale revisions are immutable');
    END;

CREATE TRIGGER sale_revisions_validate_chain
    BEFORE INSERT ON sale_revisions
    WHEN
      (NEW.revision_number = 1 AND NEW.previous_revision_id IS NOT NULL) OR
      (NEW.revision_number > 1 AND (
        NEW.previous_revision_id IS NULL OR NOT EXISTS (
          SELECT 1 FROM sale_revisions previous
          JOIN sale_revision_seals seal ON seal.sale_revision_id = previous.id
          WHERE previous.id = NEW.previous_revision_id
            AND previous.sale_id = NEW.sale_id
            AND previous.revision_number = NEW.revision_number - 1
        )
      )) OR
      NEW.warranty_start_on != NEW.sale_on OR
      NOT EXISTS (
        SELECT 1 FROM sales sale
        WHERE sale.id = NEW.sale_id
          AND sale.status = 'issued'
          AND sale.document_year = NEW.document_year
          AND sale.document_number = NEW.document_number
          AND sale.sale_on = NEW.sale_on
          AND sale.channel = NEW.channel
          AND sale.item_description = NEW.item_description
          AND sale.serial_number IS NEW.serial_number
          AND sale.price_cents IS NEW.price_cents
          AND sale.price_text IS NEW.price_text
          AND sale.warranty_months = NEW.warranty_months
          AND sale.buyer_display_name = NEW.buyer_display_name
          AND sale.buyer_address IS NEW.buyer_address
          AND sale.buyer_postal_code IS NEW.buyer_postal_code
          AND sale.buyer_city IS NEW.buyer_city
          AND sale.buyer_province IS NEW.buyer_province
          AND sale.buyer_email IS NEW.buyer_email
          AND sale.buyer_phone IS NEW.buyer_phone
          AND sale.buyer_fiscal_id IS NEW.buyer_fiscal_id
      ) OR
      NOT EXISTS (
        SELECT 1 FROM shop_profiles profile
        WHERE profile.scope = 'default'
          AND profile.shop_name = NEW.seller_shop_name
          AND profile.legal_name IS NEW.seller_legal_name
          AND profile.address IS NEW.seller_address
          AND profile.postal_code IS NEW.seller_postal_code
          AND profile.city IS NEW.seller_city
          AND profile.province IS NEW.seller_province
          AND profile.email IS NEW.seller_email
          AND profile.sales_email IS NEW.seller_sales_email
          AND profile.phone IS NEW.seller_phone
          AND profile.fiscal_id IS NEW.seller_fiscal_id
          AND profile.warranty_terms = NEW.warranty_terms
      )
    BEGIN
      SELECT RAISE(ABORT, 'invalid sale revision chain');
    END;

CREATE TRIGGER sales_legacy_access_requires_seal_insert
      BEFORE INSERT ON sales
      WHEN NEW.legacy_id IS NOT NULL
        AND NEW.legacy_access_disabled_at IS NULL
        
        
        AND NOT EXISTS (
          SELECT 1
          FROM sale_revisions revision
          JOIN sale_revision_seals seal ON seal.sale_revision_id = revision.id
          WHERE revision.sale_id = NEW.id AND revision.revision_number = 1
        )
      BEGIN
        SELECT RAISE(ABORT, 'legacy sale access requires a sealed imported revision');
      END;

CREATE TRIGGER sales_legacy_access_requires_seal_update
      BEFORE UPDATE OF legacy_id, legacy_access_disabled_at ON sales
      WHEN NEW.legacy_id IS NOT NULL
        AND NEW.legacy_access_disabled_at IS NULL
        AND (OLD.legacy_access_disabled_at IS NOT NULL OR OLD.legacy_id IS NOT NEW.legacy_id)
        AND NOT EXISTS (SELECT 1 FROM sales existing WHERE existing.id != NEW.id AND existing.legacy_id = NEW.legacy_id)
        AND NOT EXISTS (
          SELECT 1
          FROM sale_revisions revision
          JOIN sale_revision_seals seal ON seal.sale_revision_id = revision.id
          WHERE revision.sale_id = NEW.id AND revision.revision_number = 1
        )
      BEGIN
        SELECT RAISE(ABORT, 'legacy sale access requires a sealed imported revision');
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

