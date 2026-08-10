package store

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/leonardo2api/leonardo2api/internal/domain"
	"strings"
	"time"
)

func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.provider_id,a.name,a.email,a.cookie_ciphertext,a.access_token_ciphertext,a.access_token_expires_at,a.hasura_user_id,a.cognito_sub,a.team_id,a.plan,a.subscription_tokens,a.rollover_tokens,a.paid_tokens,a.proxy_url,a.user_agent,a.image_concurrency,a.queue_capacity,a.routing_role,a.protected_tokens,a.video_reserved_slots,a.status,a.cooldown_until,a.last_error,a.last_checked_at,a.created_at,a.updated_at,
		a.session_refresh_enabled,a.browser_profile_key,a.browser_worker_group,a.has_login_credentials,a.session_refresh_jitter_seconds,a.session_refresh_not_before,a.session_refresh_last_at,a.session_refresh_last_method,a.session_refresh_last_duration_ms,a.session_refresh_failures,
		COALESCE(r.reserved,0),GREATEST(0,a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(r.reserved,0)),COALESCE(r.slots,0),COALESCE(r.queued,0),
		COALESCE(j.stage,''),COALESCE(j.status,''),j.next_attempt_at
		FROM accounts a LEFT JOIN LATERAL (
			SELECT COALESCE(sum(r.estimated_tokens),0) AS reserved,
				count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')) AS slots,
				count(*) FILTER (WHERE t.status='queued') AS queued
			FROM account_reservations r JOIN tasks t ON t.id=r.task_id
			WHERE r.account_id=a.id AND r.state='held'
		) r ON true LEFT JOIN LATERAL (
			SELECT stage,status,next_attempt_at FROM session_refresh_jobs
			WHERE account_id=a.id AND status IN ('pending','leased')
			ORDER BY created_at DESC LIMIT 1
		) j ON true ORDER BY a.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.ID, &a.ProviderID, &a.Name, &a.Email, &a.CookieCiphertext, &a.AccessTokenCiphertext, &a.AccessTokenExpiresAt, &a.HasuraUserID, &a.CognitoSub, &a.TeamID, &a.Plan, &a.SubscriptionTokens, &a.RolloverTokens, &a.PaidTokens, &a.ProxyURL, &a.UserAgent, &a.ImageConcurrency, &a.QueueCapacity, &a.RoutingRole, &a.ProtectedTokens, &a.VideoReservedSlots, &a.Status, &a.CooldownUntil, &a.LastError, &a.LastCheckedAt, &a.CreatedAt, &a.UpdatedAt, &a.SessionRefreshEnabled, &a.BrowserProfileKey, &a.BrowserWorkerGroup, &a.HasLoginCredentials, &a.SessionRefreshJitterSeconds, &a.SessionRefreshNotBefore, &a.SessionRefreshLastAt, &a.SessionRefreshLastMethod, &a.SessionRefreshLastDurationMS, &a.SessionRefreshFailures, &a.ReservedTokens, &a.AvailableTokens, &a.ActiveReservations, &a.QueuedTasks, &a.SessionRefreshJobStage, &a.SessionRefreshJobStatus, &a.SessionRefreshJobNextAttemptAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAccountsPage(ctx context.Context, page, pageSize int, search string) (AccountPage, error) {
	return s.ListAccountsPageFiltered(ctx, page, pageSize, AccountPageFilter{Search: search})
}

func (s *Store) ListAccountsPageFiltered(ctx context.Context, page, pageSize int, filter AccountPageFilter) (AccountPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	filter.Search = strings.TrimSpace(filter.Search)
	filter.Status = strings.TrimSpace(filter.Status)
	filter.Role = strings.TrimSpace(filter.Role)
	filter.ProviderID = strings.TrimSpace(filter.ProviderID)
	const where = ` WHERE ($1='' OR a.name ILIKE '%'||$1||'%' OR a.email ILIKE '%'||$1||'%' OR a.provider_id ILIKE '%'||$1||'%')
		AND ($2='' OR ($2='attention' AND (a.status<>'active' OR a.access_token_expires_at IS NULL OR a.access_token_expires_at<=now()))
			OR ($2='active' AND a.status='active' AND a.access_token_expires_at>now())
			OR ($2='cooldown' AND (a.status='cooldown' OR (a.status='active' AND (a.access_token_expires_at IS NULL OR a.access_token_expires_at<=now()))))
			OR ($2 NOT IN ('attention','active','cooldown') AND a.status=$2))
		AND ($3='' OR a.routing_role=$3) AND ($4='' OR a.provider_id=$4)`
	args := []any{filter.Search, filter.Status, filter.Role, filter.ProviderID}
	var total int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM accounts a`+where, args...).Scan(&total); err != nil {
		return AccountPage{}, err
	}
	rows, err := s.DB.Query(ctx, `SELECT a.id,a.provider_id,a.name,a.email,a.cookie_ciphertext,a.access_token_ciphertext,a.access_token_expires_at,a.hasura_user_id,a.cognito_sub,a.team_id,a.plan,a.subscription_tokens,a.rollover_tokens,a.paid_tokens,a.proxy_url,a.user_agent,a.image_concurrency,a.queue_capacity,a.routing_role,a.protected_tokens,a.video_reserved_slots,a.status,a.cooldown_until,a.last_error,a.last_checked_at,a.created_at,a.updated_at,
		a.session_refresh_enabled,a.browser_profile_key,a.browser_worker_group,a.has_login_credentials,a.session_refresh_jitter_seconds,a.session_refresh_not_before,a.session_refresh_last_at,a.session_refresh_last_method,a.session_refresh_last_duration_ms,a.session_refresh_failures,
		COALESCE(r.reserved,0),GREATEST(0,a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(r.reserved,0)),COALESCE(r.slots,0),COALESCE(r.queued,0),
		COALESCE(j.stage,''),COALESCE(j.status,''),j.next_attempt_at
		FROM accounts a LEFT JOIN LATERAL (
			SELECT COALESCE(sum(r.estimated_tokens),0) AS reserved,
				count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')) AS slots,
				count(*) FILTER (WHERE t.status='queued') AS queued
			FROM account_reservations r JOIN tasks t ON t.id=r.task_id
			WHERE r.account_id=a.id AND r.state='held'
		) r ON true LEFT JOIN LATERAL (
			SELECT stage,status,next_attempt_at FROM session_refresh_jobs
			WHERE account_id=a.id AND status IN ('pending','leased')
			ORDER BY created_at DESC LIMIT 1
		) j ON true
		`+where+`
		ORDER BY a.created_at,a.id LIMIT $5 OFFSET $6`, append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return AccountPage{}, err
	}
	defer rows.Close()
	out := make([]domain.Account, 0, pageSize)
	for rows.Next() {
		var a domain.Account
		if err := rows.Scan(&a.ID, &a.ProviderID, &a.Name, &a.Email, &a.CookieCiphertext, &a.AccessTokenCiphertext, &a.AccessTokenExpiresAt, &a.HasuraUserID, &a.CognitoSub, &a.TeamID, &a.Plan, &a.SubscriptionTokens, &a.RolloverTokens, &a.PaidTokens, &a.ProxyURL, &a.UserAgent, &a.ImageConcurrency, &a.QueueCapacity, &a.RoutingRole, &a.ProtectedTokens, &a.VideoReservedSlots, &a.Status, &a.CooldownUntil, &a.LastError, &a.LastCheckedAt, &a.CreatedAt, &a.UpdatedAt, &a.SessionRefreshEnabled, &a.BrowserProfileKey, &a.BrowserWorkerGroup, &a.HasLoginCredentials, &a.SessionRefreshJitterSeconds, &a.SessionRefreshNotBefore, &a.SessionRefreshLastAt, &a.SessionRefreshLastMethod, &a.SessionRefreshLastDurationMS, &a.SessionRefreshFailures, &a.ReservedTokens, &a.AvailableTokens, &a.ActiveReservations, &a.QueuedTasks, &a.SessionRefreshJobStage, &a.SessionRefreshJobStatus, &a.SessionRefreshJobNextAttemptAt); err != nil {
			return AccountPage{}, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return AccountPage{}, err
	}
	return AccountPage{Data: out, Total: total, Page: page, PageSize: pageSize}, nil
}

func (s *Store) GetAccountOverview(ctx context.Context) (AccountOverview, error) {
	var out AccountOverview
	err := s.DB.QueryRow(ctx, `WITH held AS (
		SELECT account_id,sum(estimated_tokens) AS reserved FROM account_reservations
		WHERE state='held' GROUP BY account_id
	), costs AS (
		SELECT
		  max(unit_tokens) FILTER (WHERE provider_id='leonardo' AND kind='video' AND model='seedance-2.0' AND resolution='720p' AND duration=15 AND enabled=true AND drifted=false) AS p720_15,
		  max(unit_tokens) FILTER (WHERE provider_id='leonardo' AND kind='video' AND model='seedance-2.0' AND resolution='1080p' AND duration=8 AND enabled=true AND drifted=false) AS p1080_8,
		  max(unit_tokens) FILTER (WHERE provider_id='leonardo' AND kind='video' AND model='seedance-2.0' AND resolution='1080p' AND duration=10 AND enabled=true AND drifted=false) AS p1080_10
		FROM model_cost_rules
	)
	SELECT count(*),count(*) FILTER (WHERE a.status='active'),
		COALESCE(sum(a.subscription_tokens+a.rollover_tokens+a.paid_tokens),0),
		COALESCE(sum(COALESCE(h.reserved,0)),0),
		COALESCE(sum(GREATEST(0,a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(h.reserved,0))),0),
		COALESCE(sum(a.protected_tokens) FILTER (WHERE a.routing_role='video_reserved' AND a.status='active'),0),
		count(*) FILTER (WHERE a.status='active' AND c.p720_15 IS NOT NULL AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(h.reserved,0)>=c.p720_15),
		count(*) FILTER (WHERE a.status='active' AND c.p1080_8 IS NOT NULL AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(h.reserved,0)>=c.p1080_8),
		count(*) FILTER (WHERE a.status='active' AND c.p1080_10 IS NOT NULL AND a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(h.reserved,0)>=c.p1080_10)
	FROM accounts a LEFT JOIN held h ON h.account_id=a.id CROSS JOIN costs c`).Scan(
		&out.TotalAccounts, &out.ActiveAccounts, &out.TotalTokens, &out.ReservedTokens, &out.AvailableTokens,
		&out.VideoProtectedTokens, &out.VideoReady720P15, &out.VideoReady1080P8, &out.VideoReady1080P10)
	return out, err
}

func (s *Store) GetAccount(ctx context.Context, id uuid.UUID) (domain.Account, error) {
	var a domain.Account
	err := s.DB.QueryRow(ctx, `SELECT a.id,a.provider_id,a.name,a.email,a.cookie_ciphertext,a.access_token_ciphertext,a.access_token_expires_at,a.hasura_user_id,a.cognito_sub,a.team_id,a.plan,a.subscription_tokens,a.rollover_tokens,a.paid_tokens,a.proxy_url,a.user_agent,a.image_concurrency,a.queue_capacity,a.routing_role,a.protected_tokens,a.video_reserved_slots,a.status,a.cooldown_until,a.last_error,a.last_checked_at,a.created_at,a.updated_at,
		a.session_refresh_enabled,a.browser_profile_key,a.browser_worker_group,a.has_login_credentials,a.session_refresh_jitter_seconds,a.session_refresh_not_before,a.session_refresh_last_at,a.session_refresh_last_method,a.session_refresh_last_duration_ms,a.session_refresh_failures,
		COALESCE(r.reserved,0),GREATEST(0,a.subscription_tokens+a.rollover_tokens+a.paid_tokens-COALESCE(r.reserved,0)),COALESCE(r.slots,0),COALESCE(r.queued,0),
		COALESCE(j.stage,''),COALESCE(j.status,''),j.next_attempt_at
		FROM accounts a LEFT JOIN LATERAL (
			SELECT COALESCE(sum(r.estimated_tokens),0) AS reserved,
				count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')) AS slots,
				count(*) FILTER (WHERE t.status='queued') AS queued
			FROM account_reservations r JOIN tasks t ON t.id=r.task_id
			WHERE r.account_id=a.id AND r.state='held'
		) r ON true LEFT JOIN LATERAL (
			SELECT stage,status,next_attempt_at FROM session_refresh_jobs
			WHERE account_id=a.id AND status IN ('pending','leased')
			ORDER BY created_at DESC LIMIT 1
		) j ON true WHERE a.id=$1`, id).Scan(&a.ID, &a.ProviderID, &a.Name, &a.Email, &a.CookieCiphertext, &a.AccessTokenCiphertext, &a.AccessTokenExpiresAt, &a.HasuraUserID, &a.CognitoSub, &a.TeamID, &a.Plan, &a.SubscriptionTokens, &a.RolloverTokens, &a.PaidTokens, &a.ProxyURL, &a.UserAgent, &a.ImageConcurrency, &a.QueueCapacity, &a.RoutingRole, &a.ProtectedTokens, &a.VideoReservedSlots, &a.Status, &a.CooldownUntil, &a.LastError, &a.LastCheckedAt, &a.CreatedAt, &a.UpdatedAt, &a.SessionRefreshEnabled, &a.BrowserProfileKey, &a.BrowserWorkerGroup, &a.HasLoginCredentials, &a.SessionRefreshJitterSeconds, &a.SessionRefreshNotBefore, &a.SessionRefreshLastAt, &a.SessionRefreshLastMethod, &a.SessionRefreshLastDurationMS, &a.SessionRefreshFailures, &a.ReservedTokens, &a.AvailableTokens, &a.ActiveReservations, &a.QueuedTasks, &a.SessionRefreshJobStage, &a.SessionRefreshJobStatus, &a.SessionRefreshJobNextAttemptAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateAccount(ctx context.Context, name, email, cookieCipher, credentialCipher string, hasLoginCredentials bool, proxyURL, userAgent string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	return s.CreateProviderAccount(ctx, "leonardo", name, email, cookieCipher, credentialCipher, hasLoginCredentials, proxyURL, userAgent, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots)
}

func (s *Store) CreateProviderAccount(ctx context.Context, providerID, name, email, cookieCipher, credentialCipher string, hasLoginCredentials bool, proxyURL, userAgent string, concurrency, queueCapacity int, routingRole string, protectedTokens int64, videoReservedSlots int) (domain.Account, error) {
	if concurrency < 1 {
		concurrency = 1
	}
	if queueCapacity < 1 {
		queueCapacity = 40
	}
	if routingRole == "" {
		routingRole = "general"
	}
	if userAgent == "" {
		userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
	}
	id := uuid.New()
	_, err := s.DB.Exec(ctx, `INSERT INTO accounts(id,provider_id,name,email,cookie_ciphertext,credential_ciphertext,has_login_credentials,proxy_url,user_agent,image_concurrency,queue_capacity,routing_role,protected_tokens,video_reserved_slots,browser_profile_key,session_refresh_jitter_seconds)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,mod(mod(hashtextextended($15,0),601)+601,601)::integer)`, id, providerID, name, email, cookieCipher, credentialCipher, hasLoginCredentials, proxyURL, userAgent, concurrency, queueCapacity, routingRole, protectedTokens, videoReservedSlots, id.String())
	if err != nil {
		return domain.Account{}, err
	}
	return s.GetAccount(ctx, id)
}

func (s *Store) DeleteAccount(ctx context.Context, id uuid.UUID) error {
	command, err := s.DB.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) GetAccountLoginCredentialCiphertext(ctx context.Context, id uuid.UUID) (string, bool, error) {
	var ciphertext string
	var configured bool
	err := s.DB.QueryRow(ctx, `SELECT credential_ciphertext,has_login_credentials FROM accounts WHERE id=$1`, id).Scan(&ciphertext, &configured)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return ciphertext, configured, err
}

func (s *Store) SetAccountLoginCredential(ctx context.Context, id uuid.UUID, ciphertext string, configured bool) error {
	command, err := s.DB.Exec(ctx, `UPDATE accounts SET credential_ciphertext=$2,has_login_credentials=$3,updated_at=now() WHERE id=$1`, id, ciphertext, configured)
	if err == nil && command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// UpdateAccountSessionCredentials persists a browser-authenticated session
// without claiming that the account balance was refreshed. Balance snapshots
// are fenced and updated separately.

func (s *Store) UpdateAccountSessionCredentials(ctx context.Context, id uuid.UUID, tokenCipher, cookieCipher string, expiry time.Time, hasuraID, sub, email, userAgent string) error {
	command, err := s.DB.Exec(ctx, `UPDATE accounts SET
		access_token_ciphertext=CASE WHEN access_token_expires_at IS NULL OR $4>=access_token_expires_at THEN $2 ELSE access_token_ciphertext END,
		access_token_expires_at=GREATEST(COALESCE(access_token_expires_at,$4),$4),
		cookie_ciphertext=CASE
			WHEN (access_token_expires_at IS NULL OR $4>=access_token_expires_at) AND $3<>'' THEN $3
			ELSE cookie_ciphertext END,
		hasura_user_id=CASE WHEN access_token_expires_at IS NULL OR $4>=access_token_expires_at THEN $5 ELSE hasura_user_id END,
		cognito_sub=CASE WHEN access_token_expires_at IS NULL OR $4>=access_token_expires_at THEN $6 ELSE cognito_sub END,
		email=CASE WHEN (access_token_expires_at IS NULL OR $4>=access_token_expires_at) AND $7<>'' THEN $7 ELSE email END,
		user_agent=CASE WHEN (access_token_expires_at IS NULL OR $4>=access_token_expires_at) AND $8<>'' THEN $8 ELSE user_agent END,
		status=CASE
			WHEN status='disabled' THEN status
			WHEN status='rate_limited' AND cooldown_until>now()
			  AND last_error<>'Leonardo HTTP 429: temporary Vercel security checkpoint' THEN status
			ELSE 'active' END,
		last_error=CASE
			WHEN status='disabled' THEN last_error
			WHEN status='rate_limited' AND cooldown_until>now()
			  AND last_error<>'Leonardo HTTP 429: temporary Vercel security checkpoint' THEN last_error
			ELSE '' END,
		cooldown_until=CASE
			WHEN status='disabled' THEN cooldown_until
			WHEN status='rate_limited' AND cooldown_until>now()
			  AND last_error<>'Leonardo HTTP 429: temporary Vercel security checkpoint' THEN cooldown_until
			ELSE NULL END,
		session_refresh_failures=0,session_refresh_not_before=NULL,updated_at=now()
		WHERE id=$1`, id, tokenCipher, cookieCipher, expiry, hasuraID, sub, email, userAgent)
	if err == nil && command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) UpdateAccountSession(ctx context.Context, id uuid.UUID, balanceVersion int64, tokenCipher, cookieCipher string, expiry, snapshotStartedAt time.Time, hasuraID, sub, email string, plan string, subTokens, rollover, paid int64, lastErr string) (bool, error) {
	status := "active"
	if lastErr != "" {
		status = "invalid"
	}
	var balanceUpdated bool
	err := s.DB.QueryRow(ctx, `UPDATE accounts SET
		access_token_ciphertext=CASE WHEN access_token_expires_at IS NULL OR $5>=access_token_expires_at THEN $3 ELSE access_token_ciphertext END,
		access_token_expires_at=GREATEST(COALESCE(access_token_expires_at,$5),$5),
		cookie_ciphertext=CASE WHEN $4='' THEN cookie_ciphertext ELSE $4 END,
		hasura_user_id=$7,cognito_sub=$8,email=CASE WHEN $9='' THEN email ELSE $9 END,
		plan=CASE WHEN balance_refresh_version=$2 THEN $10 ELSE plan END,
		subscription_tokens=CASE WHEN balance_refresh_version=$2 THEN $11 ELSE subscription_tokens END,
		rollover_tokens=CASE WHEN balance_refresh_version=$2 THEN $12 ELSE rollover_tokens END,
		paid_tokens=CASE WHEN balance_refresh_version=$2 THEN $13 ELSE paid_tokens END,
		last_error=$14,status=$15,cooldown_until=NULL,
		last_checked_at=CASE WHEN balance_refresh_version=$2 THEN now() ELSE last_checked_at END,
		balance_snapshot_version=CASE WHEN balance_refresh_version=$2 THEN $2 ELSE balance_snapshot_version END,
		balance_snapshot_started_at=CASE WHEN balance_refresh_version=$2 THEN $6 ELSE balance_snapshot_started_at END,
		updated_at=now()
		WHERE id=$1 RETURNING balance_refresh_version=$2`, id, balanceVersion, tokenCipher, cookieCipher, expiry,
		snapshotStartedAt, hasuraID, sub, email, plan, subTokens, rollover, paid, lastErr, status).Scan(&balanceUpdated)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return balanceUpdated, err
}

func (s *Store) BeginAccountBalanceRefresh(ctx context.Context, id uuid.UUID) (int64, error) {
	var version int64
	err := s.DB.QueryRow(ctx, `UPDATE accounts SET balance_refresh_version=balance_refresh_version+1,updated_at=now() WHERE id=$1 RETURNING balance_refresh_version`, id).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return version, err
}

func (s *Store) UpdateAccountTokens(ctx context.Context, id uuid.UUID, version int64, snapshotStartedAt time.Time, plan string, subscription, rollover, paid int64) (bool, error) {
	command, err := s.DB.Exec(ctx, `UPDATE accounts SET
		plan=$2,subscription_tokens=$3,rollover_tokens=$4,paid_tokens=$5,
		last_error=CASE
			WHEN status IN ('disabled','invalid') OR (status IN ('rate_limited','cooldown') AND cooldown_until>now()) THEN last_error
			ELSE '' END,
		status=CASE
			WHEN status IN ('disabled','invalid') OR (status IN ('rate_limited','cooldown') AND cooldown_until>now()) THEN status
			ELSE 'active' END,
		cooldown_until=CASE
			WHEN status IN ('disabled','invalid') OR (status IN ('rate_limited','cooldown') AND cooldown_until>now()) THEN cooldown_until
			ELSE NULL END,
		last_checked_at=now(),balance_snapshot_version=$6,balance_snapshot_started_at=$7,updated_at=now()
		WHERE id=$1 AND balance_refresh_version=$6`, id, plan, subscription, rollover, paid, version, snapshotStartedAt)
	return command.RowsAffected() == 1, err
}

type AccountBalanceSnapshot struct {
	Version   int64
	StartedAt *time.Time
}

func (s *Store) GetAccountBalanceSnapshot(ctx context.Context, id uuid.UUID) (AccountBalanceSnapshot, error) {
	var snapshot AccountBalanceSnapshot
	err := s.DB.QueryRow(ctx, `SELECT balance_snapshot_version,balance_snapshot_started_at FROM accounts WHERE id=$1`, id).Scan(&snapshot.Version, &snapshot.StartedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, ErrNotFound
	}
	return snapshot, err
}

func (s *Store) SetAccountError(ctx context.Context, id uuid.UUID, status, msg string, cooldown *time.Time) error {
	_, err := s.DB.Exec(ctx, `UPDATE accounts SET
		status=CASE
			WHEN status IN ('disabled','invalid') THEN status
			WHEN status='rate_limited' AND cooldown_until>now() AND $2='cooldown' THEN status
			ELSE $2 END,
		last_error=CASE
			WHEN status IN ('disabled','invalid') THEN last_error
			WHEN status='rate_limited' AND cooldown_until>now() AND $2='cooldown' THEN last_error
			ELSE $3 END,
		cooldown_until=CASE
			WHEN status IN ('disabled','invalid') THEN cooldown_until
			WHEN cooldown_until IS NULL THEN $4::timestamptz
			WHEN $4::timestamptz IS NULL THEN cooldown_until
			ELSE GREATEST(cooldown_until,$4::timestamptz) END,
		updated_at=now()
		WHERE id=$1`, id, status, msg, cooldown)
	return err
}

func (s *Store) SetAccountStatus(ctx context.Context, id uuid.UUID, status string) error {
	ct, err := s.DB.Exec(ctx, `UPDATE accounts SET status=$2,cooldown_until=NULL,updated_at=now() WHERE id=$1`, id, status)
	if err == nil && ct.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ActivateExpiredAccountCooldowns restores routing after a temporary failure
// window. Permanent disabled and invalid states are intentionally untouched.

func (s *Store) ActivateExpiredAccountCooldowns(ctx context.Context) (int64, error) {
	command, err := s.DB.Exec(ctx, `UPDATE accounts SET
		status='active',last_error='',cooldown_until=NULL,updated_at=now()
		WHERE status IN ('rate_limited','cooldown') AND cooldown_until IS NOT NULL AND cooldown_until<=now()`)
	if err != nil {
		return 0, err
	}
	return command.RowsAffected(), nil
}

func (s *Store) UpdateAccountConfig(ctx context.Context, id uuid.UUID, patch AccountConfigPatch) (domain.Account, error) {
	tx, err := s.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return domain.Account{}, err
	}
	defer tx.Rollback(ctx)

	var name, email, proxyURL, workerGroup, routingRole string
	var concurrency, queueCapacity, videoReservedSlots int
	var protectedTokens int64
	err = tx.QueryRow(ctx, `SELECT name,email,proxy_url,image_concurrency,queue_capacity,browser_worker_group,
		routing_role,protected_tokens,video_reserved_slots
		FROM accounts WHERE id=$1 FOR UPDATE`, id).Scan(&name, &email, &proxyURL, &concurrency, &queueCapacity, &workerGroup, &routingRole, &protectedTokens, &videoReservedSlots)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Account{}, ErrNotFound
	}
	if err != nil {
		return domain.Account{}, err
	}
	if patch.Name != nil {
		name = *patch.Name
	}
	if patch.Email != nil {
		email = *patch.Email
	}
	if patch.ProxyURL != nil {
		proxyURL = *patch.ProxyURL
	}
	if patch.ImageConcurrency != nil {
		concurrency = *patch.ImageConcurrency
	}
	if patch.QueueCapacity != nil {
		queueCapacity = *patch.QueueCapacity
	}
	if patch.RoutingRole != nil {
		routingRole = *patch.RoutingRole
	}
	if patch.ProtectedTokens != nil {
		protectedTokens = *patch.ProtectedTokens
	}
	if patch.VideoReservedSlots != nil {
		videoReservedSlots = *patch.VideoReservedSlots
	}
	if patch.BrowserWorkerGroup != nil {
		workerGroup = *patch.BrowserWorkerGroup
	}

	var executingTasks, queuedTasks int
	if err := tx.QueryRow(ctx, `SELECT
		count(*) FILTER (WHERE t.status IN ('reserving','uploading','submitted','polling')),
		count(*) FILTER (WHERE t.status='queued')
		FROM account_reservations r JOIN tasks t ON t.id=r.task_id
		WHERE r.account_id=$1 AND r.state='held'`, id).Scan(&executingTasks, &queuedTasks); err != nil {
		return domain.Account{}, err
	}
	if concurrency < executingTasks || queueCapacity < queuedTasks || videoReservedSlots > concurrency {
		return domain.Account{}, &AccountCapacityInUseError{
			ExecutingTasks:       executingTasks,
			QueuedTasks:          queuedTasks,
			RequestedConcurrency: concurrency,
			RequestedQueue:       queueCapacity,
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE accounts SET name=$2,email=$3,proxy_url=$4,image_concurrency=$5,
		queue_capacity=$6,browser_worker_group=$7,routing_role=$8,protected_tokens=$9,
		video_reserved_slots=$10,updated_at=now() WHERE id=$1`, id, name, email, proxyURL, concurrency, queueCapacity, workerGroup, routingRole, protectedTokens, videoReservedSlots); err != nil {
		return domain.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Account{}, err
	}
	return s.GetAccount(ctx, id)
}

func (s *Store) SetAccountBrowserWorkerGroup(ctx context.Context, id uuid.UUID, group string) error {
	command, err := s.DB.Exec(ctx, `UPDATE accounts SET browser_worker_group=$2,updated_at=now() WHERE id=$1`, id, group)
	if err == nil && command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
