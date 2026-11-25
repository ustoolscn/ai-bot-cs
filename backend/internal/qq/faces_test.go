package qq

import "testing"

func TestDisplayFaces(t *testing.T) {
	body := `你好<faceType=1,faceId="13",ext="eyJ0ZXh0IjoiW+WRsueJmV0ifQ=="><faceType=2,ext="???">🙂`
	if got := displayFaces(body); got != "你好[呲牙][表情]🙂" {
		t.Fatal(got)
	}
}
