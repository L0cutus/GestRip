CREATE TABLE repair_photos_carry AS SELECT * FROM repair_photos;
CREATE TABLE "repairs_rebuild" (
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
  "compro_oro" INTEGER DEFAULT 0 NOT NULL,
  CONSTRAINT valid_status CHECK (
    (compro_oro = 0 AND status IN ('in_lavorazione', 'preventivo', 'pronta', 'consegnata', 'in_attesa_materiali', 'annullata'))
    OR (compro_oro = 1 AND status IN ('in_valutazione', 'valutata', 'perfezionata', 'chiusa', 'annullata'))
  ),
  CONSTRAINT valid_compro_oro CHECK (compro_oro IN (0, 1))
);
INSERT INTO repairs_rebuild (id) SELECT id FROM repairs WHERE 0;
DROP TABLE repairs;
ALTER TABLE "repairs_rebuild" RENAME TO "repairs";
INSERT INTO repair_photos SELECT * FROM repair_photos_carry;
DROP TABLE repair_photos_carry;
CREATE UNIQUE INDEX "repairs_bag_number_index" ON "repairs" ("bag_number");
CREATE UNIQUE INDEX "repairs_public_token_hash_index" ON "repairs" ("public_token_hash");
CREATE INDEX "repairs_customer_id_index" ON "repairs" ("customer_id");
CREATE INDEX "repairs_status_index" ON "repairs" ("status");
CREATE INDEX "repairs_oreficeria_id_index" ON "repairs" ("oreficeria_id");
CREATE INDEX "repairs_pinned_at_index" ON "repairs" ("pinned_at");
