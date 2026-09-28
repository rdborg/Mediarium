-- Phase 2/3: manual-import "always ask" conflict policy (PRD.md §4.8 —
-- "never delete/overwrite an existing file on a naming collision without
-- an explicit user-configured conflict policy: skip / overwrite if better
-- quality / always ask"). When the policy is "ask" and a naming collision
-- happens, the queue item parks in a new 'conflict' status with enough
-- state to resolve it later (source file still sitting in /downloads,
-- proposed destination) instead of silently skipping like the old
-- skip-only behavior did.

ALTER TABLE download_queue ADD COLUMN source_path TEXT;
ALTER TABLE download_queue ADD COLUMN dest_path TEXT;
