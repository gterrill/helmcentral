package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// terrariumPNG builds a 2x2 Terrarium tile: one deep-sea pixel, one pixel a
// hair under sea level, one at exactly 0 m and one above water.
func terrariumPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: 119, G: 10, B: 20, A: 255})   // about -2200 m
	img.Set(1, 0, color.NRGBA{R: 127, G: 255, B: 255, A: 255}) // just under 0 m
	img.Set(0, 1, color.NRGBA{R: 128, G: 0, B: 0, A: 255})     // exactly 0 m
	img.Set(1, 1, color.NRGBA{R: 128, G: 40, B: 128, A: 255})  // about 40.5 m
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func terrainRequest(z, x, y string) (echo.Context, *httptest.ResponseRecorder) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/terrain/"+z+"/"+x+"/"+y, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("z", "x", "y")
	c.SetParamValues(z, x, y)
	return c, rec
}

func TestClampSeaToZero(t *testing.T) {
	out, err := clampTerrainSeaToZero(terrariumPNG(t))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]int]color.NRGBA{
		{0, 0}: {R: 128, G: 0, B: 0, A: 255},
		{1, 0}: {R: 128, G: 0, B: 0, A: 255},
		{0, 1}: {R: 128, G: 0, B: 0, A: 255},
		{1, 1}: {R: 128, G: 40, B: 128, A: 255},
	}
	for p, w := range want {
		got := color.NRGBAModel.Convert(img.At(p[0], p[1])).(color.NRGBA)
		if got != w {
			t.Errorf("pixel %v = %v, want %v", p, got, w)
		}
	}
}

func TestClampSeaToZeroRejectsNonPNG(t *testing.T) {
	if _, err := clampTerrainSeaToZero([]byte("not a png")); err == nil {
		t.Fatal("expected an error for a body that is not a PNG")
	}
}

func TestTerrainHandlerServesClampedTileAndCachesIt(t *testing.T) {
	cache := newTestTileCache(t)
	var gotURL string
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		gotURL = req.URL.String()
		return fakeUpstreamResponse(200, "image/png", terrariumPNG(t)), nil
	}}
	h := proxyTerrainTileHandler(cache, fetcher)

	c, rec := terrainRequest("5", "3", "4")
	if err := h(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotURL != "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/5/3/4.png" {
		t.Fatalf("upstream url = %s", gotURL)
	}
	if rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("content type = %s", rec.Header().Get("Content-Type"))
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if got := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); got.R != 128 || got.G != 0 || got.B != 0 {
		t.Fatalf("sea pixel not clamped: %v", got)
	}

	c2, rec2 := terrainRequest("5", "3", "4")
	if err := h(c2); err != nil {
		t.Fatal(err)
	}
	if rec2.Code != http.StatusOK || fetcher.callCount() != 1 {
		t.Fatalf("second request: status %d, upstream calls %d (want cache hit, 1 call)", rec2.Code, fetcher.callCount())
	}
}

func TestTerrainHandlerClampsDeepRequests(t *testing.T) {
	cache := newTestTileCache(t)
	var gotURL string
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		gotURL = req.URL.String()
		return fakeUpstreamResponse(200, "image/png", terrariumPNG(t)), nil
	}}
	c, rec := terrainRequest("17", "1000", "2000")
	if err := proxyTerrainTileHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if gotURL != "https://s3.amazonaws.com/elevation-tiles-prod/terrarium/15/250/500.png" {
		t.Fatalf("upstream url = %s", gotURL)
	}
}

func TestTerrainHandlerUpstreamFailureIs502WithNoSubstitute(t *testing.T) {
	cache := newTestTileCache(t)
	// A coarser tile is already cached; it must NOT stand in.
	if err := cache.put(terrainSource, 4, 1, 2, []byte("coarse"), "image/png"); err != nil {
		t.Fatal(err)
	}
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(500, "text/plain", []byte("boom")), nil
	}}
	c, rec := terrainRequest("5", "3", "4")
	if err := proxyTerrainTileHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache-control = %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Body.Len() != 0 {
		t.Fatal("failure must not carry a substitute tile")
	}
	if _, _, ok, _ := cache.get(terrainSource, 5, 3, 4); ok {
		t.Fatal("a failed fetch must not be cached")
	}
}

func TestTerrainHandlerCorruptUpstreamIs502(t *testing.T) {
	cache := newTestTileCache(t)
	fetcher := &fakeTileFetcher{responder: func(req *http.Request) (*http.Response, error) {
		return fakeUpstreamResponse(200, "image/png", []byte("garbage")), nil
	}}
	c, rec := terrainRequest("5", "3", "4")
	if err := proxyTerrainTileHandler(cache, fetcher)(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestTerrainHandlerRejectsBadCoordinates(t *testing.T) {
	cache := newTestTileCache(t)
	c, rec := terrainRequest("a", "1", "2")
	if err := proxyTerrainTileHandler(cache, &fakeTileFetcher{})(c); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}
