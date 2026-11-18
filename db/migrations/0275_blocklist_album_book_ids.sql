-- Album and book releases that failed on import used to be blocklisted
-- without their album or book, so they could not be limited to that title
-- later. Tie each such entry to its album or book where the download it came
-- from is still on record: a download of that release for the album or book,
-- or the album's "Blocklisted" event. An entry that matches no title, or more
-- than one, is left as it is (blocked everywhere), as is every entry a person
-- blocked for a download that was for no title.
-- The work is done through small indexed temporary tables: the downloads and
-- events are looked up by release name for every entry, and on a library with a
-- long blocklist and years of history, looking through them again for each entry
-- took minutes. A release's event is found by a range on the start of the
-- message, which the index on the message answers directly.
CREATE TEMP TABLE m0275_loose AS
    SELECT id, release_title, title_key FROM blocklist
    WHERE scope = 'all' AND movie_id IS NULL AND series_id IS NULL AND album_id IS NULL AND book_id IS NULL;
CREATE TEMP TABLE m0275_album_dl AS
    SELECT lower(trim(release_title)) AS k, album_id FROM download_queue WHERE album_id IS NOT NULL;
CREATE INDEX temp.m0275_album_dl_k ON m0275_album_dl (k);
CREATE TEMP TABLE m0275_book_dl AS
    SELECT lower(trim(release_title)) AS k, book_id FROM download_queue WHERE book_id IS NOT NULL;
CREATE INDEX temp.m0275_book_dl_k ON m0275_book_dl (k);
CREATE TEMP TABLE m0275_events AS
    SELECT message, album_id FROM activity WHERE album_id IS NOT NULL AND event_type = 'blocklisted';
CREATE INDEX temp.m0275_events_message ON m0275_events (message);

CREATE TEMP TABLE m0275_single (entry_id INTEGER PRIMARY KEY, album_id INTEGER, book_id INTEGER);
INSERT INTO m0275_single (entry_id, album_id, book_id)
WITH owners AS (
    SELECT l.id AS entry_id, d.album_id AS album_id, NULL AS book_id
    FROM m0275_loose l JOIN m0275_album_dl d ON d.k = l.title_key
    UNION
    SELECT l.id, NULL, d.book_id
    FROM m0275_loose l JOIN m0275_book_dl d ON d.k = l.title_key
    UNION
    SELECT l.id, e.album_id, NULL
    FROM m0275_loose l JOIN m0275_events e
        ON e.message >= 'Blocklisted "' || l.release_title || '": '
       AND e.message <  'Blocklisted "' || l.release_title || '": ' || char(1114111)
)
SELECT entry_id, MAX(album_id), MAX(book_id) FROM owners GROUP BY entry_id HAVING COUNT(*) = 1;

UPDATE blocklist
SET album_id = (SELECT album_id FROM m0275_single WHERE entry_id = blocklist.id),
    book_id  = (SELECT book_id FROM m0275_single WHERE entry_id = blocklist.id)
WHERE id IN (SELECT entry_id FROM m0275_single);

DROP TABLE m0275_single;
DROP TABLE m0275_events;
DROP TABLE m0275_book_dl;
DROP TABLE m0275_album_dl;
DROP TABLE m0275_loose;
