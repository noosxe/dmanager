-- +goose Up
-- Docker labels of the container as a JSON object, refreshed on every sync and
-- container event. Rows that exist before this migration get an empty object
-- until the startup sync fills them in.
ALTER TABLE containers ADD COLUMN labels TEXT NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE containers DROP COLUMN labels;
