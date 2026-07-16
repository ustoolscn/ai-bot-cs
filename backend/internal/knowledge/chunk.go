package knowledge

func Chunk(text string, size, overlap int) []string {
	r := []rune(text)
	if size <= 0 || len(r) == 0 {
		return nil
	}
	if overlap < 0 {
		overlap = 0
	}
	if overlap >= size {
		overlap = size / 4
	}
	var out []string
	for start := 0; start < len(r); {
		end := start + size
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[start:end]))
		if end == len(r) {
			break
		}
		start = end - overlap
	}
	return out
}
