package store

import "testing"

func TestSystemCapacityConfigValidation(t *testing.T) {
	valid := SystemCapacityConfig{
		MaxExecuting:         100,
		MaxQueued:            1000,
		QueueHighWatermark:   900,
		QueueResumeWatermark: 700,
		QueueTimeoutSeconds:  1800,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid config: %v", err)
	}
	tests := []SystemCapacityConfig{
		{MaxExecuting: 0, MaxQueued: 1000, QueueHighWatermark: 900, QueueResumeWatermark: 700, QueueTimeoutSeconds: 1800},
		{MaxExecuting: 100, MaxQueued: 1000, QueueHighWatermark: 1001, QueueResumeWatermark: 700, QueueTimeoutSeconds: 1800},
		{MaxExecuting: 100, MaxQueued: 1000, QueueHighWatermark: 900, QueueResumeWatermark: 900, QueueTimeoutSeconds: 1800},
		{MaxExecuting: 100, MaxQueued: 1000, QueueHighWatermark: 900, QueueResumeWatermark: 700, QueueTimeoutSeconds: 59},
		{MaxExecuting: 100, MaxQueued: 1000, QueueHighWatermark: 900, QueueResumeWatermark: 700, QueueTimeoutSeconds: 1800, ExecutionPaused: true},
	}
	for index, config := range tests {
		if err := config.Validate(); err == nil {
			t.Fatalf("case %d unexpectedly valid: %+v", index, config)
		}
	}
}
