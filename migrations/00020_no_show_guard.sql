-- +goose Up
ALTER TABLE practice
    ADD COLUMN no_show_block_threshold smallint,
    ADD CONSTRAINT practice_no_show_block_threshold_check CHECK (no_show_block_threshold IS NULL OR no_show_block_threshold >= 1);

-- +goose Down
ALTER TABLE practice
    DROP CONSTRAINT practice_no_show_block_threshold_check,
    DROP COLUMN no_show_block_threshold;
