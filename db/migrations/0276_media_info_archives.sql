-- Files whose content wasn't recognised were saved with nothing but their
-- extension as the container. The reader now also knows an archive saved
-- under a video's name (a release that was never unpacked, such as a RAR
-- volume named "Show - S01E01.mkv"), so those rows are dropped and the file
-- details job reads the files again. Only rows that hold nothing but the
-- container are dropped: a few AVI, TS or WMV files at most, read again in
-- seconds.
DELETE FROM media_info
WHERE failed = 0 AND json_valid(info) AND info = json_object('container', container);
