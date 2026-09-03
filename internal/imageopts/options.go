package imageopts

import (
	"fmt"
	"strconv"
	"strings"
)

const GPTImage2 = "gpt-image-2"
const AdobeProvider = "adobe"

const (
	NanoBanana2   = "nano-banana-2"
	NanoBananaPro = "nano-banana-pro"
	Seedream50Pro = "seedream-5.0-pro"
)

var gptImage2Widths = map[int]bool{768: true, 832: true, 848: true, 864: true, 896: true, 928: true, 1024: true, 1136: true, 1152: true, 1184: true, 1200: true, 1248: true, 1264: true, 1344: true, 1376: true, 1536: true, 1584: true, 1648: true, 1696: true, 1792: true, 1856: true, 2016: true, 2048: true, 2336: true, 2448: true, 2560: true, 2880: true, 3200: true, 3264: true, 3504: true, 3584: true, 3808: true}
var gptImage2Heights = map[int]bool{672: true, 768: true, 832: true, 848: true, 864: true, 896: true, 928: true, 1024: true, 1136: true, 1152: true, 1184: true, 1200: true, 1248: true, 1264: true, 1344: true, 1376: true, 1536: true, 1632: true, 1648: true, 1696: true, 1792: true, 1856: true, 2016: true, 2048: true, 2336: true, 2448: true, 2560: true, 2880: true, 3200: true, 3264: true, 3504: true, 3584: true}
var nanoBananaWidths = map[int]bool{768: true, 848: true, 896: true, 928: true, 1024: true, 1152: true, 1200: true, 1264: true, 1376: true, 1536: true, 1584: true, 1696: true, 1792: true, 1856: true, 2048: true, 2304: true, 2400: true, 2528: true, 2752: true, 3072: true, 3168: true, 3392: true, 3584: true, 3712: true, 4096: true, 4608: true, 4800: true, 5056: true, 5504: true, 6336: true}
var nanoBananaHeights = map[int]bool{672: true, 768: true, 848: true, 896: true, 928: true, 1024: true, 1152: true, 1200: true, 1264: true, 1344: true, 1376: true, 1536: true, 1696: true, 1792: true, 1856: true, 2048: true, 2304: true, 2400: true, 2528: true, 2688: true, 2752: true, 3072: true, 3392: true, 3584: true, 3712: true, 4096: true, 4608: true, 4800: true, 5056: true, 5504: true}

func ParseSize(model, size string) (int, int, error) {
	return ParseSizeForProvider("leonardo", model, size)
}

func ParseSizeForProvider(provider, model, size string) (int, int, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		provider = "leonardo"
	}
	if size == "" || size == "auto" {
		return 1024, 1024, nil
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid image size %q", size)
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil {
		return 0, 0, fmt.Errorf("invalid image size %q", size)
	}
	// Creative Fabrica Flow receives the requested dimensions as a canvas
	// hint and then maps them to an aspect-ratio enum upstream.  Unlike the
	// Leonardo image schemas, its account-scoped models expose dimensions up
	// to 8192px per edge, so keep this validation provider-specific.
	if provider == "creativefabrica" {
		if w < 64 || h < 64 || w > 8192 || h > 8192 {
			return 0, 0, fmt.Errorf("invalid Creative Fabrica image size %q", size)
		}
		return w, h, nil
	}
	if provider == AdobeProvider && model == GPTImage2 && size != "1024x1024" && size != "2048x2048" && size != "2880x2880" {
		return 0, 0, fmt.Errorf("gpt-image-2 size must be 1024x1024, 2048x2048 or 2880x2880 on provider adobe")
	}
	if model == GPTImage2 {
		if w <= 0 || h <= 0 || w > 3840 || h > 3840 || w%16 != 0 || h%16 != 0 {
			return 0, 0, fmt.Errorf("gpt-image-2 size edges must be multiples of 16 and at most 3840px")
		}
		short, long := w, h
		if short > long {
			short, long = long, short
		}
		pixels := int64(w) * int64(h)
		if long > short*3 || pixels < 655360 || pixels > 8294400 {
			return 0, 0, fmt.Errorf("gpt-image-2 size must have ratio <= 3:1 and 655360..8294400 total pixels")
		}
		if !gptImage2Widths[w] || !gptImage2Heights[h] {
			return 0, 0, fmt.Errorf("gpt-image-2 size %q is valid for OpenAI but not exposed by Leonardo", size)
		}
		return w, h, nil
	}
	if model == NanoBanana2 || model == NanoBananaPro {
		if w <= 0 || h <= 0 {
			return 0, 0, fmt.Errorf("%s size edges must be positive", model)
		}
		if !nanoBananaWidths[w] || !nanoBananaHeights[h] {
			return 0, 0, fmt.Errorf("%s size %q is not exposed by Leonardo", model, size)
		}
		return w, h, nil
	}
	if model == Seedream50Pro {
		if w < 768 || h < 768 || w > 2048 || h > 2048 {
			return 0, 0, fmt.Errorf("seedream-5.0-pro size edges must be between 768 and 2048px")
		}
		return w, h, nil
	}
	if w < 64 || h < 64 || w > 4096 || h > 4096 {
		return 0, 0, fmt.Errorf("invalid image size %q", size)
	}
	return w, h, nil
}

func BaseModel(model string) string { return model }

func IsGPTImage2(model string) bool { return BaseModel(model) == GPTImage2 }

func IsAdobeGPTImage2(provider, model string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), AdobeProvider) && model == GPTImage2
}

func IsAdobeImageModel(provider, model string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), AdobeProvider) && (model == GPTImage2 || model == NanoBanana2)
}
