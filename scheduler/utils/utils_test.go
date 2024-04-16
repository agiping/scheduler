package utils

import (
	"testing"
	"time"
)

func TestRequestTimestamp(t *testing.T) {
	rt := NewRequestTimestamp()
	rt.Add(nil)
	time.Sleep(1 * time.Second)
	rt.Add(nil)
	time.Sleep(1 * time.Second)
	rt.Add(nil)

	got := rt.String()
	if got == "" {
		t.Errorf("Expected non-empty string, got '%s'", got)
	}

	gotMap := rt.ToMap()
	if len(gotMap) == 0 {
		t.Errorf("Expected non-empty map, got '%v'", gotMap)
	}

	rt.Clear()

	gotAfterClear := rt.String()
	if gotAfterClear != "RequestTimestamp(timestamps=[])" {
		t.Errorf("Expected empty string after Clear, got '%s'", gotAfterClear)
	}
}
