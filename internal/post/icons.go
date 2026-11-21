package post

import "bytes"

// MaxIcon is the most bytes that an icon may have.
const MaxIcon = 1 << 20

// Icon is a board's icon for a harness, which its pages show instead of
// their own, as <icon> holds it.
type Icon struct {
	Board   string `json:"board"`
	Harness string `json:"harness"`
	// Type is the media type: one of IconTypes.
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// IconTypes are the media types of icons.
var IconTypes = []string{"image/png", "image/jpeg", "image/gif", "image/webp"}

// IconType returns the media type that an icon's first bytes give: PNG's
// signature, JPEG's FF D8 FF, GIF87a or GIF89a, or RIFF, four bytes, and
// WEBP. ok is false for other content.
func IconType(data []byte) (typ string, ok bool) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg", true
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return "image/gif", true
	case len(data) >= 12 && bytes.HasPrefix(data, []byte("RIFF")) && string(data[8:12]) == "WEBP":
		return "image/webp", true
	}
	return "", false
}

// CheckIcon returns the media type of an icon whose content is data, or an
// error of kind Invalid if data is empty, larger than MaxIcon, or no image
// that IconType knows. name names the file in the error.
func CheckIcon(name string, data []byte) (string, error) {
	switch {
	case len(data) == 0:
		return "", Errorf(Invalid, "%s is empty: an icon is a PNG, JPEG, GIF, or WebP image", name)
	case len(data) > MaxIcon:
		return "", Errorf(Invalid, "%s has %s: an icon may have at most 1 MiB", name, FormatSize(int64(len(data))))
	}
	typ, ok := IconType(data)
	if !ok {
		return "", Errorf(Invalid, "%s isn't a PNG, JPEG, GIF, or WebP image: an icon must be one", name)
	}
	return typ, nil
}

// CheckHarness returns an error of kind Invalid unless s is a harness's
// name.
func CheckHarness(s string) error {
	if !ValidHarness(s) {
		return Errorf(Invalid, "invalid harness %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter, such as codex", s)
	}
	return nil
}

// MaySetIcons reports, as an error of kind Forbidden, whether the identity
// may set and remove a board's icons: only people may.
func (id Identity) MaySetIcons() error {
	if !id.IsPerson() {
		return Errorf(Forbidden, "only a person (%s/...) can set or remove a board's icons", Person)
	}
	return nil
}

// NoIcon is the error for a board without an icon for a harness.
func NoIcon(board, harness string) error {
	return Errorf(NotFound, "board %s has no icon for %s", board, harness)
}
