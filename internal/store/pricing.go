package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

func scanCostRule(row pgx.Row, rule *domain.ModelCostRule) error {
	return row.Scan(&rule.ID, &rule.ProviderID, &rule.Kind, &rule.Model, &rule.Size, &rule.Quality, &rule.Resolution, &rule.Duration, &rule.UnitTokens, &rule.Enabled, &rule.PriceVersion, &rule.Source, &rule.Drifted, &rule.DriftReason, &rule.VerifiedAt, &rule.CreatedAt, &rule.UpdatedAt)
}

const costRuleColumns = `id,provider_id,kind,model,size,quality,resolution,duration,unit_tokens,enabled,price_version,source,drifted,drift_reason,verified_at,created_at,updated_at`

func (s *Store) FindModelCostRule(ctx context.Context, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	return s.FindProviderModelCostRule(ctx, "leonardo", kind, model, size, quality, resolution, duration)
}

func (s *Store) FindProviderModelCostRule(ctx context.Context, providerID, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	var rule domain.ModelCostRule
	err := scanCostRule(s.DB.QueryRow(ctx, `SELECT `+costRuleColumns+` FROM model_cost_rules WHERE enabled=true AND drifted=false AND provider_id=$1 AND kind=$2 AND model=$3 AND size=$4 AND quality=$5 AND resolution=$6 AND duration=$7 ORDER BY updated_at DESC,id DESC LIMIT 1`, providerID, kind, model, size, quality, resolution, duration), &rule)
	if errors.Is(err, pgx.ErrNoRows) {
		return rule, ErrNotFound
	}
	return rule, err
}

func (s *Store) IsTaskPricingRuleActive(ctx context.Context, taskID uuid.UUID) (bool, error) {
	var active bool
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM tasks t JOIN model_cost_rules r ON r.id=t.pricing_rule_id
		WHERE t.id=$1 AND r.provider_id=t.provider_id AND r.enabled=true AND r.drifted=false
	)`, taskID).Scan(&active)
	return active, err
}

func (s *Store) ListModelCostRules(ctx context.Context) ([]domain.ModelCostRule, error) {
	return s.ListModelCostRulesFiltered(ctx, "", "")
}

func (s *Store) ListModelCostRulesFiltered(ctx context.Context, providerID, kind string) ([]domain.ModelCostRule, error) {
	rows, err := s.DB.Query(ctx, `SELECT `+costRuleColumns+` FROM model_cost_rules
		WHERE ($1='' OR provider_id=$1) AND ($2='' OR kind=$2)
		ORDER BY provider_id,kind,model,size,quality,resolution,duration,price_version DESC`, providerID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ModelCostRule
	for rows.Next() {
		var rule domain.ModelCostRule
		if err := scanCostRule(rows, &rule); err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func (s *Store) CreateModelCostRule(ctx context.Context, rule domain.ModelCostRule) (domain.ModelCostRule, error) {
	if rule.ProviderID == "" {
		rule.ProviderID = "leonardo"
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return domain.ModelCostRule{}, err
	}
	defer tx.Rollback(ctx)
	if rule.Enabled {
		if _, err := tx.Exec(ctx, `UPDATE model_cost_rules SET enabled=false,updated_at=now()
			WHERE enabled=true AND provider_id=$1 AND kind=$2 AND model=$3 AND size=$4 AND quality=$5 AND resolution=$6 AND duration=$7`,
			rule.ProviderID, rule.Kind, rule.Model, rule.Size, rule.Quality, rule.Resolution, rule.Duration); err != nil {
			return domain.ModelCostRule{}, err
		}
	}
	var created domain.ModelCostRule
	err = scanCostRule(tx.QueryRow(ctx, `INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,enabled,price_version,source,drifted,drift_reason,verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,false,'',now()) RETURNING `+costRuleColumns, rule.ProviderID, rule.Kind, rule.Model, rule.Size, rule.Quality, rule.Resolution, rule.Duration, rule.UnitTokens, rule.Enabled, rule.PriceVersion, rule.Source), &created)
	if err != nil {
		return created, err
	}
	return created, tx.Commit(ctx)
}

// ReplaceModelCostRules installs a provider pricing snapshot atomically. Old
// rules remain in the table for task-history reconciliation, while only the
// newly supplied rule for each parameter tuple is enabled. A single
// transaction prevents a partially synchronized catalog from becoming the
// active admission source.
func (s *Store) ReplaceModelCostRules(ctx context.Context, rules []domain.ModelCostRule) (int, error) {
	if len(rules) == 0 {
		return 0, nil
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// A successful upstream snapshot is authoritative for the provider/kind
	// pairs it contains. Disable the previous snapshot first; historical rows
	// stay available for settled-task references.
	type scope struct{ provider, kind string }
	scopes := make(map[scope]struct{})
	for _, rule := range rules {
		provider := rule.ProviderID
		if provider == "" {
			provider = "leonardo"
		}
		scopes[scope{provider: provider, kind: rule.Kind}] = struct{}{}
	}
	for current := range scopes {
		if _, err := tx.Exec(ctx, `UPDATE model_cost_rules SET enabled=false,updated_at=now()
			WHERE enabled=true AND provider_id=$1 AND kind=$2`, current.provider, current.kind); err != nil {
			return 0, err
		}
	}
	count := 0
	for _, rule := range rules {
		if rule.ProviderID == "" {
			rule.ProviderID = "leonardo"
		}
		if _, err := tx.Exec(ctx, `INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,enabled,price_version,source,drifted,drift_reason,verified_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,false,'',now())
			ON CONFLICT(provider_id,kind,model,size,quality,resolution,duration,price_version)
			DO UPDATE SET unit_tokens=EXCLUDED.unit_tokens,enabled=EXCLUDED.enabled,source=EXCLUDED.source,
				drifted=false,drift_reason='',verified_at=now(),updated_at=now()`,
			rule.ProviderID, rule.Kind, rule.Model, rule.Size, rule.Quality, rule.Resolution, rule.Duration,
			rule.UnitTokens, rule.Enabled, rule.PriceVersion, rule.Source); err != nil {
			return count, err
		}
		count++
	}
	if err := tx.Commit(ctx); err != nil {
		return count, err
	}
	return count, nil
}

func (s *Store) UpdateModelCostRule(ctx context.Context, rule domain.ModelCostRule) (domain.ModelCostRule, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return domain.ModelCostRule{}, err
	}
	defer tx.Rollback(ctx)
	var existing domain.ModelCostRule
	err = scanCostRule(tx.QueryRow(ctx, `SELECT `+costRuleColumns+` FROM model_cost_rules WHERE id=$1 FOR UPDATE`, rule.ID), &existing)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ModelCostRule{}, ErrNotFound
	}
	if err != nil {
		return domain.ModelCostRule{}, err
	}
	if rule.ProviderID == "" {
		rule.ProviderID = existing.ProviderID
	}
	sameDefinition := existing.ProviderID == rule.ProviderID && existing.Kind == rule.Kind && existing.Model == rule.Model && existing.Size == rule.Size &&
		existing.Quality == rule.Quality && existing.Resolution == rule.Resolution && existing.Duration == rule.Duration &&
		existing.UnitTokens == rule.UnitTokens && existing.PriceVersion == rule.PriceVersion && existing.Source == rule.Source
	if sameDefinition {
		if rule.Enabled {
			if _, err := tx.Exec(ctx, `UPDATE model_cost_rules SET enabled=false,updated_at=now()
				WHERE id<>$1 AND enabled=true AND provider_id=$2 AND kind=$3 AND model=$4 AND size=$5 AND quality=$6 AND resolution=$7 AND duration=$8`,
				rule.ID, rule.ProviderID, rule.Kind, rule.Model, rule.Size, rule.Quality, rule.Resolution, rule.Duration); err != nil {
				return domain.ModelCostRule{}, err
			}
		}
		var updated domain.ModelCostRule
		if err := scanCostRule(tx.QueryRow(ctx, `UPDATE model_cost_rules SET enabled=$2,drifted=false,drift_reason='',verified_at=now(),updated_at=now() WHERE id=$1 RETURNING `+costRuleColumns, rule.ID, rule.Enabled), &updated); err != nil {
			return domain.ModelCostRule{}, err
		}
		return updated, tx.Commit(ctx)
	}
	if _, err := tx.Exec(ctx, `UPDATE model_cost_rules SET enabled=false,updated_at=now() WHERE id=$1`, rule.ID); err != nil {
		return domain.ModelCostRule{}, err
	}
	if rule.Enabled {
		if _, err := tx.Exec(ctx, `UPDATE model_cost_rules SET enabled=false,updated_at=now()
			WHERE enabled=true AND provider_id=$1 AND kind=$2 AND model=$3 AND size=$4 AND quality=$5 AND resolution=$6 AND duration=$7`,
			rule.ProviderID, rule.Kind, rule.Model, rule.Size, rule.Quality, rule.Resolution, rule.Duration); err != nil {
			return domain.ModelCostRule{}, err
		}
	}
	var created domain.ModelCostRule
	if err := scanCostRule(tx.QueryRow(ctx, `INSERT INTO model_cost_rules(provider_id,kind,model,size,quality,resolution,duration,unit_tokens,enabled,price_version,source,drifted,drift_reason,verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,false,'',now()) RETURNING `+costRuleColumns,
		rule.ProviderID, rule.Kind, rule.Model, rule.Size, rule.Quality, rule.Resolution, rule.Duration, rule.UnitTokens, rule.Enabled, rule.PriceVersion, rule.Source), &created); err != nil {
		return domain.ModelCostRule{}, err
	}
	return created, tx.Commit(ctx)
}

func (s *Store) DeleteModelCostRule(ctx context.Context, id int64) error {
	command, err := s.DB.Exec(ctx, `DELETE FROM model_cost_rules WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
