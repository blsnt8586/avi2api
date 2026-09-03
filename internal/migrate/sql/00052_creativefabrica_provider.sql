-- +goose Up
-- Creative Fabrica Studio uses coins and account-scoped model/cost catalogs.
-- The adapter catalog is synchronized from the authenticated Studio account;
-- no guessed price rows are inserted here.
INSERT INTO providers(id,display_name,auth_type,credit_unit,priority,capabilities,settings)
VALUES (
  'creativefabrica',
  'Creative Fabrica Studio',
  'cookie',
  'coins',
  120,
  '["image","video"]',
  '{"catalog_sync":true,"dynamic_pricing":true,"origin":"https://studio.creativefabrica.com","graphql_url":"https://graphql-gw.creativefabrica.com/query","jwt_auth_url":"https://www.creativefabrica.com/cfsecure/jwtauth","flow_url":"https://flow-api.creativefabrica.com","media_matrix_url":"https://studio-media-matrix.creativefabrica.com","coins_url":"https://coins.creativefabrica.com","modality_url":"https://modality.creativefabrica.com"}'
)
ON CONFLICT (id) DO UPDATE SET
  display_name=excluded.display_name,
  auth_type=excluded.auth_type,
  credit_unit=excluded.credit_unit,
  priority=excluded.priority,
  capabilities=excluded.capabilities,
  settings=excluded.settings,
  enabled=true,
  updated_at=now();

-- These are the account/catalog IDs exposed by Creative Fabrica Studio.
-- Shared IDs are intentionally left untouched so an existing Leonardo model
-- definition cannot be overwritten by a provider-specific migration.
INSERT INTO model_configs(id,upstream_model,display_name,enabled,capabilities,defaults) VALUES
  ('flux-2-max','flux-2-max','FLUX.2 Max',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('flux-2-pro','flux-2-pro','FLUX.2 Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('flux-2-klein','flux-2-klein','FLUX.2 Klein',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('grok','grok','Grok Image',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('ideogram-p-image','ideogram-p-image','Ideogram P-Image',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('ideogram-v4','ideogram-v4','Ideogram V4',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('kling-image-o1','kling-image-o1','Kling Image O1',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('kling-v2-1-image','kling-v2-1-image','Kling V2.1 Image',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('kling-v3-image','kling-v3-image','Kling V3 Image',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('kling-v3-omni-image','kling-v3-omni-image','Kling V3 Omni Image',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('nano-banana','nano-banana','Nano Banana',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('nano-banana-2','nano-banana-2','Nano Banana 2',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('nano-banana-flash-lite','nano-banana-flash-lite','Nano Banana Flash Lite',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('nano-banana-pro','nano-banana-pro','Nano Banana Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('openai-gpt-image-2','openai-gpt-image-2','GPT Image 2',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('qwen-image-2-0-pro','qwen-image-2-0-pro','Qwen Image 2.0 Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('qwen-image-3','qwen-image-3','Qwen Image 3',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('qwen-image-3-pro','qwen-image-3-pro','Qwen Image 3 Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('qwen-image-edit-spicy','qwen-image-edit-spicy','Qwen Image Edit Spicy',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('recraft-v4','recraft-v4','Recraft V4',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('recraft-v4-svg','recraft-v4-svg','Recraft V4 SVG',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('seedream-4-0','seedream-4-0','Seedream 4.0',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('seedream-4-5','seedream-4-5','Seedream 4.5',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('seedream-5-lite','seedream-5-lite','Seedream 5 Lite',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('seedream-5-pro','seedream-5-pro','Seedream 5 Pro',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('wan-2-7-pro-image','wan-2-7-pro-image','WAN 2.7 Pro Image',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('z-image-spicy','z-image-spicy','Z-Image Spicy',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('quiverai-arrow-1-1','quiverai-arrow-1-1','QuiverAI Arrow 1.1',true,'["text-to-image","image-to-image"]','{"width":1024,"height":1024,"quantity":1}'),
  ('alibaba_happy_horse_v1','alibaba_happy_horse_v1','Happy Horse 1.0',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('alibaba_happy_horse_v1_1','alibaba_happy_horse_v1_1','Happy Horse 1.1',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('alibaba_wan_2_7','alibaba_wan_2_7','WAN 2.7',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('berry_1_0','berry_1_0','Berry 1.0',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('berry_1_0_pro','berry_1_0_pro','Berry 1.0 Pro',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('fal_ltx_2_3','fal_ltx_2_3','LTX 2.3',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('fal_pixverse_c1','fal_pixverse_c1','PixVerse C1',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('fal_pixverse_v6','fal_pixverse_v6','PixVerse V6',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('flux_3','flux_3','FLUX 3',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('gemini_omni_flash','gemini_omni_flash','Gemini Omni Flash',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('gemini_omni_flash_1_1','gemini_omni_flash_1_1','Gemini Omni Flash 1.1',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('gemini_omni_flash_1_1_video_extended','gemini_omni_flash_1_1_video_extended','Gemini Omni Flash 1.1 Video Extended',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('grok_imagine_video','grok_imagine_video','Grok Imagine Video',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('grok_imagine_video_1_5','grok_imagine_video_1_5','Grok Imagine Video 1.5',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('heygen_avatar_5','heygen_avatar_5','HeyGen Avatar 5',true,'["text-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('kling_ai_v3','kling_ai_v3','Kling AI V3',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('kling_ai_v3_motion_control','kling_ai_v3_motion_control','Kling AI V3 Motion Control',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('kling_ai_v3_turbo','kling_ai_v3_turbo','Kling AI V3 Turbo',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('kling_v3_omni','kling_v3_omni','Kling V3 Omni',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('luma_ray_2','luma_ray_2','Luma Ray 2',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('luma_ray_3_2','luma_ray_3_2','Luma Ray 3.2',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('minimax_hailuo_v3','minimax_hailuo_v3','MiniMax Hailuo V3',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('minimax_hailuo_v3_max','minimax_hailuo_v3_max','MiniMax Hailuo V3 Max',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('mulerouter_wan_2_7_spicy','mulerouter_wan_2_7_spicy','WAN 2.7 Spicy',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('pika_v2_5','pika_v2_5','Pika 2.5',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('runway_gen_4_5','runway_gen_4_5','Runway Gen 4.5',true,'["text-to-video","image-to-video"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('seedance_one_five_pro','seedance_one_five_pro','Seedance 1.5 Pro',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('seedance_two_point_zero','seedance_two_point_zero','Seedance 2.0',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
  ('seedance_v2_5','seedance_v2_5','Seedance 2.5',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
  ('seedance_v2_fast','seedance_v2_fast','Seedance 2.0 Fast',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
  ('seedance_v2_mini','seedance_v2_mini','Seedance 2.0 Mini',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
  ('veo_31_fast_generate_preview','veo_31_fast_generate_preview','Veo 3.1 Fast',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":8,"resolution":"720p","generate_audio":true}'),
  ('wan_3_0','wan_3_0','WAN 3.0',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}'),
  ('wan_3_0_prime','wan_3_0_prime','WAN 3.0 Prime',true,'["text-to-video","image-to-video","native-audio"]','{"width":1280,"height":720,"duration":5,"resolution":"720p","generate_audio":true}')
ON CONFLICT(id) DO NOTHING;

WITH image_models(model_id,upstream_model) AS (
  VALUES
    ('flux-2-max','FLOW_MODEL_FLUX_2_MAX'),
    ('flux-2-pro','FLOW_MODEL_FLUX_2_PRO'),
    ('flux-2-klein','FLOW_MODEL_FLUX_2_KLEIN'),
    ('grok','FLOW_MODEL_GROK_2_IMAGE_GEN_1212'),
    ('ideogram-p-image','FLOW_MODEL_IDEOGRAM_P_IMAGE'),
    ('ideogram-v4','FLOW_MODEL_IDEOGRAM_V4'),
    ('kling-image-o1','FLOW_MODEL_KLING_IMAGE_O1'),
    ('kling-v2-1-image','FLOW_MODEL_KLING_V2_1_IMAGE'),
    ('kling-v3-image','FLOW_MODEL_KLING_V3_IMAGE'),
    ('kling-v3-omni-image','FLOW_MODEL_KLING_V3_OMNI_IMAGE'),
    ('nano-banana','FLOW_MODEL_NANO_BANANA'),
    ('nano-banana-2','FLOW_MODEL_NANO_BANANA_2'),
    ('nano-banana-flash-lite','FLOW_MODEL_NANO_BANANA_FLASH_LITE'),
    ('nano-banana-pro','FLOW_MODEL_NANO_PRO'),
    ('openai-gpt-image-2','FLOW_MODEL_OPENAI_GPT_IMAGE_2'),
    ('qwen-image-2-0-pro','FLOW_MODEL_QWEN_IMAGE_2_0_PRO'),
    ('qwen-image-3','FLOW_MODEL_QWEN_IMAGE_3'),
    ('qwen-image-3-pro','FLOW_MODEL_QWEN_IMAGE_3_PRO'),
    ('qwen-image-edit-spicy','FLOW_MODEL_QWEN_IMAGE_EDIT_SPICY'),
    ('recraft-v4','FLOW_MODEL_RECRAFT_V4'),
    ('recraft-v4-svg','FLOW_MODEL_RECRAFT_V4_SVG'),
    ('seedream-4-0','FLOW_MODEL_SEEDREAM_4_0'),
    ('seedream-4-5','FLOW_MODEL_SEEDREAM_4_5'),
    ('seedream-5-lite','FLOW_MODEL_SEEDREAM_5_LITE'),
    ('seedream-5-pro','FLOW_MODEL_SEEDREAM_5_PRO'),
    ('wan-2-7-pro-image','FLOW_MODEL_WAN_2_7_PRO_IMAGE'),
    ('z-image-spicy','FLOW_MODEL_Z_IMAGE_SPICY'),
    ('quiverai-arrow-1-1','FLOW_MODEL_QUIVERAI_ARROW_1_1')
)
INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings)
SELECT 'creativefabrica',model_id,upstream_model,true,120,'{"pricing":"dynamic"}'::jsonb
FROM image_models
ON CONFLICT(provider_id,model_id) DO UPDATE SET
  upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,settings=excluded.settings,updated_at=now();

WITH video_models(model_id,upstream_model) AS (
  VALUES
    ('alibaba_happy_horse_v1','VIDEO_GENERATOR_MODEL_ALIBABA_HAPPY_HORSE_V1'),
    ('alibaba_happy_horse_v1_1','VIDEO_GENERATOR_MODEL_ALIBABA_HAPPY_HORSE_V1_1'),
    ('alibaba_wan_2_7','VIDEO_GENERATOR_MODEL_ALIBABA_WAN_2_7'),
    ('berry_1_0','VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0'),
    ('berry_1_0_pro','VIDEO_GENERATOR_MODEL_MULEROUTER_BERRY_1_0_PRO'),
    ('fal_ltx_2_3','VIDEO_GENERATOR_MODEL_FAL_LTX_2_3'),
    ('fal_pixverse_c1','VIDEO_GENERATOR_MODEL_FAL_PIXVERSE_C1'),
    ('fal_pixverse_v6','VIDEO_GENERATOR_MODEL_FAL_PIXVERSE_V6'),
    ('flux_3','VIDEO_GENERATOR_MODEL_FLUX_3'),
    ('gemini_omni_flash','VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH'),
    ('gemini_omni_flash_1_1','VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH_1_1'),
    ('gemini_omni_flash_1_1_video_extended','VIDEO_GENERATOR_MODEL_GEMINI_OMNI_FLASH_1_1_VIDEO_EXTENDED'),
    ('grok_imagine_video','VIDEO_GENERATOR_MODEL_GROK_IMAGINE_VIDEO'),
    ('grok_imagine_video_1_5','VIDEO_GENERATOR_MODEL_GROK_IMAGINE_VIDEO_1_5'),
    ('heygen_avatar_5','VIDEO_GENERATOR_MODEL_HEYGEN_AVATAR_5'),
    ('kling_ai_v3','VIDEO_GENERATOR_MODEL_KLING_AI_V3'),
    ('kling_ai_v3_motion_control','VIDEO_GENERATOR_MODEL_KLING_AI_V3_MOTION_CONTROL'),
    ('kling_ai_v3_turbo','VIDEO_GENERATOR_MODEL_KLING_AI_V3_TURBO'),
    ('kling_v3_omni','VIDEO_GENERATOR_MODEL_KLING_V3_OMNI'),
    ('luma_ray_2','VIDEO_GENERATOR_MODEL_LUMA_RAY_2'),
    ('luma_ray_3_2','VIDEO_GENERATOR_MODEL_LUMA_RAY_3_2'),
    ('minimax_hailuo_v3','VIDEO_GENERATOR_MODEL_MINIMAX_HAILUO_V3'),
    ('minimax_hailuo_v3_max','VIDEO_GENERATOR_MODEL_MINIMAX_HAILUO_V3_MAX'),
    ('mulerouter_wan_2_7_spicy','VIDEO_GENERATOR_MODEL_MULEROUTER_WAN_2_7_SPICY'),
    ('pika_v2_5','VIDEO_GENERATOR_MODEL_PIKA_2_5'),
    ('runway_gen_4_5','VIDEO_GENERATOR_MODEL_RUNWAY_GEN_4_5'),
    ('seedance_one_five_pro','VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_1_5_PRO'),
    ('seedance_two_point_zero','VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDDREAM_2'),
    ('seedance_v2_5','VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_5'),
    ('seedance_v2_fast','VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_FAST'),
    ('seedance_v2_mini','VIDEO_GENERATOR_MODEL_BYTEDANCE_SEEDANCE_2_MINI'),
    ('veo_31_fast_generate_preview','VIDEO_GENERATOR_MODEL_VEO_31_FAST_GENERATE_PREVIEW'),
    ('wan_3_0','VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0'),
    ('wan_3_0_prime','VIDEO_GENERATOR_MODEL_ALIBABA_WAN_3_0_PRIME')
)
INSERT INTO model_provider_configs(provider_id,model_id,upstream_model,enabled,priority,settings)
SELECT 'creativefabrica',model_id,upstream_model,true,120,'{"pricing":"dynamic"}'::jsonb
FROM video_models
ON CONFLICT(provider_id,model_id) DO UPDATE SET
  upstream_model=excluded.upstream_model,enabled=true,priority=excluded.priority,settings=excluded.settings,updated_at=now();

-- +goose Down
-- Remove only provider bindings. Shared model definitions are retained because
-- they may also be referenced by Leonardo or another provider.
DELETE FROM model_provider_configs WHERE provider_id='creativefabrica';
DELETE FROM providers WHERE id='creativefabrica';
