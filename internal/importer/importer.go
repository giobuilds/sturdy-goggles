// Package importer turns a recorded file (GPX or FIT) into a stored source
// file plus a route session with derived metrics, and re-derives metrics for
// every stored file. It is the only code that writes under the data directory.
package importer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/giobuilds/sturdy-goggles/internal/fitfile"
	"github.com/giobuilds/sturdy-goggles/internal/gpx"
	"github.com/giobuilds/sturdy-goggles/internal/store"
	"github.com/giobuilds/sturdy-goggles/internal/track"
)

type Importer struct {
	Store   *store.Store
	DataDir string
	Params  track.Params
}

type Options struct {
	// Mode overrides the source's own guess (FIT sport). Required for GPX,
	// which carries no sport; defaults to "walk".
	Mode       string
	ProtocolID *int64
}

type Result struct {
	SessionID  int64
	Duplicate  bool
	SourceKind string
	Hash       string
	Mode       string
	Metrics    track.Metrics
	Device     *fitfile.DeviceTotals // FIT only
}

// ImportFile is idempotent: a file whose sha256 is already stored returns the
// existing session and changes nothing.
func (im *Importer) ImportFile(path string, opts Options) (Result, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Result{}, err
	}
	kind, err := sourceKind(path)
	if err != nil {
		return Result{}, err
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])

	if existing, err := im.Store.SessionBySourceHash(hash); err == nil {
		m, _ := im.Store.RouteMetricsBySession(existing.ID)
		res := Result{SessionID: existing.ID, Duplicate: true, SourceKind: kind, Hash: hash, Mode: existing.Mode}
		if m != nil {
			res.Metrics = m.Metrics
		}
		return res, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return Result{}, err
	}

	tr, device, err := parse(kind, raw)
	if err != nil {
		return Result{}, err
	}
	mode := opts.Mode
	if mode == "" {
		mode = tr.Mode
	}
	if mode == "" {
		mode = "walk"
	}
	if opts.ProtocolID != nil {
		p, err := im.Store.ProtocolByID(*opts.ProtocolID)
		if err != nil {
			return Result{}, fmt.Errorf("protocol %d: %w", *opts.ProtocolID, err)
		}
		if p.Kind != "route" {
			return Result{}, fmt.Errorf("protocol %q is a %s, not a route", p.Name, p.Kind)
		}
		if p.Mode != "" && p.Mode != mode {
			return Result{}, fmt.Errorf("protocol %q is a %s route, file is a %s", p.Name, p.Mode, mode)
		}
	}
	metrics := track.Derive(tr, im.Params)

	if err := im.storeFile(kind, hash, raw); err != nil {
		return Result{}, err
	}
	sessID, err := im.Store.CreateSession(store.Session{
		Kind:       "route",
		Mode:       mode,
		ProtocolID: opts.ProtocolID,
		StartedAt:  tr.Points[0].Time,
		Completed:  true,
	})
	if err != nil {
		return Result{}, err
	}
	if err := im.Store.InsertRouteMetrics(store.RouteMetrics{
		SessionID:     sessID,
		SourceKind:    kind,
		SourceHash:    hash,
		Metrics:       metrics,
		DeriveVersion: track.DeriveVersion,
	}); err != nil {
		return Result{}, err
	}
	return Result{SessionID: sessID, SourceKind: kind, Hash: hash, Mode: mode, Metrics: metrics, Device: device}, nil
}

// Rederive recomputes every route session's metrics from its stored file.
// Sessions keep their effort and note; only route_metrics rows change.
func (im *Importer) Rederive() (int, error) {
	all, err := im.Store.AllRouteMetrics()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, m := range all {
		raw, err := os.ReadFile(im.SourcePath(m.SourceKind, m.SourceHash))
		if err != nil {
			return n, fmt.Errorf("session %d: %w", m.SessionID, err)
		}
		tr, _, err := parse(m.SourceKind, raw)
		if err != nil {
			return n, fmt.Errorf("session %d: %w", m.SessionID, err)
		}
		if err := im.Store.UpdateRouteMetrics(m.SessionID, track.Derive(tr, im.Params), track.DeriveVersion); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (im *Importer) SourcePath(kind, hash string) string {
	return filepath.Join(im.DataDir, kind, hash+"."+kind)
}

func (im *Importer) storeFile(kind, hash string, raw []byte) error {
	dst := im.SourcePath(kind, hash)
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func parse(kind string, raw []byte) (track.Track, *fitfile.DeviceTotals, error) {
	switch kind {
	case "gpx":
		tr, err := gpx.Parse(bytes.NewReader(raw))
		return tr, nil, err
	case "fit":
		tr, dev, err := fitfile.Parse(bytes.NewReader(raw))
		return tr, &dev, err
	}
	return track.Track{}, nil, fmt.Errorf("unknown source kind %q", kind)
}

func sourceKind(path string) (string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".gpx":
		return "gpx", nil
	case ".fit":
		return "fit", nil
	}
	return "", fmt.Errorf("%s: only .gpx and .fit files are supported", filepath.Base(path))
}
