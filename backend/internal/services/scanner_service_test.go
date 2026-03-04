package services

import (
	"testing"

	"gorm.io/gorm"
)

func TestScannerService(t *testing.T) {
	db := &gorm.DB{}
	s := NewScannerService(db)

	if s == nil {
		t.Fatal("Expected ScannerService to be initialized")
	}
}
