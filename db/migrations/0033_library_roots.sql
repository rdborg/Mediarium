-- More than one library folder per kind: the folder a movie or show is kept
-- in, when it is not the main one. '' = the main movies or TV folder.
ALTER TABLE movies ADD COLUMN root_path TEXT NOT NULL DEFAULT '';
ALTER TABLE series ADD COLUMN root_path TEXT NOT NULL DEFAULT '';
