-- +goose Up
UPDATE model_configs SET upstream_model='gpt-image-2' WHERE id='gpt-image-2';
UPDATE model_configs SET upstream_model='lucid-origin' WHERE id='leonardo-lucid-origin';
UPDATE model_configs SET upstream_model='flux-pro-2.0' WHERE id='flux-2-pro';
UPDATE model_configs SET upstream_model='nano-banana-2' WHERE id='nano-banana-2';

-- +goose Down
UPDATE model_configs SET upstream_model='135b2740-a20b-48c8-8f86-6f68199e06c5' WHERE id='gpt-image-2';
UPDATE model_configs SET upstream_model='7b592283-e8a7-4c5a-9ba6-d18c31f258b9' WHERE id='leonardo-lucid-origin';
UPDATE model_configs SET upstream_model='5478273a-68e1-4efe-a0c4-3fe84e4c16a8' WHERE id='flux-2-pro';
UPDATE model_configs SET upstream_model='7418e71f-4133-4e1b-9895-bee19f48f2ce' WHERE id='nano-banana-2';
