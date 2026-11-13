package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"
)

// A tiny PNG that claims to be 100000×100000 pixels must be refused before anything is decoded.
func TestHugePicturesAreRefused(t *testing.T) {
	var buf bytes.Buffer
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	data := buf.Bytes()
	// rewrite the IHDR width and height (and its checksum)
	binary.BigEndian.PutUint32(data[16:], 100000)
	binary.BigEndian.PutUint32(data[20:], 100000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	if _, err := checkPicture(data); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("huge picture accepted: %v", err)
	}
	if _, err := decodePicture(data); err == nil {
		t.Fatal("huge picture decoded")
	}
	if _, err := checkPicture([]byte("<html>not a picture</html>")); err == nil {
		t.Fatal("a web page was taken for a picture")
	}
	buf.Reset()
	png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 20, 10)))
	if ctype, err := checkPicture(buf.Bytes()); err != nil || ctype != "image/png" {
		t.Fatalf("a normal picture was refused: %v %v", ctype, err)
	}
}

// Text a spreadsheet would run as a formula is exported as plain text; ordinary names and numbers are unchanged.
func TestCSVFormulasAreDefused(t *testing.T) {
	for in, want := range map[string]string{
		"=HYPERLINK(\"http://x\")": "'=HYPERLINK(\"http://x\")",
		"+cmd":                     "'+cmd",
		"@SUM(A1)":                 "'@SUM(A1)",
		"-ship":                    "'-ship",
		"-5":                       "-5",
		"Constellation Mk V":       "Constellation Mk V",
		"":                         "",
		"96":                       "96",
	} {
		if got := csvText(in); got != want {
			t.Errorf("csvText(%q) = %q, want %q", in, got, want)
		}
	}
}
