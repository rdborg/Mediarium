-- Quality fallback chain: profile ids (a JSON array, in order) that an
-- automatic search tries when nothing it found is acceptable to the profile
-- itself. Empty for every profile until the user opts in.
ALTER TABLE quality_profiles ADD COLUMN fallback TEXT NOT NULL DEFAULT '[]';
