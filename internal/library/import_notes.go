package library

import "fmt"

// importNote is the note of a show's import item: what was placed by
// TheTVDB's season numbers, so the person knows why those episodes sit where
// they do, then what was left out. Each is a sentence of its own on its own
// line, so the screen can show both in the reader's language.
func importNote(bySeason int, leftOut string) string {
	var placed string
	switch {
	case bySeason == 1:
		placed = "Placed 1 episode by the TheTVDB season numbers."
	case bySeason > 1:
		placed = fmt.Sprintf("Placed %d episodes by the TheTVDB season numbers.", bySeason)
	}
	switch {
	case placed == "":
		return leftOut
	case leftOut == "":
		return placed
	}
	return placed + "\n" + leftOut
}

// leftOutNote is the import report's note for a show whose files named
// episodes the movie database doesn't list: how many, and what to do about
// it when the names say. byAbsolute is true when some of those files were
// named by their number counted from the start of the show, byNumber when
// some had a season and episode. Each case is one whole sentence, so the
// screen can show it in the reader's language.
func leftOutNote(unknown int, byAbsolute, byNumber, anime bool) string {
	if unknown <= 0 {
		return ""
	}
	setAnime := byAbsolute && !anime
	rename := !setAnime && (byAbsolute || (byNumber && anime))
	switch {
	case unknown == 1 && setAnime:
		return "Left out 1 episode that the movie database doesn't list for this show. Its name carries the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again."
	case setAnime:
		return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show. Their names carry the episode number counted from the start of the show, so set this show's Episode numbering to Anime and import the folder again.", unknown)
	case unknown == 1 && rename:
		return "Left out 1 episode that the movie database doesn't list for this show. Its number doesn't match the movie database's list, so nothing was guessed. Rename the file to the season and episode shown on the show's page and import the folder again."
	case rename:
		return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show. Their numbers don't match the movie database's list, so nothing was guessed. Rename the files to the season and episode shown on the show's page and import the folder again.", unknown)
	case unknown == 1:
		return "Left out 1 episode that the movie database doesn't list for this show."
	}
	return fmt.Sprintf("Left out %d episodes that the movie database doesn't list for this show.", unknown)
}
