-- +goose Up
ALTER TABLE accounts ADD COLUMN queue_capacity integer NOT NULL DEFAULT 10;
UPDATE accounts SET queue_capacity=GREATEST(10,image_concurrency);
ALTER TABLE accounts ADD CONSTRAINT accounts_queue_capacity_check
  CHECK(queue_capacity>=image_concurrency AND queue_capacity<=1000);

-- +goose Down
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_queue_capacity_check;
ALTER TABLE accounts DROP COLUMN IF EXISTS queue_capacity;
