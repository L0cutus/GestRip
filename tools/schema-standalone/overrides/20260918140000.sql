ALTER TABLE sale_photos RENAME TO sale_photos_six_position_limit;
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
INSERT INTO sale_photos
      (id, sale_id, position, storage_key, original_filename, mime_type, byte_size, sha256, inserted_at)
    SELECT id, sale_id, position, storage_key, original_filename, mime_type, byte_size, sha256, inserted_at
    FROM sale_photos_six_position_limit;
DROP TABLE sale_photos_six_position_limit;
CREATE INDEX sale_photos_storage_key_index ON sale_photos(storage_key);
CREATE UNIQUE INDEX sale_photos_sale_id_position_index ON sale_photos(sale_id, position);
CREATE TRIGGER sale_photos_require_content_address
    BEFORE INSERT ON sale_photos
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'sale photo storage key must match sha256');
    END;
CREATE TRIGGER sale_photos_keep_content_address
    BEFORE UPDATE ON sale_photos
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'sale photo storage key must match sha256');
    END;
ALTER TABLE sale_revision_photos RENAME TO sale_revision_photos_six_position_limit;
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
INSERT INTO sale_revision_photos
      (id, sale_revision_id, position, storage_key, original_filename, mime_type, byte_size, sha256, inserted_at)
    SELECT id, sale_revision_id, position, storage_key, original_filename, mime_type, byte_size, sha256, inserted_at
    FROM sale_revision_photos_six_position_limit;
DROP TABLE sale_revision_photos_six_position_limit;
CREATE UNIQUE INDEX sale_revision_photos_sale_revision_id_position_index ON sale_revision_photos(sale_revision_id, position);
CREATE TRIGGER sale_revision_photos_no_update
    BEFORE UPDATE ON sale_revision_photos
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photos are immutable');
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
CREATE TRIGGER sale_revision_photos_require_open_revision
    BEFORE INSERT ON sale_revision_photos
    WHEN EXISTS (
      SELECT 1 FROM sale_revision_seals seal
      WHERE seal.sale_revision_id = NEW.sale_revision_id
    )
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photos are sealed');
    END;
CREATE TRIGGER sale_revision_photos_require_content_address
    BEFORE INSERT ON sale_revision_photos
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'sale revision photo storage key must match sha256');
    END;
CREATE TABLE historical_documents (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      source_filename TEXT NOT NULL,
      storage_key TEXT NOT NULL,
      byte_size INTEGER NOT NULL CONSTRAINT positive_historical_document_size CHECK (byte_size > 0),
      sha256 BLOB NOT NULL,
      inserted_at TEXT NOT NULL
    );
CREATE UNIQUE INDEX historical_documents_source_filename_index ON historical_documents(source_filename);
CREATE INDEX historical_documents_storage_key_index ON historical_documents(storage_key);
CREATE TRIGGER historical_documents_require_content_address
    BEFORE INSERT ON historical_documents
    WHEN NEW.storage_key != lower(hex(NEW.sha256))
    BEGIN
      SELECT RAISE(ABORT, 'historical document storage key must match sha256');
    END;
CREATE TRIGGER historical_documents_no_update
    BEFORE UPDATE ON historical_documents
    BEGIN
      SELECT RAISE(ABORT, 'historical documents are immutable');
    END;
CREATE TRIGGER historical_documents_no_delete
    BEFORE DELETE ON historical_documents
    BEGIN
      SELECT RAISE(ABORT, 'historical documents are immutable');
    END;
