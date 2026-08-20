package service

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateImageAcceptsPNG(t *testing.T) {
	ext, err := validateImage("foto.png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	if err != nil {
		t.Fatalf("validateImage() error = %v", err)
	}
	if ext != "png" {
		t.Errorf("ext = %q, want png", ext)
	}
}

func TestValidateImageRejectsNonImage(t *testing.T) {
	_, err := validateImage("script.php", []byte("<?php echo 1; ?>"))
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
}

func TestValidateImageRejectsExtensionMismatch(t *testing.T) {
	// PNG bytes but .jpg extension
	_, err := validateImage("fake.jpg", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want ValidationError on extension mismatch", err)
	}
}

func TestValidateImageRejectsOversize(t *testing.T) {
	big := append([]byte{0xFF, 0xD8, 0xFF}, make([]byte, maxImageSize+1)...)
	_, err := validateImage("big.jpg", big)
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want ValidationError on oversize", err)
	}
}

func TestNewUUIDFormat(t *testing.T) {
	id := newUUID()
	parts := strings.Split(id, "-")
	if len(parts) != 5 || len(parts[0]) != 8 || len(parts[1]) != 4 || len(parts[2]) != 4 || len(parts[3]) != 4 || len(parts[4]) != 12 {
		t.Errorf("newUUID() = %q, want UUID v4 shape", id)
	}
	if id[14] != '4' {
		t.Errorf("newUUID() = %q, want version 4 (char 15 = '4')", id)
	}
}

func TestExtractKeyFromPublicURL(t *testing.T) {
	key := "products/1234.png"
	url := "https://ref.supabase.co/storage/v1/object/public/images/products/1234.png"
	if got := extractKey(url); got != key {
		t.Errorf("extractKey(%q) = %q, want %q", url, got, key)
	}
}
