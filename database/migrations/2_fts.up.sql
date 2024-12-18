CREATE VIRTUAL TABLE samples_fts USING fts5(
  id,
  summary,
  transcript,
  content='samples'
);
