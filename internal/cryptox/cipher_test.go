package cryptox

import "testing"

func TestRoundTrip(t *testing.T) {
	c, err := New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Encrypt("secret cookie")
	if err != nil {
		t.Fatal(err)
	}
	plain, err := c.Decrypt(got)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "secret cookie" {
		t.Fatalf("got %q", plain)
	}
}
func TestRejectsWrongKey(t *testing.T) {
	a, _ := New(make([]byte, 32))
	key := make([]byte, 32)
	key[0] = 1
	b, _ := New(key)
	enc, _ := a.Encrypt("secret")
	if _, err := b.Decrypt(enc); err == nil {
		t.Fatal("expected authentication failure")
	}
}
