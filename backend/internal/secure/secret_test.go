package secure

import "testing"

func TestCipherRoundTrip(t *testing.T) {
	c, err := NewCipher([]byte("12345678901234567890123456789012"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := c.Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Decrypt(v)
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret" || v == "secret" {
		t.Fatalf("unexpected round trip: %q", got)
	}
}
