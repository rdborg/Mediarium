-- Release restrictions and preferred terms on quality profiles. Lists are
-- stored newline-separated; preferred terms as "term|score" lines.
ALTER TABLE quality_profiles ADD COLUMN must_contain     TEXT NOT NULL DEFAULT '';
ALTER TABLE quality_profiles ADD COLUMN must_not_contain TEXT NOT NULL DEFAULT '';
ALTER TABLE quality_profiles ADD COLUMN preferred        TEXT NOT NULL DEFAULT '';
