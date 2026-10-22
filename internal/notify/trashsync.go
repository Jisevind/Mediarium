package notify

import (
	"fmt"
	"strings"
	"time"
)

// trashSyncNamesShown is how many changed formats the message names before it
// says "and N more".
const trashSyncNamesShown = 5

// ComposeTrashSync is the message for a check of the TRaSH Guides that changed
// something: formats is the names of the formats that changed (the first few
// are enough), updated how many changed in all, scores how many profile scores
// moved with them, in profiles different profiles. automatic is true for the
// daily or weekly job and false for the button. The second result is false
// when nothing changed: that is not worth a message.
func ComposeTrashSync(formats []string, updated, scores, profiles int, automatic bool, links Links, at time.Time) (Event, bool) {
	if updated <= 0 {
		return Event{}, false
	}
	ev := Event{Type: EventTrashSync, Timestamp: at, Title: "TRaSH Guides sync applied", LinkURL: links.to("/settings/quality")}
	ev.Lead = fmt.Sprintf("%s updated from the TRaSH Guides.", capital(count(updated, "custom format", "custom formats")))
	var rows []Detail
	if names := namesWithMore(formats, updated); names != "" {
		rows = append(rows, Detail{"Custom formats", names})
	}
	if scores > 0 {
		rows = append(rows, Detail{"Profile scores", fmt.Sprintf("%s changed in %s", count(scores, "score", "scores"), count(profiles, "profile", "profiles"))})
	}
	if automatic {
		rows = append(rows, Detail{"Started by", "The automatic update"})
	} else {
		rows = append(rows, Detail{"Started by", "Update from TRaSH"})
	}
	ev.Details = rows
	ev.Formats = formats
	ev.Message = plainBody(ev)
	ev.SettingsURL = links.to("/settings/notifications")
	return ev, true
}

// namesWithMore lists up to trashSyncNamesShown names, then "and N more" for
// the rest of total.
func namesWithMore(names []string, total int) string {
	var shown []string
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" && len(shown) < trashSyncNamesShown {
			shown = append(shown, n)
		}
	}
	if len(shown) == 0 {
		return ""
	}
	out := strings.Join(shown, ", ")
	if more := total - len(shown); more > 0 {
		out += fmt.Sprintf(" and %d more", more)
	}
	return out
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
