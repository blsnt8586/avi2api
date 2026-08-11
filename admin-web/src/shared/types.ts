export type Account = {
  id: string;
  provider_id: string;
  name: string;
  email: string;
  plan: string;
  subscription_tokens: number;
  rollover_tokens: number;
  paid_tokens: number;
  proxy_url: string;
  reserved_tokens: number;
  available_tokens: number;
  active_reservations: number;
  queued_tasks: number;
  status: string;
  image_concurrency: number;
  queue_capacity: number;
  routing_role: "general" | "video_reserved";
  protected_tokens: number;
  video_reserved_slots: number;
  access_token_expires_at?: string;
  last_checked_at?: string;
  last_error?: string;
  session_refresh_enabled: boolean;
  browser_worker_group: string;
  session_refresh_last_at?: string;
  session_refresh_last_method?: string;
  session_refresh_last_duration_ms?: number;
  session_refresh_failures: number;
  session_refresh_job_stage?: string;
  session_refresh_job_status?: string;
  session_refresh_job_next_attempt_at?: string;
  has_login_credentials: boolean;
};

export type AccountsPage = {
  data: Account[];
  total: number;
  page: number;
  page_size: number;
};

export type OverviewResponse = {
  accounts: number;
  active_accounts: number;
  task_counts: Record<string, number>;
  task_total: number;
  failed_last_hour: number;
  total_tokens: number;
  reserved_tokens: number;
  available_tokens: number;
  video_protected_tokens: number;
  video_ready_720p_15s: number;
  video_ready_1080p_8s: number;
  video_ready_1080p_10s: number;
  top_accounts: Account[];
};

export type SystemCapacity = {
  max_executing: number;
  max_queued: number;
  queue_high_watermark: number;
  queue_resume_watermark: number;
  queue_timeout_seconds: number;
  maintenance_mode: boolean;
  execution_paused: boolean;
  overload_active: boolean;
  revision: number;
  updated_at: string;
  queued: number;
  executing: number;
  executing_images: number;
  executing_videos: number;
  executing_audio: number;
  eligible_accounts: number;
  eligible_execution_slots: number;
  eligible_queue_slots: number;
  effective_execution_limit: number;
  execution_headroom: number;
  queue_headroom: number;
  oldest_queued_seconds: number;
};

type SystemRuntime = {
  task_dispatcher_concurrency: number;
  database_max_connections: number;
  api_key_queue_multiplier: number;
  gateway_inflight: number;
  gateway_inflight_limit: number;
  multipart_inflight: number;
  multipart_inflight_limit: number;
  sync_inflight: number;
  sync_inflight_limit: number;
};

export type SystemCapacityResponse = {
  capacity: SystemCapacity;
  runtime: SystemRuntime;
};

export type Provider = {
  id: string;
  display_name: string;
  enabled: boolean;
  auth_type: string;
  credit_unit: string;
  capabilities: string[];
};

type TaskErrorDetails = {
  source?: string;
  media_type?: string;
  generation_id?: string;
  upstream_status?: string;
  provider_error_code?: string;
  nsfw?: boolean;
  detail_error?: string;
  notes?: Array<{
    noteType?: string;
    notePayload?: unknown;
    failureReason?: Record<string, unknown>;
  }>;
  prompt_moderations?: Array<{ moderationClassification?: unknown }>;
};

export type Task = {
  id: string;
  account_id?: string;
  kind: string;
  status: string;
  progress: number;
  queue_position?: number;
  model: string;
  prompt: string;
  upstream_request?: Record<string, unknown>;
  generation_id?: string;
  result?: {
    created?: number;
    nsfw?: boolean;
    moderation_classifications?: string[];
    data?: Array<{
      id?: string;
      url?: string;
      media_type?: string;
      width?: number;
      height?: number;
      duration?: number;
      nsfw?: boolean;
      moderation_classifications?: string[];
    }>;
  };
  error_code?: string;
  error_message?: string;
  error_details?: TaskErrorDetails;
  retry_count?: number;
  tokens_before?: number;
  tokens_after?: number;
  estimated_tokens?: number;
  settled_tokens?: number;
  upstream_reported_cost?: number;
  reservation_state?: string;
  reservation_release_reason?: string;
  created_at: string;
  updated_at: string;
  started_at?: string;
  completed_at?: string;
  events?: Array<{
    id: number;
    status: string;
    message?: string;
    created_at: string;
  }>;
};

export type APIKeyRecord = {
  id: string;
  name: string;
  description: string;
  prefix: string;
  enabled: boolean;
  concurrency_limit: number;
  allowed_models: string[];
  request_count: number;
  created_at: string;
  last_used_at?: string;
  expires_at?: string;
};

export type APIKeysPage = {
  data: APIKeyRecord[];
  total: number;
  page: number;
  page_size: number;
};

export type TasksPage = {
  data: Task[];
  total: number;
  page: number;
  page_size: number;
};

export type AuditLog = {
  id: number;
  actor: string;
  action: string;
  target: string;
  metadata: Record<string, unknown>;
  created_at: string;
};

export type AuditLogsPage = {
  data: AuditLog[];
  total: number;
  page: number;
  page_size: number;
  actions?: string[];
};

export type APIRequestLog = {
  id: number;
  request_id: string;
  api_key_prefix: string;
  account_id?: string;
  account_name?: string;
  task_id?: string;
  method: string;
  path: string;
  kind?: string;
  model?: string;
  parameters: Record<string, unknown>;
  prompt_chars: number;
  estimated_tokens?: number;
  status: number;
  error_code?: string;
  duration_ms: number;
  client_ip: string;
  created_at: string;
};

export type APIRequestLogsPage = {
  data: APIRequestLog[];
  total: number;
  page: number;
  page_size: number;
};

export type PlatformModel = {
  id: string;
  type?: string;
  model_id: string;
  name: string;
  provider: string;
  description: string;
  production_api: boolean;
  cost_type?: string;
  base_token_cost?: number;
  quality_options?: string[];
  default_quality?: string;
  default_quantity?: number;
  maximum_quantity?: number;
  duration_options?: number[];
  default_duration?: number;
  resolution_modes?: string[];
};

export type PlatformModelRow = {
  platform: PlatformModel;
  exposed: boolean;
  public_id: string;
};

export type PlatformModelsResponse = {
  schema_version: string;
  media_type?: string;
  synced_at?: string;
  data: PlatformModelRow[];
};

export type ModelCostRecord = {
  model: string;
  kind: string;
  size?: string;
  quality?: string;
  resolution?: string;
  duration?: string;
  samples: number;
  average: number;
  minimum: number;
  maximum: number;
  upstream_reported_samples: number;
  upstream_reported_average?: number;
  upstream_reported_minimum?: number;
  upstream_reported_maximum?: number;
  last_used_at: string;
};

export type MediaKind = "image" | "video" | "audio";

export type CostRule = {
  id: number;
  provider_id: string;
  kind: MediaKind;
  model: string;
  size: string;
  quality: string;
  resolution: string;
  duration: number;
  unit_tokens: number;
  enabled: boolean;
  price_version: string;
  source: string;
  drifted?: boolean;
  drift_reason?: string;
  verified_at?: string;
  created_at: string;
  updated_at: string;
};

export type SalePricingProfile = {
  provider_id: string;
  currency: string;
  account_cost: number;
  included_credits: number;
  usable_credit_rate: number;
  overhead_rate: number;
  payment_fee_rate: number;
  target_margin: number;
  rounding_step: number;
};

export type SalePricingSettings = {
  profiles: SalePricingProfile[];
};

export type SaleMonetaryQuote = {
  credits: number;
  cost: number;
  price: number;
  payment_fee: number;
  profit: number;
  margin: number;
};

export type SalePricingItemQuote = {
  rule_id: number;
  available: boolean;
  error?: string;
  base_credits: number;
  effective_credits: number;
  applied_modifiers: Array<"reference_video" | "native_audio_disabled">;
  quote?: SaleMonetaryQuote;
};

export type SalePricingVideoRateQuote = {
  model: string;
  resolution: string;
  available: boolean;
  error?: string;
  durations: number[];
  base_credits_per_second: number;
  effective_credits_per_second: number;
  applied_modifiers: Array<"reference_video" | "native_audio_disabled">;
  quote?: SaleMonetaryQuote;
};

export type SalePricingQuoteResponse = {
  economics: {
    usable_credits: number;
    loaded_account_cost: number;
    cost_per_credit: number;
    sell_per_credit: number;
    projected_revenue: number;
    projected_profit: number;
  };
  manual_quotes: SaleMonetaryQuote[];
  items: SalePricingItemQuote[];
  video_rates: SalePricingVideoRateQuote[];
};

type EstimateRow = { label: string; values: number[] };

export type PlatformEstimate = {
  model: string;
  columns: string[];
  rows: EstimateRow[];
  note: string;
};

export type ImageCostEstimate = {
  model: PublicModel;
  size: string;
  quality?: string;
  quantity: number;
  unit_tokens: number;
  estimated_tokens: number;
  pricing_basis: "pixel_formula" | "size_tier" | "size_threshold" | "fixed";
  pricing_tier?: "small" | "medium" | "large" | "standard" | "2k";
  pricing_anchor?: string;
  quality_multiplier?: number;
  formula: string;
  cost_parameters: string[];
  price_version: string;
  source: string;
};

export type VideoCostEstimate = {
  model: PublicVideoModel;
  duration: number;
  size: string;
  resolution: string;
  generate_audio?: boolean;
  has_video_reference: boolean;
  base_tokens: number;
  estimated_tokens: number;
  applied_modifiers: Array<"seedance_video_reference" | "flux_video_reference" | "kling_o3_video_reference" | "native_audio_disabled">;
  formula: string;
  cost_parameters: string[];
  price_version: string;
  source: string;
};

export type PlaygroundOutput = {
  id?: string;
  url?: string;
  b64_json?: string;
  media_type?: string;
  duration?: number;
  width?: number;
  height?: number;
  nsfw?: boolean;
  moderation_classifications?: string[];
};

export type PlaygroundTask = {
  id: string;
  kind: string;
  status: string;
  progress: number;
  model: string;
  prompt: string;
  generation_id?: string;
  result?: {
    created?: number;
    nsfw?: boolean;
    moderation_classifications?: string[];
    data?: PlaygroundOutput[];
  };
  error_code?: string;
  error_message?: string;
  error_details?: TaskErrorDetails;
};

export type CachedImageResult = { task: PlaygroundTask; saved_at: number };

export type CachedVideoResult = { task: PlaygroundTask; saved_at: number };

export type CachedAudioResult = { task: PlaygroundTask; saved_at: number };

export type PublicModel =
  | "gpt-image-2"
  | "nano-banana-2"
  | "nano-banana-pro"
  | "seedream-5.0-pro";

export type ImageSizeGroup = {
  ratio: string;
  small?: string;
  medium: string;
  large: string;
};

export type ImageCostMatrix = {
  model: PublicModel;
  qualities: Array<"fixed" | "low" | "medium" | "high">;
  rows: Array<{
    size: string;
    costs: Partial<Record<"fixed" | "low" | "medium" | "high", number>>;
    pricing_tier?: string;
    pricing_anchor?: string;
  }>;
  price_versions: string[];
  sources: string[];
};

export type PublicVideoModel =
  | "flux-3-video"
  | "seedance-2.0"
  | "seedance-2.0-fast"
  | "seedance-2.0-mini"
  | "seedance-2.5"
  | "veo-3.1"
  | "veo-3.1-fast"
  | "kling-o3-omni"
  | "minimax-h3"
  | "grok-imagine-1.5";
