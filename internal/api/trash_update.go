package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rdborg/mediarium/internal/quality"
	"github.com/rdborg/mediarium/internal/settings"
)

// Keeping the custom formats that came from the TRaSH Guides up to date: their
// conditions follow the guides, and so do the scores profiles give them, as
// long as nobody changed those scores by hand. Formats that were pasted or
// made by hand are never touched. It runs once a day when the person switched
// it on, or on request from the Custom formats card.

const (
	trashUpdateEvery = 24 * time.Hour
	trashUpdateRetry = 6 * time.Hour
)

func (s *Server) trashAutoUpdate() bool {
	return s.settingOn(settings.KeyTrashFormatsAuto, false)
}

// trashFormatChange is one format the guides changed.
type trashFormatChange struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Conditions is true when the conditions (patterns, sizes...) differ.
	Conditions bool `json:"conditions"`
	// ScoreFrom and ScoreTo are TRaSH's score before and now; they differ only
	// when ScoreChanged.
	ScoreChanged bool `json:"scoreChanged"`
	ScoreFrom    int  `json:"scoreFrom"`
	ScoreTo      int  `json:"scoreTo"`
	// Profiles names the quality profiles whose score follows the change: the
	// ones that still have the old TRaSH score.
	Profiles []string `json:"profiles"`

	app    quality.FormatApp
	specs  []quality.FormatSpec
	scores map[int64]int // profile id -> new score
}

type trashUpdateResult struct {
	Changes []trashFormatChange `json:"changes"`
	// Checked is how many formats from the guides were compared.
	Checked int `json:"checked"`

	// quiet are formats whose conditions are the same and whose remembered
	// TRaSH score moved from "not known" (0): nothing to tell, but the new
	// score is kept for next time.
	quiet []trashFormatChange
}

// trashChanges compares the formats that came from the guides with the guides
// as they are now. It changes nothing.
func (s *Server) trashChanges(ctx context.Context) (trashUpdateResult, error) {
	res := trashUpdateResult{Changes: []trashFormatChange{}}
	formats, err := s.FormatRepo.List()
	if err != nil {
		return res, fmt.Errorf("list custom formats: %w", err)
	}
	profiles, err := s.QualityRepo.List()
	if err != nil {
		return res, fmt.Errorf("list quality profiles: %w", err)
	}
	live := map[quality.FormatApp]map[string]quality.ImportedFormat{}
	for _, f := range formats {
		if f.TrashID == "" || f.TrashScore == nil {
			continue
		}
		if _, ok := live[f.App]; !ok {
			all, err := s.trashFormats(ctx, f.App)
			if err != nil {
				return res, err
			}
			live[f.App] = all
		}
		now, ok := live[f.App][f.TrashID]
		if !ok {
			continue // the guides dropped it: it stays as it is
		}
		res.Checked++
		ch := trashFormatChange{ID: f.ID, Name: f.Name, app: now.Format.App, specs: now.Format.Specs, scores: map[int64]int{}, Profiles: []string{}}
		ch.Conditions = !specsEqual(f.Specs, now.Format.Specs) || f.App != now.Format.App
		ch.ScoreFrom, ch.ScoreTo = *f.TrashScore, now.DefaultScore
		ch.ScoreChanged = ch.ScoreFrom != ch.ScoreTo
		if ch.ScoreChanged {
			for _, p := range profiles {
				if sc, changed := quality.RetargetScore(p.FormatScores, f.ID, ch.ScoreFrom, ch.ScoreTo); changed {
					ch.scores[p.ID] = sc
					ch.Profiles = append(ch.Profiles, p.Name)
				}
			}
		}
		switch {
		case ch.Conditions || (ch.ScoreChanged && ch.ScoreFrom != 0):
			res.Changes = append(res.Changes, ch)
		case ch.ScoreChanged:
			res.quiet = append(res.quiet, ch)
		}
	}
	return res, nil
}

// applyTrashChanges re-reads the guides and writes what changed.
func (s *Server) applyTrashChanges(ctx context.Context) (trashUpdateResult, error) {
	res, err := s.trashChanges(ctx)
	if err != nil {
		return res, err
	}
	for _, ch := range append(append([]trashFormatChange{}, res.Changes...), res.quiet...) {
		if err := s.FormatRepo.UpdateFromTrash(ch.ID, ch.app, ch.specs, ch.ScoreTo); err != nil {
			return res, fmt.Errorf("update custom format %q: %w", ch.Name, err)
		}
	}
	// One write per profile, whatever the number of formats.
	byProfile := map[int64]map[int64]int{}
	for _, ch := range res.Changes {
		for pid, sc := range ch.scores {
			if byProfile[pid] == nil {
				byProfile[pid] = map[int64]int{}
			}
			byProfile[pid][ch.ID] = sc
		}
	}
	for pid, scores := range byProfile {
		p, err := s.QualityRepo.Get(pid)
		if err != nil {
			continue // deleted while this ran
		}
		for fid, sc := range scores {
			if sc == 0 {
				delete(p.FormatScores, fid)
			} else {
				p.FormatScores[fid] = sc
			}
		}
		if _, err := s.QualityRepo.Update(p); err != nil {
			return res, fmt.Errorf("update quality profile %q: %w", p.Name, err)
		}
	}
	if n := len(res.Changes); n > 0 {
		_ = s.QueueRepo.LogActivity(0, "update", fmt.Sprintf("Updated %s from the TRaSH Guides", plural(n, "custom format")))
	}
	return res, nil
}

func writeTrashUpdateError(w http.ResponseWriter, err error) {
	writeError(w, http.StatusBadGateway, sentence(err.Error()))
}

// handleTrashUpdatePreview lists what an update from the guides would change.
func (s *Server) handleTrashUpdatePreview(w http.ResponseWriter, r *http.Request) {
	res, err := s.trashChanges(r.Context())
	if err != nil {
		writeTrashUpdateError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleTrashUpdate updates the formats that came from the guides.
func (s *Server) handleTrashUpdate(w http.ResponseWriter, r *http.Request) {
	res, err := s.applyTrashChanges(r.Context())
	if err != nil {
		writeTrashUpdateError(w, err)
		return
	}
	_ = s.Settings.Set(settings.KeyTrashFormatsLast, time.Now().UTC().Format(time.RFC3339), false)
	writeJSON(w, http.StatusOK, res)
}

// trashFormatsJob reads the guides once a day when the person asked for it.
// The job wakes up every hour; the stored time of the last read keeps it to
// one read a day, and a failed read is tried again after a few hours.
func (s *Server) trashFormatsJob(ctx context.Context) {
	if !s.trashAutoUpdate() {
		return
	}
	now := time.Now()
	if v, _ := s.Settings.Get(settings.KeyTrashFormatsLast); v != "" {
		if last, err := time.Parse(time.RFC3339, v); err == nil && now.Sub(last) < trashUpdateEvery {
			return
		}
	}
	stamp := now
	if _, err := s.applyTrashChanges(ctx); err != nil {
		slog.Warn("custom formats: couldn't update from the TRaSH Guides", "err", err)
		stamp = now.Add(-(trashUpdateEvery - trashUpdateRetry))
	}
	_ = s.Settings.Set(settings.KeyTrashFormatsLast, stamp.UTC().Format(time.RFC3339), false)
}
