DROP TABLE sample_fts;

DELETE TRIGGER samples_fts_before_update;

DELETE TRIGGER samples_fts_after_update;

DELETE TRIGGER samples_fts_before_delete;

DELETE TRIGGER samples_fts_after_insert;
