package api

import (
	"github.com/rdborg/mediarium/internal/indexers"
	"github.com/rdborg/mediarium/internal/quality"
)

// markBestMatch flags the one release in an interactive search that automatic
// search would take: of the releases nothing speaks against (no rejection, not
// blocklisted), the one with the best pickKey, which is the language, then the
// quality, then the preferred-term and custom-format score, with the same
// tie-breaks. The search list shows it first with a "Best match" label.
// payloads and found run in step; nothing is marked when no release qualifies.
func markBestMatch(payloads []searchResultPayload, found []indexers.Result, profile quality.Profile) {
	best := -1
	var bestKey pickKey
	for i := range payloads {
		if len(payloads[i].Rejections) > 0 || payloads[i].Blocklisted || payloads[i].BelowMinSeeders {
			continue
		}
		if !profile.Accepts(found[i].Parsed()) {
			continue
		}
		key := keyFor(profile, found[i].Parsed(), &found[i])
		if best < 0 || key.above(bestKey) || (key == bestKey && preferOver(&found[best], &found[i])) {
			best, bestKey = i, key
		}
	}
	if best >= 0 {
		payloads[best].Best = true
	}
}
