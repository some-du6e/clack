package inputidle

import (
	"testing"
	"time"
)

func TestSince(t *testing.T) {
	if d := Since(); d < 0 {
		t.Fatalf("Since before any input: %v", d)
	}
	time.Sleep(20 * time.Millisecond)
	Note()
	if d := Since(); d >= 20*time.Millisecond {
		t.Fatalf("Since right after Note: %v", d)
	}
	time.Sleep(30 * time.Millisecond)
	if d := Since(); d < 30*time.Millisecond {
		t.Fatalf("Since 30 ms after Note: %v", d)
	}
}
