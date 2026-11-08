package uiguard

import "testing"

func TestOffUIRecordsOnlyTheUIGoroutine(t *testing.T) {
	Reset()
	MarkUI()
	defer Unmark()
	if !OnUI() {
		t.Fatal("OnUI false on the marked goroutine")
	}
	done := make(chan bool)
	go func() {
		OffUI("background")
		done <- OnUI()
	}()
	if <-done {
		t.Fatal("OnUI true on another goroutine")
	}
	if v := Violations(); len(v) != 0 {
		t.Fatalf("violations from another goroutine: %v", v)
	}
	OffUI("exec")
	OffUI("exec")
	if v := Violations(); len(v) != 2 || v[0] != "exec" {
		t.Fatalf("violations = %v", v)
	}
	Reset()
	if len(Violations()) != 0 {
		t.Fatal("Reset kept violations")
	}
}
