package storage

import (
	"context"
	"testing"
)

func TestLocalStorage(t *testing.T) {
	s, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.Save(context.Background(), "guide.md", []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Read(context.Background(), key)
	if err != nil || string(got) != "hello" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if err = s.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Save(context.Background(), "bad.exe", []byte("x")); err == nil {
		t.Fatal("unsupported extension accepted")
	}
}
