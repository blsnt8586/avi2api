package creativefabrica

import (
	"context"
	"errors"
)

// CalculateFlowGenerationCost is Studio's separate whole-Flow estimator.
// It must not be confused with the preset calculator's per-model quote.
func (c *Client) CalculateFlowGenerationCost(ctx context.Context, token string, settings map[string]any) (int64, error) {
	data, err := c.RPC(ctx, "FlowService", "CalculateGenerationCost", c.FlowURL, flowServicePath, token, map[string]any{"settings": settings})
	if err != nil {
		return 0, err
	}
	coins, present := firstPresentInt(data, "totalCoinAmount", "total_coin_amount")
	if !present || coins < 0 {
		return 0, errors.New("Creative Fabrica Flow cost response has no non-negative total amount")
	}
	return coins, nil
}
