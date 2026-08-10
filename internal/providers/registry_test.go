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
