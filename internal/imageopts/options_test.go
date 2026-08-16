package imageopts

import "testing"

func TestGPTImage2SizeConstraints(t *testing.T) {
	valid := []string{"1024x1024", "1536x1024", "2048x2048", "3584x2016", "auto"}
	for _, size := range valid {
		if _, _, err := ParseSize(GPTImage2, size); err != nil {
			t.Fatalf("expected %s to be valid: %v", size, err)
		}
	}
	invalid := []string{"1000x1000", "3840x128", "512x512", "3840x3840", "3840x2160", "1024x0"}
	for _, size := range invalid {
		if _, _, err := ParseSize(GPTImage2, size); err == nil {
			t.Fatalf("expected %s to be invalid", size)
		}
	}
}

func TestAdobeGPTImage2UsesPublishedProviderTiers(t *testing.T) {
	for _, size := range []string{"auto", "1024x1024", "2048x2048", "2880x2880"} {
		if _, _, err := ParseSizeForProvider(AdobeProvider, GPTImage2, size); err != nil {
			t.Fatalf("expected %s to be valid: %v", size, err)
		}
	}
	if _, _, err := ParseSizeForProvider(AdobeProvider, GPTImage2, "1536x1024"); err == nil {
		t.Fatal("expected non-tier Adobe size to be rejected")
	}
}

func TestNanoBananaSizeConstraints(t *testing.T) {
	for _, model := range []string{NanoBanana2, NanoBananaPro} {
		for _, size := range []string{"1024x1024", "2048x2048", "4096x4096", "5504x3072", "768x1344"} {
			if _, _, err := ParseSize(model, size); err != nil {
				t.Fatalf("expected %s %s to be valid: %v", model, size, err)
			}
		}
		for _, size := range []string{"1000x1000", "64x64", "4095x4095", "0x1024"} {
			if _, _, err := ParseSize(model, size); err == nil {
				t.Fatalf("expected %s %s to be invalid", model, size)
			}
		}
	}
}

func TestSeedream50ProSizeConstraints(t *testing.T) {
	for _, size := range []string{"768x768", "2048x2048", "1792x1008", "1024x1536"} {
		if _, _, err := ParseSize(Seedream50Pro, size); err != nil {
			t.Fatalf("expected %s to be valid: %v", size, err)
		}
	}
	for _, size := range []string{"767x1024", "1024x2049", "2049x768"} {
		if _, _, err := ParseSize(Seedream50Pro, size); err == nil {
			t.Fatalf("expected %s to be invalid", size)
		}
	}
}
