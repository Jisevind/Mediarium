package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rdborg/mediarium/internal/library"
)

func releaseDatesTMDB(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/movie/603", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":603,"title":"The Matrix","release_date":"1999-03-31","release_dates":{"results":[
			{"iso_3166_1":"US","release_dates":[{"type":3,"release_date":"1999-03-31T00:00:00.000Z"},{"type":4,"release_date":"1999-09-21T00:00:00.000Z"}]},
			{"iso_3166_1":"DE","release_dates":[{"type":3,"release_date":"1999-06-17T00:00:00.000Z"},{"type":3,"release_date":"1999-06-10T00:00:00.000Z"}]}
		]}}`))
	})
	mux.HandleFunc("/genre/movie/list", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"genres":[]}`)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// The movie page gets the cinema and digital date for the country of the info
// language, and the US dates where that country lists none.
func TestMovieDetailCarriesReleaseDates(t *testing.T) {
	server, base, admin := loginNewServer(t)
	server.TestSetTMDBBaseURL("k", releaseDatesTMDB(t).URL)

	dates := func() map[string]any {
		return getJSON[map[string]any](t, admin, base+"/api/tmdb/movies/603")["releaseDates"].(map[string]any)
	}
	got := dates()
	if got["cinema"] != "1999-03-31" || got["digital"] != "1999-09-21" || got["region"] != "US" {
		t.Fatalf("English shows the US dates, got %v", got)
	}

	putJSONStatus(t, admin, base+"/api/settings", map[string]any{"infoLanguage": "de-DE"}, http.StatusOK)
	got = dates()
	if got["cinema"] != "1999-06-10" || got["region"] != "DE" || got["cinemaRegion"] != nil {
		t.Fatalf("German shows its own earliest cinema date, got %v", got)
	}
	if got["digital"] != "1999-09-21" || got["digitalRegion"] != "US" {
		t.Fatalf("Germany lists no digital date, so the US one is used and says so, got %v", got)
	}
}

// A monitored movie shows up on the calendar a second time on its digital
// date, and both entries say which they are.
func TestCalendarShowsDigitalReleaseDate(t *testing.T) {
	server, base, client := loginNewServer(t)
	day := func(d int) string { return time.Now().AddDate(0, 0, d).Format("2006-01-02") }
	for _, m := range []library.Movie{
		{TMDBID: 1, Title: "Both Dates", ReleaseDate: day(-20), Monitored: true},
		{TMDBID: 2, Title: "Same Day", ReleaseDate: day(5), Monitored: true},
		{TMDBID: 3, Title: "Unmonitored", ReleaseDate: day(-20), Monitored: false},
		{TMDBID: 4, Title: "Not Asked Yet", ReleaseDate: day(-10), Monitored: true},
	} {
		if _, err := server.MovieRepo.Add(m); err != nil {
			t.Fatalf("add movie %s: %v", m.Title, err)
		}
	}
	for id, digital := range map[int]string{1: day(10), 2: day(5), 3: day(10)} {
		if err := server.MovieRepo.SaveMovieReleaseDates(id, "US", "", digital, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	type seen struct{ date, release string }
	got := map[string][]seen{}
	for _, e := range getJSON[[]map[string]any](t, client, base+"/api/calendar") {
		release, _ := e["release"].(string)
		got[e["title"].(string)] = append(got[e["title"].(string)], seen{e["releaseDate"].(string), release})
	}
	if want := []seen{{day(-20), "cinema"}, {day(10), "digital"}}; len(got["Both Dates"]) != 2 || got["Both Dates"][0] != want[0] || got["Both Dates"][1] != want[1] {
		t.Fatalf("a movie with two dates shows twice, labelled: %v", got["Both Dates"])
	}
	if len(got["Same Day"]) != 1 || got["Same Day"][0].release != "" {
		t.Fatalf("two dates on one day show once, unlabelled: %v", got["Same Day"])
	}
	if len(got["Unmonitored"]) != 0 {
		t.Fatalf("an unmonitored movie is not on the calendar: %v", got["Unmonitored"])
	}
	if len(got["Not Asked Yet"]) != 1 || got["Not Asked Yet"][0].release != "" {
		t.Fatalf("a movie without a saved digital date keeps its one entry: %v", got["Not Asked Yet"])
	}
}
