-- +goose Up
INSERT INTO providers(id,display_name,auth_type,credit_unit,priority,capabilities,settings)
VALUES (
  'adobe',
  'Adobe Firefly',
  'api_key',
  'credits',
  110,
  '["image","video"]',
  '{"submit_image_url":"https://firefly-3p.ff.adobe.io/v2/3p-images/generate-async","submit_video_url":"https://firefly-3p.ff.adobe.io/v2/3p-videos/generate-async","image_upload_url":"https://firefly-3p.ff.adobe.io/v2/storage/image","video_upload_url":"https://firefly-3p.ff.adobe.io/v2/storage/video","audio_upload_url":"https://firefly-3p.ff.adobe.io/v2/storage/audio","profile_url":"https://ims-na1.adobelogin.com/ims/profile/v1","credits_url":"https://firefly.adobe.io/v1/credits/balance","models_enabled":false}'
)
ON CONFLICT (id) DO UPDATE SET
  display_name=excluded.display_name,
  auth_type=excluded.auth_type,
  credit_unit=excluded.credit_unit,
  priority=excluded.priority,
  capabilities=excluded.capabilities,
  settings=excluded.settings,
  updated_at=now();

-- +goose Down
DELETE FROM model_provider_configs WHERE provider_id='adobe';
DELETE FROM model_cost_rules WHERE provider_id='adobe';
DELETE FROM providers WHERE id='adobe';
