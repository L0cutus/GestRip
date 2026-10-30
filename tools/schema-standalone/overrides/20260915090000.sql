CREATE TABLE "users_rebuild" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "email" TEXT NOT NULL, "password_hash" TEXT, "active" INTEGER DEFAULT 1 NOT NULL,
 "role" TEXT DEFAULT 'staff' NOT NULL CONSTRAINT "valid_user_role" CHECK (role IN ('owner', 'staff')),
 "display_name" TEXT, "last_login_at" TEXT, "deactivated_at" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);
DROP TABLE users;
ALTER TABLE users_rebuild RENAME TO users;
CREATE UNIQUE INDEX "users_email_index" ON "users" ("email") WHERE active = 1;
CREATE TABLE "user_invitations" ("id" INTEGER PRIMARY KEY AUTOINCREMENT, "user_id" INTEGER NOT NULL CONSTRAINT "user_invitations_user_id_fkey" REFERENCES "users"("id") ON DELETE CASCADE,
 "token_hash" BLOB NOT NULL, "expires_at" TEXT NOT NULL, "accepted_at" TEXT, "revoked_at" TEXT, "inserted_at" TEXT NOT NULL, "updated_at" TEXT NOT NULL);
CREATE UNIQUE INDEX "user_invitations_token_hash_index" ON "user_invitations" ("token_hash");
CREATE INDEX "user_invitations_user_id_index" ON "user_invitations" ("user_id");
