package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
)

// The two ways a photo's suggested title uses the nearby land features
// (ADR 0282): the nearest one in the direction the camera pointed, without
// Mate, or five of them handed to the vision model, with Mate.

// inboxFeatureCell is a position rounded to 0.02 degrees, about 1.2 nm. The
// features within 10 nm of any point in a cell are all within 11 nm of its
// centre, so one fetch serves every photo taken in it.
type inboxFeatureCell struct{ lat, lon int64 }

type inboxFeatureEntry struct {
	once     sync.Once
	features []poiFeatureRaw
	err      error
}

// inboxFeatureCache fetches the features around a cell once for one title
// request, however many photos were taken in it and however many workers ask.
type inboxFeatureCache struct {
	mu    sync.Mutex
	cells map[inboxFeatureCell]*inboxFeatureEntry
}

func newInboxFeatureCache() *inboxFeatureCache {
	return &inboxFeatureCache{cells: map[inboxFeatureCell]*inboxFeatureEntry{}}
}

func (fc *inboxFeatureCache) get(lat, lon float64) ([]poiFeatureRaw, error) {
	cell := inboxFeatureCell{int64(math.Round(lat * 50)), int64(math.Round(lon * 50))}
	fc.mu.Lock()
	e, ok := fc.cells[cell]
	if !ok {
		e = &inboxFeatureEntry{}
		fc.cells[cell] = e
	}
	fc.mu.Unlock()
	e.once.Do(func() { e.features, e.err = inboxFeaturesNear(float64(cell.lat)/50, float64(cell.lon)/50) })
	return e.features, e.err
}

// nearbyAt is the named features within 10 nm of a fix, nearest first.
func (fc *inboxFeatureCache) nearbyAt(pt *trackFix) ([]inboxFeature, error) {
	raw, err := fc.get(pt.Lat, pt.Lon)
	if err != nil {
		return nil, fmt.Errorf("could not look up nearby features: %w", err)
	}
	return nearbyFeatures(raw, pt.Lat, pt.Lon), nil
}

// headingAt is the boat's heading at a track fix, taken as its course over
// ground while it was moving. At rest a course over ground is noise, so there
// is no heading.
func headingAt(pt *trackFix) *float64 {
	if pt.COGDeg == nil || pt.SOGKts == nil || *pt.SOGKts < passageRestSOGKts {
		return nil
	}
	return pt.COGDeg
}

// cameraTrueOf is the true bearing the phone faced, or nil when it recorded
// none. No magnetic variation is known to the box, so a magnetic bearing is
// read as true (see cameraBearingTrue).
func cameraTrueOf(it inboxItem) *float64 {
	if it.BearingDeg == nil {
		return nil
	}
	deg := cameraBearingTrue(*it.BearingDeg, it.BearingRef, nil)
	return &deg
}

// sectorTitleAt is the title without Mate for a photo that carries a bearing:
// the nearest named feature in the direction it faced, else the nearest place
// to the boat. A feature lookup that fails is an error, not a quiet drop to the
// nearest place.
func sectorTitleAt(fc *inboxFeatureCache, it inboxItem, pt *trackFix) (string, error) {
	near, err := fc.nearbyAt(pt)
	if err != nil {
		return "", err
	}
	if f, ok := nearestInSector(near, *cameraTrueOf(it)); ok {
		return joinTitle(f.Name), nil
	}
	return joinTitle(inboxPlaceLookup(pt.Lat, pt.Lon)), nil
}

// mateTitleAt asks the vision model for a title for one photo with the boat's
// heading and the five nearest features, those in the camera's sector marked.
func mateTitleAt(ctx context.Context, sess inboxMateSession, fc *inboxFeatureCache, it inboxItem, pt *trackFix) (string, error) {
	near, err := fc.nearbyAt(pt)
	if err != nil {
		return "", err
	}
	camera := cameraTrueOf(it)
	if camera != nil {
		near = markSector(near, *camera)
	}
	if len(near) > inboxFeatureCount {
		near = near[:inboxFeatureCount]
	}
	title, err := mateTitle(ctx, sess, it, headingAt(pt), camera, near)
	if err != nil {
		return "", fmt.Errorf("Mate could not suggest a title: %w", err)
	}
	return strings.TrimSpace(title), nil
}
