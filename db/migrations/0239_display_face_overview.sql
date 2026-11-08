-- The plot shown with a stored title and poster, in that info language.
-- NULL means the background job has not asked for it yet (rows saved before
-- this column existed). An empty string means TMDB had no plot, so it is not
-- asked again. The English plot stays on the movie or series row.
ALTER TABLE display_faces ADD COLUMN overview TEXT;
