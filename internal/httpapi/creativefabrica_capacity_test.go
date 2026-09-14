package httpapi

import "testing"

func TestCreativeFabricaConcurrencyLimit(t *testing.T) {
	for _, value := range []int{1, 5, 10} {
		if got, err := normalizeAccountConcurrency("creativefabrica", value); err != nil || got != value {
			t.Fatalf("capacity %d: got %d, %v", value, got, err)
		}
	}
	if _, err := normalizeAccountConcurrency("creativefabrica", 11); err == nil {
		t.Fatal("expected rejection above configured ceiling")
	}
	if _, err := normalizeAccountConcurrency("leonardo", 10); err == nil {
		t.Fatal("Leonardo ceiling must remain unchanged")
	}
}
