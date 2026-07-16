package knowledge

import "testing"

func TestChunkUnicodeOverlap(t *testing.T) {
	got := Chunk("一二三四五六七八九十", 4, 1)
	want := []string{"一二三四", "四五六七", "七八九十"}
	if len(got) != len(want) {
		t.Fatalf("got %#v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("chunk %d=%q", i, got[i])
		}
	}
}
