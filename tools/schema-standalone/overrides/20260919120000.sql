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
