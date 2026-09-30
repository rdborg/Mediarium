-- Definition-based ("Cardigann") indexers: sites without a Newznab/Torznab
-- API, run from a community definition plus the settings it asks for.
--
-- kind:                'newznab', 'torznab' or 'cardigann'
-- settings_json:       the definition's non-secret settings (JSON object)
-- secrets_encrypted:   its passwords, cookies and keys (encrypted JSON object)
-- last_test_error:     message of the last failed Test ('' when it passed)
-- last_test_at:        when Test last ran
ALTER TABLE indexers ADD COLUMN kind TEXT NOT NULL DEFAULT '';
ALTER TABLE indexers ADD COLUMN settings_json TEXT NOT NULL DEFAULT '';
ALTER TABLE indexers ADD COLUMN secrets_encrypted TEXT;
ALTER TABLE indexers ADD COLUMN last_test_error TEXT NOT NULL DEFAULT '';
ALTER TABLE indexers ADD COLUMN last_test_at TEXT NOT NULL DEFAULT '';

UPDATE indexers SET kind = CASE WHEN protocol = 'torrent' THEN 'torznab' ELSE 'newznab' END WHERE kind = '';
