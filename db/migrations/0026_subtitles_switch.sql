-- The subtitles master switch (settings key subtitles.enabled).
--
-- From this version subtitles are off unless a person switches them on. An
-- install that was already using them keeps working: when there is any sign
-- of that, the switch is set to on, once, here. Nothing else changes.
--
-- Signs of use: the person saved their own OpenSubtitles key or account, turned
-- on automatic downloading, marked a title as not needing subtitles, or a
-- subtitle was looked for or downloaded before.
--
-- A value the person already set is never touched, and a new install (which
-- has none of these) is left with the switch unset, which reads as off.
INSERT INTO settings (key, value, encrypted)
SELECT 'subtitles.enabled', '1', 0
WHERE NOT EXISTS (SELECT 1 FROM settings WHERE key = 'subtitles.enabled')
  AND (
       EXISTS (SELECT 1 FROM settings WHERE key = 'subtitles.opensubtitles_api_key' AND value <> '')
    OR EXISTS (SELECT 1 FROM settings WHERE key = 'subtitles.opensubtitles_username' AND value <> '')
    OR EXISTS (SELECT 1 FROM settings WHERE key = 'subtitles.auto_download' AND value = '1')
    OR EXISTS (SELECT 1 FROM subtitle_downloads)
    OR EXISTS (SELECT 1 FROM subtitle_attempts)
    OR EXISTS (SELECT 1 FROM subtitle_dismissed)
    OR EXISTS (SELECT 1 FROM activity WHERE event_type = 'subtitle' AND message LIKE 'Downloaded % subtitle for %')
  );
