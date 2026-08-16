package providers

import "testing"

func TestRegistryIncludesLeonardo(t *testing.T) {
	r := NewRegistry()
	descriptor, err := r.Get(Leonardo)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.AuthType != "browser_session" || len(descriptor.Capabilities) != 3 {
		t.Fatalf("unexpected descriptor: %#v", descriptor)
	}
}

func TestRegistryRejectsUnknownProvider(t *testing.T) {
	if _, err := NewRegistry().Get("fixture"); err != ErrUnsupported {
		t.Fatalf("expected ErrUnsupported, got %v", err)
	}
}

func TestRegistryIncludesAdobe(t *testing.T) {
	descriptor, err := NewRegistry().Get(Adobe)
	if err != nil || descriptor.AuthType != "oauth" || len(descriptor.Capabilities) != 2 {
		t.Fatalf("unexpected Adobe descriptor: %#v %v", descriptor, err)
	}
}

func TestRegistryResolvesProviderModelsAndCapabilities(t *testing.T) {
	registry := NewRegistry()
	model, err := registry.ResolveModel("image", Adobe, "gpt-image-2")
	if err != nil || model != "gpt-image-2" {
		t.Fatalf("Adobe image route = %q, %v", model, err)
	}
	if _, err := registry.ResolveModel("audio", Adobe, "music-v1"); err == nil {
		t.Fatal("Adobe audio route was accepted")
	}
	registry.Register(Descriptor{
		ID: "fixture", Capabilities: []string{"image"},
		ResolveModel: func(_, model string) (string, error) { return model, nil },
	})
	model, err = registry.ResolveModel("image", "fixture", "fixture-image")
	if err != nil || model != "fixture-image" {
		t.Fatalf("registered provider route = %q, %v", model, err)
	}
}
