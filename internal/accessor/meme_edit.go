package accessor

import (
	"errors"
	"strings"
)

// MemeEdit is a partial change to one meme. Only what it names is touched:
// a tag delta links and unlinks just those tags, and notes or the viewer's
// favorite change only when their pointer is set. A store applies an edit
// against the meme's current state while holding its row lock, so two edits
// to the same meme are serialized and neither overwrites the other.
type MemeEdit struct {
	// TagsAdded and TagsRemoved are a delta against whatever tags the meme
	// has when the edit is applied. A tag named in both ends up added.
	TagsAdded   []string
	TagsRemoved []string
	// ReplaceTags, when non-nil, replaces the whole tag set. It is the legacy
	// full-replacement payload and cannot be combined with a delta.
	ReplaceTags *[]string
	Notes       *string
	Favorite    *bool
	Actor       AuditActor
	// Authorize, when set, is shown the change this edit would make, worked
	// out against the meme's current state with its row locked. Returning an
	// error aborts the edit without writing anything, and the store returns
	// that error unchanged. It runs inside the store's transaction, so it must
	// be quick and must not call back into the store.
	Authorize func(MemeChange) error
}

// MemeChange is what an edit actually changes, as opposed to what it asked
// for: a tag that was already there is not "added", one that was not there is
// not "removed", and notes or a favorite equal to the current value are left
// out.
type MemeChange struct {
	TagsAdded   []string
	TagsRemoved []string
	// Notes is the trimmed new notes, or nil when the notes do not change.
	Notes *string
	// Favorite is the viewer's new favorite state, or nil when it does not
	// change.
	Favorite *bool
}

// Empty reports whether the change would write nothing.
func (c MemeChange) Empty() bool {
	return len(c.TagsAdded) == 0 && len(c.TagsRemoved) == 0 && c.Notes == nil && c.Favorite == nil
}

// ApplyToTags returns current with the change's tags removed and added.
func (c MemeChange) ApplyToTags(current []string) []string {
	removed := make(map[string]struct{}, len(c.TagsRemoved))
	for _, tag := range c.TagsRemoved {
		removed[tag] = struct{}{}
	}
	next := make([]string, 0, len(current)+len(c.TagsAdded))
	for _, tag := range current {
		if _, drop := removed[normalizeTag(tag)]; !drop {
			next = append(next, tag)
		}
	}
	return normalizeTags(append(next, c.TagsAdded...))
}

var errMixedTagEdit = errors.New("a meme edit cannot both replace the tag set and apply a tag delta")

// PlanMemeEdit works out the change edit makes to current. Stores call it with
// the meme's row locked, so the result is what the write will really do.
func PlanMemeEdit(current Meme, edit MemeEdit) (MemeChange, error) {
	if edit.ReplaceTags != nil && (len(edit.TagsAdded) > 0 || len(edit.TagsRemoved) > 0) {
		return MemeChange{}, errMixedTagEdit
	}

	change := MemeChange{}
	currentTags := normalizeTags(current.Tags)
	if edit.ReplaceTags != nil {
		change.TagsAdded, change.TagsRemoved = diffAuditTags(currentTags, *edit.ReplaceTags)
	} else {
		have := make(map[string]struct{}, len(currentTags))
		for _, tag := range currentTags {
			have[tag] = struct{}{}
		}
		added := normalizeTags(edit.TagsAdded)
		adding := make(map[string]struct{}, len(added))
		for _, tag := range added {
			adding[tag] = struct{}{}
			if _, ok := have[tag]; !ok {
				change.TagsAdded = append(change.TagsAdded, tag)
			}
		}
		for _, tag := range normalizeTags(edit.TagsRemoved) {
			_, onMeme := have[tag]
			_, readded := adding[tag]
			if onMeme && !readded {
				change.TagsRemoved = append(change.TagsRemoved, tag)
			}
		}
	}

	if edit.Notes != nil {
		notes := strings.TrimSpace(*edit.Notes)
		if notes != strings.TrimSpace(current.Notes) {
			change.Notes = &notes
		}
	}
	if edit.Favorite != nil && *edit.Favorite != current.Favorite {
		favorite := *edit.Favorite
		change.Favorite = &favorite
	}
	return change, nil
}
