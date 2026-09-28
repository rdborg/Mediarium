-- Notification targets grow up: which events each one hears about, and a
-- generic config (public fields as JSON, secret fields as encrypted JSON) so
-- email/ntfy/gotify/pushover/slack targets don't each need their own columns.
-- Existing targets keep receiving every event they always did.
ALTER TABLE notification_targets ADD COLUMN events TEXT NOT NULL DEFAULT 'added,imported,failed,conflict,subtitle,health';
ALTER TABLE notification_targets ADD COLUMN config_json TEXT NOT NULL DEFAULT '';
ALTER TABLE notification_targets ADD COLUMN secrets_encrypted TEXT NOT NULL DEFAULT '';
