package httpapi

import (
	"context"
	"errors"
	"math"
	"net/http"
	"sort"
	"strings"

	"github.com/leonardo2api/leonardo2api/internal/domain"
	"github.com/leonardo2api/leonardo2api/internal/pricing"
	"github.com/leonardo2api/leonardo2api/internal/store"
	"github.com/leonardo2api/leonardo2api/internal/videospec"
)

const salePricingSettingKey = "sale_pricing_profiles"

type salePricingProfile struct {
	ProviderID       string  `json:"provider_id"`
	Currency         string  `json:"currency"`
	AccountCost      float64 `json:"account_cost"`
	IncludedCredits  int64   `json:"included_credits"`
	UsableCreditRate float64 `json:"usable_credit_rate"`
	OverheadRate     float64 `json:"overhead_rate"`
	PaymentFeeRate   float64 `json:"payment_fee_rate"`
	TargetMargin     float64 `json:"target_margin"`
	RoundingStep     float64 `json:"rounding_step"`
}

type salePricingSettings struct {
	Profiles []salePricingProfile `json:"profiles"`
}

type salePricingQuoteRequest struct {
	Profile       salePricingProfile            `json:"profile"`
	ManualCredits []int64                       `json:"manual_credits"`
	Items         []salePricingQuoteItem        `json:"items"`
	VideoRates    []salePricingVideoRateRequest `json:"video_rates"`
}

type salePricingQuoteItem struct {
	RuleID            int64 `json:"rule_id"`
	GenerateAudio     *bool `json:"generate_audio,omitempty"`
	HasVideoReference bool  `json:"has_video_reference,omitempty"`
}

type salePricingEconomics struct {
	UsableCredits     float64 `json:"usable_credits"`
	LoadedAccountCost float64 `json:"loaded_account_cost"`
	CostPerCredit     float64 `json:"cost_per_credit"`
	SellPerCredit     float64 `json:"sell_per_credit"`
	ProjectedRevenue  float64 `json:"projected_revenue"`
	ProjectedProfit   float64 `json:"projected_profit"`
}

type saleMonetaryQuote struct {
	Credits    int64   `json:"credits"`
	Cost       float64 `json:"cost"`
	Price      float64 `json:"price"`
	PaymentFee float64 `json:"payment_fee"`
	Profit     float64 `json:"profit"`
	Margin     float64 `json:"margin"`
}

type salePricingItemQuote struct {
	RuleID           int64              `json:"rule_id"`
	Available        bool               `json:"available"`
	Error            string             `json:"error,omitempty"`
	BaseCredits      int64              `json:"base_credits"`
	EffectiveCredits int64              `json:"effective_credits"`
	AppliedModifiers []string           `json:"applied_modifiers"`
	Quote            *saleMonetaryQuote `json:"quote,omitempty"`
}

type salePricingQuoteResponse struct {
	Economics    salePricingEconomics        `json:"economics"`
	ManualQuotes []saleMonetaryQuote         `json:"manual_quotes"`
	Items        []salePricingItemQuote      `json:"items"`
	VideoRates   []salePricingVideoRateQuote `json:"video_rates"`
}

type salePricingVideoRateRequest struct {
	Model             string `json:"model"`
	Resolution        string `json:"resolution"`
	GenerateAudio     *bool  `json:"generate_audio,omitempty"`
	HasVideoReference bool   `json:"has_video_reference,omitempty"`
}

type salePricingRateQuote struct {
	Credits    float64 `json:"credits"`
	Cost       float64 `json:"cost"`
	Price      float64 `json:"price"`
	PaymentFee float64 `json:"payment_fee"`
	Profit     float64 `json:"profit"`
	Margin     float64 `json:"margin"`
}

type salePricingVideoRateQuote struct {
	Model                     string                `json:"model"`
	Resolution                string                `json:"resolution"`
	Available                 bool                  `json:"available"`
	Error                     string                `json:"error,omitempty"`
	Durations                 []int                 `json:"durations"`
	BaseCreditsPerSecond      float64               `json:"base_credits_per_second"`
	EffectiveCreditsPerSecond float64               `json:"effective_credits_per_second"`
	AppliedModifiers          []string              `json:"applied_modifiers"`
	Quote                     *salePricingRateQuote `json:"quote,omitempty"`
}

type salePricingProviderRules struct {
	store      *store.Store
	providerID string
}

func (rules salePricingProviderRules) FindModelCostRule(ctx context.Context, kind, model, size, quality, resolution string, duration int) (domain.ModelCostRule, error) {
	return rules.store.FindProviderModelCostRule(ctx, rules.providerID, kind, model, size, quality, resolution, duration)
}

func (s *Server) adminSalePricing(w http.ResponseWriter, r *http.Request) {
	settings := salePricingSettings{Profiles: []salePricingProfile{}}
	if err := s.Store.GetSetting(r.Context(), salePricingSettingKey, &settings); err != nil && !errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	if settings.Profiles == nil {
		settings.Profiles = []salePricingProfile{}
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) adminUpdateSalePricing(w http.ResponseWriter, r *http.Request) {
	var settings salePricingSettings
	if err := decodeJSON(r, &settings); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	providers, err := s.Store.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	providerIDs := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		providerIDs[provider.ID] = struct{}{}
	}
	if err := validateSalePricingSettings(&settings, providerIDs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_sale_pricing", err.Error())
		return
	}
	if err := s.Store.SetSetting(r.Context(), salePricingSettingKey, settings); err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	s.writeAudit(r.Context(), s.Config.AdminUsername, "settings.sale_pricing", "sale_pricing", map[string]any{"profiles": settings.Profiles})
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) adminSalePricingQuote(w http.ResponseWriter, r *http.Request) {
	var req salePricingQuoteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if len(req.ManualCredits) > 20 || len(req.Items) > 100 || len(req.VideoRates) > 100 {
		writeError(w, http.StatusBadRequest, "invalid_sale_pricing_quote", "manual_credits must not exceed 20 entries; items and video_rates must not exceed 100 entries each")
		return
	}
	providers, err := s.Store.ListProviders(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	providerIDs := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		providerIDs[provider.ID] = struct{}{}
	}
	settings := salePricingSettings{Profiles: []salePricingProfile{req.Profile}}
	if err := validateSalePricingSettings(&settings, providerIDs); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_sale_pricing_quote", err.Error())
		return
	}
	profile := settings.Profiles[0]
	economics := calculateSalePricingEconomics(profile)
	response := salePricingQuoteResponse{
		Economics:    economics,
		ManualQuotes: make([]saleMonetaryQuote, 0, len(req.ManualCredits)),
		Items:        make([]salePricingItemQuote, 0, len(req.Items)),
		VideoRates:   make([]salePricingVideoRateQuote, 0, len(req.VideoRates)),
	}
	for _, credits := range req.ManualCredits {
		if credits < 1 || credits > 1_000_000_000_000 {
			writeError(w, http.StatusBadRequest, "invalid_sale_pricing_quote", "manual credits must be between 1 and 1000000000000")
			return
		}
		response.ManualQuotes = append(response.ManualQuotes, calculateSaleMonetaryQuote(profile, economics, credits))
	}
	rules, err := s.Store.ListModelCostRules(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "database_error", err.Error())
		return
	}
	rulesByID := make(map[int64]domain.ModelCostRule, len(rules))
	for _, rule := range rules {
		if rule.Enabled && !rule.Drifted && rule.ProviderID == profile.ProviderID {
			rulesByID[rule.ID] = rule
		}
	}
	for _, item := range req.Items {
		rule, ok := rulesByID[item.RuleID]
		if !ok {
			response.Items = append(response.Items, unavailableSalePricingItem(item.RuleID, "积分规则已失效或不属于当前平台"))
			continue
		}
		effectiveCredits, modifiers, itemErr := s.salePricingEffectiveCredits(r.Context(), rule, item)
		if itemErr != nil {
			result := unavailableSalePricingItem(item.RuleID, itemErr.Error())
			result.BaseCredits = rule.UnitTokens
			response.Items = append(response.Items, result)
			continue
		}
		quote := calculateSaleMonetaryQuote(profile, economics, effectiveCredits)
		response.Items = append(response.Items, salePricingItemQuote{
			RuleID: item.RuleID, Available: true, BaseCredits: rule.UnitTokens,
			EffectiveCredits: effectiveCredits, AppliedModifiers: modifiers, Quote: &quote,
		})
	}
	for _, item := range req.VideoRates {
		response.VideoRates = append(response.VideoRates, s.salePricingVideoRate(r.Context(), profile, economics, rules, item))
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) salePricingVideoRate(ctx context.Context, profile salePricingProfile, economics salePricingEconomics, rules []domain.ModelCostRule, request salePricingVideoRateRequest) salePricingVideoRateQuote {
	request.Model = strings.TrimSpace(request.Model)
	request.Resolution = strings.ToLower(strings.TrimSpace(request.Resolution))
	result := salePricingVideoRateQuote{
		Model: request.Model, Resolution: request.Resolution, Durations: []int{}, AppliedModifiers: []string{},
	}
	if request.Model == "" || request.Resolution == "" {
		result.Error = "model 和 resolution 不能为空"
		return result
	}
	matching := make([]domain.ModelCostRule, 0)
	for _, rule := range rules {
		if rule.Enabled && !rule.Drifted && rule.ProviderID == profile.ProviderID && rule.Kind == "video" &&
			rule.Model == request.Model && strings.EqualFold(rule.Resolution, request.Resolution) && rule.Duration > 0 {
			matching = append(matching, rule)
		}
	}
	if len(matching) == 0 {
		result.Error = "当前模型和分辨率没有有效积分规则"
		return result
	}
	sort.Slice(matching, func(i, j int) bool { return matching[i].Duration < matching[j].Duration })
	for _, rule := range matching {
		effectiveCredits, modifiers, err := s.salePricingEffectiveCredits(ctx, rule, salePricingQuoteItem{
			RuleID: rule.ID, GenerateAudio: request.GenerateAudio, HasVideoReference: request.HasVideoReference,
		})
		if err != nil {
			result.Error = err.Error()
			return result
		}
		result.Durations = append(result.Durations, rule.Duration)
		result.BaseCreditsPerSecond = math.Max(result.BaseCreditsPerSecond, float64(rule.UnitTokens)/float64(rule.Duration))
		result.EffectiveCreditsPerSecond = math.Max(result.EffectiveCreditsPerSecond, float64(effectiveCredits)/float64(rule.Duration))
		result.AppliedModifiers = modifiers
	}
	quote := calculateSaleRateQuote(profile, economics, result.EffectiveCreditsPerSecond)
	result.Available = true
	result.Quote = &quote
	return result
}

func (s *Server) salePricingEffectiveCredits(ctx context.Context, rule domain.ModelCostRule, item salePricingQuoteItem) (int64, []string, error) {
	if rule.Kind != "video" {
		if item.GenerateAudio != nil || item.HasVideoReference {
			return 0, nil, errors.New("视频场景修饰只能用于视频积分规则")
		}
		return rule.UnitTokens, []string{}, nil
	}
	if item.GenerateAudio == nil && !item.HasVideoReference {
		return rule.UnitTokens, []string{}, nil
	}
	spec, ok := videospec.GetForProvider(rule.ProviderID, rule.Model)
	if !ok {
		return 0, nil, errors.New("视频模型缺少参数规范")
	}
	request := domain.VideoRequest{
		Provider: rule.ProviderID, Model: rule.Model, Duration: rule.Duration, Resolution: rule.Resolution,
		GenerateAudio: item.GenerateAudio,
	}
	if len(spec.ResolutionBySize) > 0 {
		size, found := spec.DefaultSizeForResolution(rule.Resolution)
		if !found {
			return 0, nil, errors.New("当前分辨率没有对应尺寸")
		}
		request.Size = size
	}
	if item.HasVideoReference {
		if spec.MaxReferenceVideos < 1 {
			return 0, nil, errors.New("当前模型不支持参考视频")
		}
		request.ReferenceVideos = []domain.SourceMedia{{Filename: "sale-pricing-reference"}}
	}
	if err := normalizeVideoOptions(&request, spec); err != nil {
		return 0, nil, err
	}
	estimate, err := pricing.VideoForProvider(ctx, salePricingProviderRules{store: s.Store, providerID: rule.ProviderID}, rule.ProviderID, request)
	if err != nil {
		return 0, nil, err
	}
	modifiers := make([]string, 0, 2)
	if item.HasVideoReference {
		modifiers = append(modifiers, "reference_video")
	}
	if item.GenerateAudio != nil && !*item.GenerateAudio {
		modifiers = append(modifiers, "native_audio_disabled")
	}
	return estimate.Tokens, modifiers, nil
}

func unavailableSalePricingItem(ruleID int64, message string) salePricingItemQuote {
	return salePricingItemQuote{RuleID: ruleID, Available: false, Error: message, AppliedModifiers: []string{}}
}

func calculateSalePricingEconomics(profile salePricingProfile) salePricingEconomics {
	usableCredits := float64(profile.IncludedCredits) * profile.UsableCreditRate
	loadedAccountCost := profile.AccountCost * (1 + profile.OverheadRate)
	costPerCredit := loadedAccountCost / usableCredits
	sellPerCredit := costPerCredit / (1 - profile.PaymentFeeRate - profile.TargetMargin)
	projectedRevenue := sellPerCredit * usableCredits
	projectedProfit := projectedRevenue*(1-profile.PaymentFeeRate) - loadedAccountCost
	return salePricingEconomics{
		UsableCredits: usableCredits, LoadedAccountCost: loadedAccountCost,
		CostPerCredit: costPerCredit, SellPerCredit: sellPerCredit,
		ProjectedRevenue: projectedRevenue, ProjectedProfit: projectedProfit,
	}
}

func calculateSaleMonetaryQuote(profile salePricingProfile, economics salePricingEconomics, credits int64) saleMonetaryQuote {
	cost := economics.CostPerCredit * float64(credits)
	price := roundSalePrice(economics.SellPerCredit*float64(credits), profile.RoundingStep)
	paymentFee := price * profile.PaymentFeeRate
	profit := price - paymentFee - cost
	margin := 0.0
	if price > 0 {
		margin = profit / price
	}
	return saleMonetaryQuote{
		Credits: credits, Cost: cost, Price: price, PaymentFee: paymentFee,
		Profit: profit, Margin: margin,
	}
}

func calculateSaleRateQuote(profile salePricingProfile, economics salePricingEconomics, credits float64) salePricingRateQuote {
	cost := economics.CostPerCredit * credits
	price := roundSalePrice(cost/(1-profile.PaymentFeeRate-profile.TargetMargin), profile.RoundingStep)
	paymentFee := price * profile.PaymentFeeRate
	profit := price - paymentFee - cost
	margin := 0.0
	if price > 0 {
		margin = profit / price
	}
	return salePricingRateQuote{
		Credits: credits, Cost: cost, Price: price, PaymentFee: paymentFee,
		Profit: profit, Margin: margin,
	}
}

func roundSalePrice(value, step float64) float64 {
	quotient := value / step
	nearest := math.Round(quotient)
	tolerance := 1e-10 * math.Max(1, math.Abs(quotient))
	units := nearest
	if math.Abs(quotient-nearest) > tolerance {
		units = math.Ceil(quotient)
	}
	return math.Round(units*step*1e9) / 1e9
}

func validateSalePricingSettings(settings *salePricingSettings, providerIDs map[string]struct{}) error {
	if len(settings.Profiles) > 100 {
		return errors.New("profiles must not exceed 100 entries")
	}
	seen := make(map[string]struct{}, len(settings.Profiles))
	for i := range settings.Profiles {
		profile := &settings.Profiles[i]
		profile.ProviderID = strings.TrimSpace(profile.ProviderID)
		profile.Currency = strings.ToUpper(strings.TrimSpace(profile.Currency))
		if _, ok := providerIDs[profile.ProviderID]; !ok {
			return errors.New("provider_id does not reference a configured provider")
		}
		if _, ok := seen[profile.ProviderID]; ok {
			return errors.New("each provider can have only one sale pricing profile")
		}
		seen[profile.ProviderID] = struct{}{}
		if !validCurrencyCode(profile.Currency) {
			return errors.New("currency must be a three-letter code")
		}
		if !finitePositive(profile.AccountCost) || profile.AccountCost > 1_000_000_000 {
			return errors.New("account_cost must be greater than zero and no more than 1000000000")
		}
		if profile.IncludedCredits < 1 || profile.IncludedCredits > 1_000_000_000_000 {
			return errors.New("included_credits must be between 1 and 1000000000000")
		}
		if !finiteRate(profile.UsableCreditRate) || profile.UsableCreditRate <= 0 {
			return errors.New("usable_credit_rate must be greater than zero and no more than one")
		}
		if !finiteNonNegative(profile.OverheadRate) || profile.OverheadRate > 10 {
			return errors.New("overhead_rate must be between zero and ten")
		}
		if !finiteRate(profile.PaymentFeeRate) || !finiteRate(profile.TargetMargin) {
			return errors.New("payment_fee_rate and target_margin must be between zero and one")
		}
		if profile.PaymentFeeRate+profile.TargetMargin >= 0.99 {
			return errors.New("payment_fee_rate plus target_margin must be less than 0.99")
		}
		if !finitePositive(profile.RoundingStep) || profile.RoundingStep > 1000 {
			return errors.New("rounding_step must be greater than zero and no more than 1000")
		}
	}
	if settings.Profiles == nil {
		settings.Profiles = []salePricingProfile{}
	}
	return nil
}

func validCurrencyCode(value string) bool {
	if len(value) != 3 {
		return false
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return false
		}
	}
	return true
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finiteRate(value float64) bool {
	return finiteNonNegative(value) && value < 1
}
