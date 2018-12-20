package storage

import (
	"os"
	"testing"
)

func TestNewStorage(t *testing.T) {
	s, err := NewStorage("./__test-db")
	if err != nil {
		t.Fatalf("unexpected error on storage creating: %v", err)
	}
	if s == nil {
		t.Fatal("unexpected nil storage value right after creating")
	}
	os.Remove("./__test-db")
}
