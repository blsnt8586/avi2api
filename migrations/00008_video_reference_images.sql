UPDATE model_configs
SET capabilities = capabilities || '["image-to-video"]'::jsonb
WHERE id IN ('seedance-2.0','seedance-2.0-fast','seedance-2.0-mini','gemini-omni-flash')
  AND NOT capabilities ? 'image-to-video';
