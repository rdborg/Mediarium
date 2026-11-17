-- Album and book releases that failed on import used to be blocklisted
-- without their album or book, so they could not be limited to that title
-- later. Tie each such entry to its album or book where the download it came
-- from is still on record: a download of that release for the album or book,
-- or the album's "Blocklisted" event. An entry that matches no title, or more
-- than one, is left as it is (blocked everywhere), as is every entry a person
-- blocked for a download that was for no title.
WITH loose AS (
    SELECT id, release_title, title_key FROM blocklist
    WHERE scope = 'all' AND movie_id IS NULL AND series_id IS NULL AND album_id IS NULL AND book_id IS NULL
),
owners AS (
    SELECT l.id AS entry_id, q.album_id AS album_id, NULL AS book_id
    FROM loose l JOIN download_queue q ON q.album_id IS NOT NULL AND lower(trim(q.release_title)) = l.title_key
    UNION
    SELECT l.id, NULL, q.book_id
    FROM loose l JOIN download_queue q ON q.book_id IS NOT NULL AND lower(trim(q.release_title)) = l.title_key
    UNION
    SELECT l.id, a.album_id, NULL
    FROM loose l JOIN activity a ON a.album_id IS NOT NULL AND a.event_type = 'blocklisted'
        AND substr(a.message, 1, length('Blocklisted "' || l.release_title || '": ')) = 'Blocklisted "' || l.release_title || '": '
),
single AS (
    SELECT entry_id, MAX(album_id) AS album_id, MAX(book_id) AS book_id
    FROM owners GROUP BY entry_id HAVING COUNT(*) = 1
)
UPDATE blocklist
SET album_id = (SELECT album_id FROM single WHERE single.entry_id = blocklist.id),
    book_id  = (SELECT book_id FROM single WHERE single.entry_id = blocklist.id)
WHERE id IN (SELECT entry_id FROM single);
