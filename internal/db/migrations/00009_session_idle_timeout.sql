-- +goose Up
-- Each session carries its own idle window so remember-me sessions slide by
-- their (longer) idle timeout instead of the global default. Rows created
-- before this migration cannot be told apart, so they keep 0, which means
-- "use the configured default idle timeout" (a one-time shortening for any
-- existing remember-me session once it first slides).
ALTER TABLE sessions ADD COLUMN idle_timeout_seconds INTEGER NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE sessions DROP COLUMN idle_timeout_seconds;
