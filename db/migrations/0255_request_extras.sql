-- Request extras: an age limit per account, and requests to remove a title.
--   max_rating     the most mature rating the account may ask for or add: a
--                  certification such as PG-13 or TV-14, or an age such as 12.
--                  Empty = no limit.
--   block_unrated  1 = titles that have no rating are refused too.
--   action         what a request is for: add (all earlier ones) or remove.
ALTER TABLE users ADD COLUMN max_rating TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN block_unrated INTEGER NOT NULL DEFAULT 0;
ALTER TABLE requests ADD COLUMN action TEXT NOT NULL DEFAULT 'add';
