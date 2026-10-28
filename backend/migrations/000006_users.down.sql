-- admin_users was never changed by the up migration, so it still holds the
-- legacy admins.
DROP TABLE IF EXISTS users;
