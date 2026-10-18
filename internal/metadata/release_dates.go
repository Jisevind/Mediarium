package metadata

import (
	"sort"
	"strings"
)

// MovieReleaseDates are the dates a film opens in the cinema and comes out
// digitally, for one country.
type MovieReleaseDates struct {
	// Cinema is the premiere or theatrical date ("YYYY-MM-DD"), Digital the
	// date it can be bought or rented online; empty when TMDB has none.
	Cinema  string `json:"cinema,omitempty"`
	Digital string `json:"digital,omitempty"`
	// Region is the country asked for. CinemaRegion and DigitalRegion name the
	// country a date really came from when it was not that one (the US or the
	// earliest anywhere, since a small country often lists nothing).
	Region        string `json:"region,omitempty"`
	CinemaRegion  string `json:"cinemaRegion,omitempty"`
	DigitalRegion string `json:"digitalRegion,omitempty"`
}

// RegionOfLanguage is the country part of a TMDB language tag ("de-DE" gives
// "DE"), or "US" when the tag has none.
func RegionOfLanguage(tag string) string {
	if _, region, ok := strings.Cut(tag, "-"); ok && len(region) == 2 {
		return strings.ToUpper(region)
	}
	return "US"
}

// TMDB release types: 1 premiere, 2 limited theatrical, 3 theatrical, 4
// digital, 5 physical, 6 TV. Cinema takes the first type a country has.
var (
	cinemaTypes  = []int{3, 2, 1}
	digitalTypes = []int{4}
)

// earliest is the first date of the wanted types (tried in order) that a
// country lists, or "" when it lists none. Dates arrive as
// "2025-03-12T00:00:00.000Z"; only the day is kept.
func (d *MovieDetail) earliest(country string, types []int) string {
	for _, want := range types {
		var dates []string
		for _, c := range d.ReleaseDates.Results {
			if c.Country != country {
				continue
			}
			for _, rd := range c.ReleaseDates {
				if rd.Type == want && len(rd.Date) >= 10 {
					dates = append(dates, rd.Date[:10])
				}
			}
		}
		if len(dates) > 0 {
			sort.Strings(dates)
			return dates[0]
		}
	}
	return ""
}

// earliestAnywhere is earliest over every country.
func (d *MovieDetail) earliestAnywhere(types []int) (date, country string) {
	for _, c := range d.ReleaseDates.Results {
		got := d.earliest(c.Country, types)
		if got != "" && (date == "" || got < date || (got == date && c.Country < country)) {
			date, country = got, c.Country
		}
	}
	return date, country
}

// ReleaseDatesFor gives the cinema and digital dates in a country. A date the
// country does not list falls back to the US, then to the earliest anywhere.
func (d *MovieDetail) ReleaseDatesFor(region string) MovieReleaseDates {
	region = strings.ToUpper(region)
	out := MovieReleaseDates{Region: region}
	pick := func(types []int) (string, string) {
		for _, c := range []string{region, "US"} {
			if got := d.earliest(c, types); got != "" {
				if c == region {
					return got, ""
				}
				return got, c
			}
		}
		return d.earliestAnywhere(types)
	}
	out.Cinema, out.CinemaRegion = pick(cinemaTypes)
	out.Digital, out.DigitalRegion = pick(digitalTypes)
	return out
}
