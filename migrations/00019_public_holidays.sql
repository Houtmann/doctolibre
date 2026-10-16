-- +goose Up
ALTER TABLE practice
    ADD COLUMN close_on_holidays boolean NOT NULL DEFAULT false,
    ADD COLUMN holidays_alsace_moselle boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE practice
    DROP COLUMN holidays_alsace_moselle,
    DROP COLUMN close_on_holidays;
