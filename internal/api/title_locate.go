package api

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rdborg/mediarium/internal/libimport"
	"github.com/rdborg/mediarium/internal/library"
	"github.com/rdborg/mediarium/internal/mediaservers"
	"github.com/rdborg/mediarium/internal/organizer"
	"github.com/rdborg/mediarium/internal/quality"
)

// Pointing a title at files that were renamed or moved by hand. Nothing here
// moves, renames or deletes a file: the library only learns the new place.
// Every path a person sends is checked against the configured folders, with
// symlinks resolved, before anything is read or saved.

// browseRoots are the folders the file picker may show: the library folders
// and the downloads folder.
func (s *Server) browseRoots() []string {
	roots := s.videoRoots()
	if d := s.downloadsRoot(); d != "" {
		roots = append(roots, d)
	}
	return roots
}

// pathInside returns the cleaned path when it is a full path inside one of
// roots and still inside after symlinks are resolved. Both the path itself
// and the folder it is in have to be there; a link that leaves the roots
// makes the path refused. The path must exist.
func pathInside(roots []string, p string) (string, bool) {
	if p == "" || !filepath.IsAbs(p) {
		return "", false
	}
	clean := filepath.Clean(p)
	root := rootContaining(roots, clean)
	if root == "" {
		return "", false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	real, err := filepath.EvalSymlinks(clean)
	if err != nil || rootContaining([]string{realRoot}, real) == "" {
		return "", false
	}
	return clean, true
}

// locateProblem is a refusal with the status to send and a sentence for the person.
type locateProblem struct {
	status int
	msg    string
}

// checkLocateFile says whether path can become the file of the movie (movieID,
// 0 when a file is only being checked): inside a library folder after symlinks
// are resolved, a regular video file, and not already the file of another
// movie or an episode.
func (s *Server) checkLocateFile(movieID int64, path string) (string, *locateProblem) {
	clean, ok := pathInside(s.videoRoots(), path)
	if !ok {
		return "", &locateProblem{http.StatusBadRequest, "That file isn't inside one of your library folders, so Mediarium won't use it."}
	}
	info, err := os.Stat(clean) // follows the link, which pathInside already kept inside
	if err != nil || !info.Mode().IsRegular() {
		return "", &locateProblem{http.StatusBadRequest, "That isn't a file Mediarium can use. Pick a video file."}
	}
	if !organizer.IsVideoFile(clean) || strings.HasPrefix(filepath.Base(clean), ".") {
		return "", &locateProblem{http.StatusBadRequest, "That doesn't look like a video file."}
	}
	all, err := s.MovieRepo.ListWithCopies()
	if err != nil {
		return "", &locateProblem{http.StatusInternalServerError, "Couldn't check your library. Check the Mediarium log for details."}
	}
	for _, o := range all {
		if o.ID != movieID && o.FilePath != "" && filepath.Clean(o.FilePath) == clean {
			return "", &locateProblem{http.StatusConflict, fmt.Sprintf("That file already belongs to %s.", movieLabel(o))}
		}
	}
	if eps, err := s.MovieRepo.EpisodesByFiles([]string{clean}); err == nil && len(eps) > 0 {
		return "", &locateProblem{http.StatusConflict, "That file already belongs to an episode of a show."}
	}
	return clean, nil
}

// locateRequest is the body of the locate routes: one full path.
type locateRequest struct {
	Path string `json:"path"`
}

func (s *Server) locateMovie(w http.ResponseWriter, r *http.Request) (library.Movie, locateRequest, bool) {
	var req locateRequest
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid movie ID.")
		return library.Movie{}, req, false
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return library.Movie{}, req, false
	}
	m, err := s.MovieRepo.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That movie isn't in your library.")
		return library.Movie{}, req, false
	}
	if err := s.checkMovieNotDownloading(m.ID); err != nil {
		writeError(w, http.StatusConflict, "This movie is being downloaded right now. Wait for the download to finish, or cancel it, and try again.")
		return library.Movie{}, req, false
	}
	return m, req, true
}

// handleLocateMovieFile sets the file a person picked as the movie's file.
func (s *Server) handleLocateMovieFile(w http.ResponseWriter, r *http.Request) {
	m, req, ok := s.locateMovie(w, r)
	if !ok {
		return
	}
	file, problem := s.checkLocateFile(m.ID, req.Path)
	if problem != nil {
		writeError(w, problem.status, problem.msg)
		return
	}
	tier := libimport.TierFromPath(file)
	tierName := string(tier)
	if tier == quality.TierUnknown {
		tierName = ""
	}
	if err := s.MovieRepo.SetStatus(m.ID, library.StatusDownloaded, tierName, file); err != nil {
		slog.Warn("locate: couldn't save a movie's file", "movie", m.Title, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't save the file to the library. Check the Mediarium log for details.")
		return
	}
	slog.Info("locate: a movie was pointed at a file", "movie", m.Title, "file", filepath.Base(file))
	_ = s.QueueRepo.LogActivity(m.ID, "imported", fmt.Sprintf("%s: the file was set to %s by hand.", movieLabel(m), filepath.Base(file)))
	s.mediaServersImported(mediaservers.MediaMovie, file)
	writeJSON(w, http.StatusOK, rescanReport{Added: filepath.Base(file)})
}

// handleLocateMovieFolder looks in a folder a person picked and gives the
// movie the biggest video there.
func (s *Server) handleLocateMovieFolder(w http.ResponseWriter, r *http.Request) {
	m, req, ok := s.locateMovie(w, r)
	if !ok {
		return
	}
	dir, problem := s.checkLocateFolder(req.Path)
	if problem != nil {
		writeError(w, problem.status, problem.msg)
		return
	}
	rep, err := s.attachBiggestVideo(m, dir, rescanReport{})
	if err != nil {
		slog.Warn("locate: couldn't add a movie's file", "movie", m.Title, "err", err)
		writeError(w, http.StatusInternalServerError, "Found a video file but couldn't save it to the library. Check the Mediarium log for details.")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// checkLocateFolder says whether path is a folder a title can be pointed at:
// inside a library folder (links resolved), a real folder, and not a whole
// library folder, which holds many titles.
func (s *Server) checkLocateFolder(path string) (string, *locateProblem) {
	roots := s.videoRoots()
	clean, ok := pathInside(roots, path)
	if !ok {
		return "", &locateProblem{http.StatusBadRequest, "That folder isn't inside one of your library folders, so Mediarium won't use it."}
	}
	if info, err := os.Stat(clean); err != nil || !info.IsDir() {
		return "", &locateProblem{http.StatusBadRequest, "That isn't a folder Mediarium can read."}
	}
	for _, r := range roots {
		if r != "" && filepath.Clean(r) == clean {
			return "", &locateProblem{http.StatusBadRequest, "That is a whole library folder. Pick the folder of this title inside it."}
		}
	}
	return clean, nil
}

// seriesFolderReport says what changing a show's folder found.
type seriesFolderReport struct {
	Folder    string `json:"folder"`
	Found     int    `json:"found"`     // episodes with their file in the new folder now
	Total     int    `json:"total"`     // episodes the show has that have aired, or have a file
	Missing   int    `json:"missing"`   // of those, the ones still without a file
	NotPlaced int    `json:"notPlaced"` // video files in the folder that matched no episode
	Message   string `json:"message"`
}

// handleChangeSeriesFolder reads the episode files in the folder a person
// picked and attaches them to the show's episodes. The show has no folder of
// its own in the database (it is where its episodes' files are), so the new
// folder shows up as soon as the episodes point into it.
func (s *Server) handleChangeSeriesFolder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "That isn't a valid show ID.")
		return
	}
	var req locateRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that request. Reload the page and try again.")
		return
	}
	sr, err := s.MovieRepo.GetSeries(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "That show isn't in your library.")
		return
	}
	dir, problem := s.checkLocateFolder(req.Path)
	if problem != nil {
		writeError(w, problem.status, problem.msg)
		return
	}
	rep, err := s.rereadSeriesFolder(sr, dir, time.Now())
	if err != nil {
		slog.Warn("locate: couldn't read a show's folder", "series", sr.Title, "folder", dir, "err", err)
		writeError(w, http.StatusInternalServerError, "Couldn't read that folder. Check the Mediarium log for details.")
		return
	}
	writeJSON(w, http.StatusOK, rep)
}

// rereadSeriesFolder reads the episode files in dir the way a library import
// reads a show folder (names with season and episode, and for an anime the
// number counted from the first episode) and gives them to the show's
// episodes. An episode whose old file is gone, or that had none, takes the
// file; an episode whose old file is still on the disk is left alone, and so
// is every episode the folder has no file for.
func (s *Server) rereadSeriesFolder(sr library.Series, dir string, today time.Time) (seriesFolderReport, error) {
	rep := seriesFolderReport{Folder: dir}
	files, skipped, err := libimport.ScanSeriesFolder(dir)
	if err != nil {
		return rep, fmt.Errorf("read %s: %w", dir, err)
	}
	eps, err := s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return rep, fmt.Errorf("list episodes: %w", err)
	}
	byNumber := map[[2]int]library.Episode{}
	for _, e := range eps {
		byNumber[[2]int{e.Season, e.Episode}] = e
	}
	place := library.ImportPlacer(eps, sr.SeriesType == library.SeriesAnime, nil)
	var attached []string
	rep.NotPlaced = len(skipped)
	for _, f := range files {
		season, numbers, placed := place(library.ImportFile{Path: f.Path, Quality: f.Quality, Season: f.Season, Episodes: f.Episodes, Absolute: f.Absolute})
		if !placed {
			rep.NotPlaced++
			continue
		}
		tier := f.Quality
		if tier == string(quality.TierUnknown) {
			tier = ""
		}
		hit := false
		for _, n := range numbers {
			ep, ok := byNumber[[2]int{season, n}]
			if !ok {
				continue
			}
			hit = true
			if ep.FilePath == f.Path && ep.Status == library.StatusDownloaded {
				continue
			}
			if ep.Status == library.StatusDownloaded && ep.FilePath != "" && rootContaining([]string{dir}, ep.FilePath) == "" {
				if _, err := os.Stat(ep.FilePath); err == nil {
					continue // its own file is still where it was
				}
			}
			if err := s.MovieRepo.SetEpisodeStatus(ep.ID, library.StatusDownloaded, tier, f.Path); err != nil {
				return rep, fmt.Errorf("save the file of season %d episode %d: %w", season, n, err)
			}
			attached = append(attached, f.Path)
		}
		if !hit {
			rep.NotPlaced++
		}
	}

	eps, err = s.MovieRepo.ListEpisodes(sr.ID)
	if err != nil {
		return rep, fmt.Errorf("list episodes: %w", err)
	}
	now := today.Format("2006-01-02")
	for _, e := range eps {
		downloaded := e.Status == library.StatusDownloaded
		aired := e.Season > 0 && e.AirDate != "" && e.AirDate <= now
		if !downloaded && !aired {
			continue
		}
		rep.Total++
		if downloaded && e.FilePath != "" && rootContaining([]string{dir}, e.FilePath) != "" {
			rep.Found++
		}
		if !downloaded {
			rep.Missing++
		}
	}
	rep.Message = seriesFolderMessage(rep)
	if len(attached) > 0 {
		slog.Info("locate: a show got episode files from a folder", "series", sr.Title, "folder", dir, "files", len(attached))
		_ = s.QueueRepo.LogSeriesActivity(sr.ID, "imported", fmt.Sprintf("%s: read the folder %s and found %d of %d episodes.", seriesLabel(sr), dir, rep.Found, rep.Total))
		s.mediaServersImported(mediaservers.MediaTV, attached...)
	}
	return rep, nil
}

// seriesFolderMessage says what changing a show's folder found, in plain words.
func seriesFolderMessage(rep seriesFolderReport) string {
	var msg string
	switch {
	case rep.Found == 0:
		msg = fmt.Sprintf("Found no episode files for this show in %s. Nothing was changed.", rep.Folder)
	case rep.Missing == 0:
		msg = fmt.Sprintf("Found %d of %d episodes in %s.", rep.Found, rep.Total, rep.Folder)
	case rep.Missing == 1:
		msg = fmt.Sprintf("Found %d of %d episodes in %s. 1 is still missing.", rep.Found, rep.Total, rep.Folder)
	default:
		msg = fmt.Sprintf("Found %d of %d episodes in %s. %d are still missing.", rep.Found, rep.Total, rep.Folder, rep.Missing)
	}
	switch {
	case rep.NotPlaced == 1:
		msg += " 1 video file there wasn't placed on an episode."
	case rep.NotPlaced > 1:
		msg += fmt.Sprintf(" %d video files there weren't placed on an episode.", rep.NotPlaced)
	}
	return msg
}

// browseEntry is one folder or video file in the file picker.
type browseEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size,omitempty"`
}

// browseAnswer is one folder of the file picker. Path is "" at the top, where
// the entries are the library folders and the downloads folder. Parent is the
// folder above ("" when that is the top).
type browseAnswer struct {
	Path    string        `json:"path"`
	Parent  string        `json:"parent"`
	Entries []browseEntry `json:"entries"`
}

// handleBrowse lists the folders and video files inside the library folders
// and the downloads folder, for choosing a file or folder. Nothing outside
// them is ever listed: a link that leaves them is left out.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	roots := s.browseRoots()
	answer := browseAnswer{Entries: []browseEntry{}}
	want := r.URL.Query().Get("path")
	if want == "" {
		seen := map[string]bool{}
		for _, root := range roots {
			if root == "" {
				continue
			}
			clean, ok := pathInside([]string{root}, root)
			if !ok || seen[clean] {
				continue
			}
			seen[clean] = true
			answer.Entries = append(answer.Entries, browseEntry{Name: clean, Path: clean, Dir: true})
		}
		writeJSON(w, http.StatusOK, answer)
		return
	}
	clean, ok := pathInside(roots, want)
	if !ok {
		writeError(w, http.StatusBadRequest, "That folder isn't inside one of your library folders.")
		return
	}
	entries, err := os.ReadDir(clean)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Couldn't read that folder.")
		return
	}
	answer.Path = clean
	if rootContaining(roots, filepath.Dir(clean)) != "" && !isRoot(roots, clean) {
		answer.Parent = filepath.Dir(clean)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "@") {
			continue
		}
		full := filepath.Join(clean, name)
		if _, ok := pathInside(roots, full); !ok {
			continue // a link that leaves the folders
		}
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		switch {
		case info.IsDir():
			answer.Entries = append(answer.Entries, browseEntry{Name: name, Path: full, Dir: true})
		case info.Mode().IsRegular() && organizer.IsVideoFile(name):
			answer.Entries = append(answer.Entries, browseEntry{Name: name, Path: full, Size: info.Size()})
		}
	}
	sort.SliceStable(answer.Entries, func(i, j int) bool {
		a, b := answer.Entries[i], answer.Entries[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	writeJSON(w, http.StatusOK, answer)
}

// isRoot says path is one of roots itself.
func isRoot(roots []string, path string) bool {
	for _, r := range roots {
		if r != "" && filepath.Clean(r) == path {
			return true
		}
	}
	return false
}
