package headroom

import "testing"

func TestHello(t *testing.T) {
	got := Hello()
	want := "headroom-core"
	if got != want {
		t.Errorf("Hello() = %q, want %q", got, want)
	}
}
