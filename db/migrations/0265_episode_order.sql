-- Episode order per show (#184): which list a show's regular seasons follow.
-- 'tmdb' (the default, as it always was) takes the episodes as the movie
-- database counts them; 'tvdb' takes TheTVDB's aired order, which counts
-- every double-length episode as two, as many people's files do (The Office).
-- Needs a saved TheTVDB key. Existing shows stay on 'tmdb'.
ALTER TABLE series ADD COLUMN episode_order TEXT NOT NULL DEFAULT 'tmdb';
