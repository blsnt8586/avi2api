package accounts

import (
	"testing"

	"github.com/leonardo2api/leonardo2api/internal/creativefabrica"
)

func TestCreativeFabricaQuoteValidation(t *testing.T) {
	valid := CreativeFabricaQuoteRequest{
		Kind: "video", Model: "creativefabrica/seedance_v2_5",
		Options:    map[string]any{"duration_seconds": float64(5), "resolution": "720p"},
		InputMedia: []CreativeFabricaQuoteMedia{{Role: "referenceVideo", Metadata: map[string]float64{"durationSeconds": 4.25, "fps": 29.97}}},
	}
	if err := valid.Validate(); err != nil || valid.Model != "seedance_v2_5" {
		t.Fatalf("valid metadata quote: %+v %v", valid, err)
	}
	if valid.InputMedia[0].Metadata["durationSeconds"] != 4 || valid.InputMedia[0].Metadata["fps"] != 30 {
		t.Fatalf("does not match official positive Math.round: %+v", valid.InputMedia)
	}
	tests := []CreativeFabricaQuoteRequest{
		{Kind: "video", Model: "adobe/model"},
		{Kind: "audio", Model: "model"},
		{Kind: "video", Model: "model", Options: map[string]any{"resolution": map[string]any{"url": "https://example.com"}}},
		{Kind: "video", Model: "model", InputMedia: []CreativeFabricaQuoteMedia{{Role: "referenceVideo", Metadata: map[string]float64{"durationSeconds": -1}}}},
		{Kind: "video", Model: "model", InputMedia: []CreativeFabricaQuoteMedia{{Role: "referenceVideo", Metadata: map[string]float64{"unknown": 1}}}},
		{Kind: "video", Model: "model", InputMedia: []CreativeFabricaQuoteMedia{{Role: "referenceVideo", Metadata: map[string]float64{"durationSeconds": 0.1}}}},
		{Kind: "video", Model: "model", InputMedia: []CreativeFabricaQuoteMedia{{Role: "sourceAudio", Metadata: map[string]float64{"durationSeconds": 4.25}}}},
	}
	for _, request := range tests {
		if err := request.Validate(); err == nil {
			t.Fatalf("invalid quote accepted: %+v", request)
		}
	}
}

func TestCreativeFabricaQuotePreservesIntegerWireType(t *testing.T) {
	model := creativefabrica.Model{InputOptions: map[string]any{
		"duration_seconds": map[string]any{"integerType": map[string]any{"min": "4", "max": "30"}},
		"resolution":       map[string]any{"stringType": map[string]any{"allowedValues": []any{"720p", "1080p"}}},
		"aspect_ratio":     map[string]any{"stringType": map[string]any{"allowedValues": []any{"16:9", "9:16"}, "defaultValue": "16:9"}},
	}}
	result, err := creativeFabricaQuoteOptions(model, map[string]any{"duration_seconds": float64(5), "resolution": "720p"})
	if err != nil || result["duration_seconds"] != int64(5) {
		t.Fatalf("integer option would be encoded as a float: %+v %v", result, err)
	}
	if result["aspect_ratio"] != "16:9" {
		t.Fatalf("catalog default not applied: %+v", result)
	}
	for _, options := range []map[string]any{
		{"duration_seconds": float64(3)}, {"duration_seconds": float64(31)},
		{"duration_seconds": 5.5}, {"duration_seconds": "5"}, {"resolution": "8k"},
	} {
		if _, err := creativeFabricaQuoteOptions(model, options); err == nil {
			t.Fatalf("invalid options accepted: %+v", options)
		}
	}
}
