CREATE TABLE legacy_import_mappings (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      source_table TEXT NOT NULL CONSTRAINT nonempty_legacy_source_table CHECK (length(trim(source_table)) > 0),
      source_id TEXT NOT NULL CONSTRAINT nonempty_legacy_source_id CHECK (length(trim(source_id)) > 0),
      target_table TEXT NOT NULL CONSTRAINT nonempty_legacy_target_table CHECK (length(trim(target_table)) > 0),
      target_id INTEGER NOT NULL CONSTRAINT positive_legacy_target_id CHECK (target_id > 0),
      inserted_at TEXT NOT NULL
    );
CREATE UNIQUE INDEX "legacy_import_mappings_source_table_source_id_index" ON "legacy_import_mappings" ("source_table","source_id");
CREATE UNIQUE INDEX "legacy_import_mappings_target_table_target_id_index" ON "legacy_import_mappings" ("target_table","target_id");
ALTER TABLE ready_lots ADD COLUMN "public_access_disabled_at" TEXT;
ALTER TABLE sales ADD COLUMN "legacy_access_disabled_at" TEXT;
ALTER TABLE sales ADD COLUMN "legacy_source_status" TEXT;
ALTER TABLE sales ADD COLUMN "price_text" TEXT;
ALTER TABLE sale_revisions ADD COLUMN "price_text" TEXT;
DROP TRIGGER IF EXISTS sale_revisions_validate_chain;
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
CREATE UNIQUE INDEX "branding_assets_kind_index" ON "branding_assets" ("kind");
CREATE UNIQUE INDEX "branding_assets_storage_key_index" ON "branding_assets" ("storage_key");
