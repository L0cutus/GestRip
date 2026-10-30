CREATE TABLE "permanent_deletion_permits" ("family" TEXT NOT NULL, "resource_id" INTEGER NOT NULL, PRIMARY KEY ("family","resource_id"));
DROP TRIGGER sale_revisions_no_delete;
CREATE TRIGGER sale_revisions_no_delete BEFORE DELETE ON sale_revisions
WHEN NOT EXISTS (SELECT 1 FROM permanent_deletion_permits permit WHERE permit.family = 'sale' AND permit.resource_id = OLD.sale_id)
BEGIN SELECT RAISE(ABORT, 'sale revisions are immutable'); END;
DROP TRIGGER sale_revision_photos_no_delete;
CREATE TRIGGER sale_revision_photos_no_delete BEFORE DELETE ON sale_revision_photos
WHEN NOT EXISTS (SELECT 1 FROM sale_revisions revision JOIN permanent_deletion_permits permit ON permit.family = 'sale' AND permit.resource_id = revision.sale_id WHERE revision.id = OLD.sale_revision_id)
BEGIN SELECT RAISE(ABORT, 'sale revision photos are immutable'); END;
DROP TRIGGER sale_revision_revocations_no_delete;
CREATE TRIGGER sale_revision_revocations_no_delete BEFORE DELETE ON sale_revision_revocations
WHEN NOT EXISTS (SELECT 1 FROM sale_revisions revision JOIN permanent_deletion_permits permit ON permit.family = 'sale' AND permit.resource_id = revision.sale_id WHERE revision.id = OLD.sale_revision_id)
BEGIN SELECT RAISE(ABORT, 'sale revision revocations are immutable'); END;
DROP TRIGGER sale_revision_seals_no_delete;
CREATE TRIGGER sale_revision_seals_no_delete BEFORE DELETE ON sale_revision_seals
WHEN NOT EXISTS (SELECT 1 FROM sale_revisions revision JOIN permanent_deletion_permits permit ON permit.family = 'sale' AND permit.resource_id = revision.sale_id WHERE revision.id = OLD.sale_revision_id)
BEGIN SELECT RAISE(ABORT, 'sale revision seals are immutable'); END;
