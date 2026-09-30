-- Phase 3: notification targets ("Discord/Telegram/
-- webhook/email on events... generic, not Discord-only").

CREATE TABLE notification_targets (
    id                  INTEGER PRIMARY KEY AUTOINCREMENT,
    name                TEXT NOT NULL,
    type                TEXT NOT NULL, -- webhook | discord | telegram
    url                 TEXT,          -- webhook/discord
    bot_token_encrypted TEXT,          -- telegram
    chat_id             TEXT,          -- telegram
    enabled             INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
