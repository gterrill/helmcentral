package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// Synthetic forms for the tests, made with pdfcpu so no real insurer or
// marina paperwork ever sits in the repository.

func createPDF(t *testing.T, json string) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := api.Create(context.Background(), nil, bytes.NewReader([]byte(json)), &out, nil); err != nil {
		t.Fatalf("create fixture PDF: %v", err)
	}
	return out.Bytes()
}

func writeFixturePDF(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// acroFormFixture is a one-page form with two text fields, a check box, a
// radio group and a combo box.
func acroFormFixture(t *testing.T) []byte {
	return createPDF(t, `{
 "paper": "A4P",
 "origin": "LowerLeft",
 "pages": {"1": {"content": {
  "text": [
   {"value": "Berth application", "pos": [50, 780], "font": {"name": "Helvetica", "size": 14}},
   {"value": "Owner name", "pos": [50, 735], "font": {"name": "Helvetica", "size": 10}},
   {"value": "Vessel length", "pos": [50, 705], "font": {"name": "Helvetica", "size": 10}},
   {"value": "Needs shore power", "pos": [70, 675], "font": {"name": "Helvetica", "size": 10}}
  ],
  "textfield": [
   {"id": "owner_name", "pos": [150, 730], "width": 200, "height": 18, "font": {"name": "Helvetica", "size": 10}},
   {"id": "vessel_length", "pos": [150, 700], "width": 80, "height": 18, "font": {"name": "Helvetica", "size": 10}}
  ],
  "checkbox": [{"id": "shore_power", "pos": [50, 672], "width": 12}],
  "radiobuttongroup": [{"id": "berth_type", "pos": [50, 640], "width": 12, "buttons": {"values": ["Alongside", "Stern to"], "label": {"value": "Berth type", "width": 60, "pos": "left", "font": {"name": "Helvetica", "size": 10}}}, "value": "Alongside"}],
  "combobox": [{"id": "currency", "pos": [150, 600], "width": 100, "height": 18, "options": ["AUD", "NZD", "USD"], "font": {"name": "Helvetica", "size": 10}}]
 }}}}`)
}

// flatFormFixture is a one-page form with no fields: labels with blanks after
// them and a short list of tick boxes drawn as small stroked squares.
func flatFormFixture(t *testing.T) []byte {
	return createPDF(t, `{
 "paper": "A4P",
 "origin": "LowerLeft",
 "pages": {"1": {"content": {
  "text": [
   {"value": "Storm declaration", "pos": [50, 780], "font": {"name": "Helvetica", "size": 14}},
   {"value": "Vessel owner", "pos": [50, 730], "font": {"name": "Helvetica", "size": 11}},
   {"value": "Policy number", "pos": [50, 700], "font": {"name": "Helvetica", "size": 11}},
   {"value": "Tick the marina where the vessel is kept", "pos": [50, 660], "font": {"name": "Helvetica", "size": 11}},
   {"value": "North Harbour", "pos": [70, 630], "font": {"name": "Helvetica", "size": 10}},
   {"value": "South Cove", "pos": [70, 610], "font": {"name": "Helvetica", "size": 10}},
   {"value": "Signed", "pos": [50, 100], "font": {"name": "Helvetica", "size": 11}}
  ],
  "box": [
   {"pos": [50, 628], "width": 10, "height": 10, "border": {"width": 1}},
   {"pos": [50, 608], "width": 10, "height": 10, "border": {"width": 1}}
  ]
 }}}}`)
}
