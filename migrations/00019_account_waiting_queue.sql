-- +goose Up
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_queue_capacity_check;
ALTER TABLE accounts ALTER COLUMN queue_capacity SET DEFAULT 5;
UPDATE accounts
SET queue_capacity=GREATEST(1,queue_capacity-image_concurrency);
ALTER TABLE accounts ADD CONSTRAINT accounts_queue_capacity_check
  CHECK(queue_capacity>=1 AND queue_capacity<=1000);

-- +goose Down
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_queue_capacity_check;
UPDATE accounts SET queue_capacity=LEAST(1000,queue_capacity+image_concurrency);
ALTER TABLE accounts ALTER COLUMN queue_capacity SET DEFAULT 10;
ALTER TABLE accounts ADD CONSTRAINT accounts_queue_capacity_check
  CHECK(queue_capacity>=image_concurrency AND queue_capacity<=1000);
