package processor

import (
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UserInfo is the subset of iam User fields needed to enrich activities with
// human-readable display names and avatars.
type UserInfo struct {
	// Name is the User's metadata.name. For Milo users this is the identity
	// provider subject, which is also what the authenticator puts in the
	// request's user.UID (and so in audit events' user.uid).
	Name string

	// UID is the User's metadata.uid.
	UID string

	// Email is the User's spec.email. It is used only to apply the staff
	// email-domain rule and is never copied into activities.
	Email string

	// GivenName is the User's spec.givenName.
	GivenName string

	// FamilyName is the User's spec.familyName.
	FamilyName string

	// AvatarURL is the User's status.avatarUrl.
	AvatarURL string
}

// DisplayName returns a sanitized human-readable name built from GivenName
// and FamilyName, or whichever of the two is set. It never falls back to the
// email: when both are empty it returns "" and UIs fall back to the actor's
// name. See SanitizeDisplayName for the rules applied.
func (u UserInfo) DisplayName() string {
	return SanitizeDisplayName(u.GivenName + " " + u.FamilyName)
}

// SafeAvatarURL returns AvatarURL if it is an absolute https URL of bounded
// length, and "" otherwise. See SanitizeAvatarURL.
func (u UserInfo) SafeAvatarURL() string {
	return SanitizeAvatarURL(u.AvatarURL)
}

const (
	// maxDisplayNameRunes caps display names so a self-asserted name cannot
	// dominate a rendered summary.
	maxDisplayNameRunes = 64
	// maxAvatarURLLength caps avatar URLs stored on activities.
	maxAvatarURLLength = 2048
)

// SanitizeDisplayName makes a self-asserted name safe to render. It drops
// control characters, Unicode format characters (category Cf: bidi controls,
// zero-width characters, soft hyphen, BOM, tag characters), variation
// selectors and characters that render as blanks (Hangul fillers, braille
// blank). It then collapses runs of whitespace into a single space, trims
// the result and caps it at 64 runes. If nothing visible remains (only
// spaces or combining marks), it returns "".
func SanitizeDisplayName(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == utf8.RuneError, isInvisible(r):
			continue
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if utf8.RuneCountInString(out) > maxDisplayNameRunes {
		out = strings.TrimSpace(string([]rune(out)[:maxDisplayNameRunes]))
	}
	if !hasVisibleRune(out) {
		return ""
	}
	return out
}

// isInvisible reports whether r renders as nothing (or as a blank that is
// not whitespace) and so could hide or fake text in a display name.
func isInvisible(r rune) bool {
	switch {
	case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		// Controls, and format characters: bidi controls (U+202A-U+202E,
		// U+2066-U+2069, LRM, RLM, ALM), ZWSP, ZWNJ, ZWJ, U+2060, U+FEFF,
		// soft hyphen U+00AD, tag characters U+E0001, U+E0020-U+E007F.
		return true
	case r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF, r >= 0x180B && r <= 0x180F:
		// Variation selectors, including the Mongolian free variation
		// selectors.
		return true
	case r == 0x115F, r == 0x1160, r == 0x3164, r == 0xFFA0, r == 0x2800:
		// Hangul fillers and the braille pattern blank.
		return true
	}
	return false
}

// hasVisibleRune reports whether s contains a rune other than whitespace and
// combining marks.
func hasVisibleRune(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) && !unicode.Is(unicode.M, r) {
			return true
		}
	}
	return false
}

// SanitizeAvatarURL returns s if it is an absolute https URL with a host, no
// whitespace or control characters, and at most 2048 bytes; otherwise "".
func SanitizeAvatarURL(s string) string {
	if s == "" || len(s) > maxAvatarURLLength {
		return ""
	}
	for _, r := range s {
		if unicode.IsSpace(r) || isInvisible(r) {
			return ""
		}
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return ""
	}
	return s
}

// UserResolver looks up iam User records to enrich activities with display
// names and avatars.
//
// Lookups happen on the hot path for every activity, so implementations MUST
// be safe for concurrent use and SHOULD be in-memory reads (no API calls). A
// resolver that is not ready (for example, an informer that has not synced)
// returns ok=false, and the activity is emitted without enrichment.
type UserResolver interface {
	// LookupByName resolves a user by the User's metadata.name. Audit events
	// for Milo users carry this value in user.uid, and User-typed link
	// targets carry it in the link's resource name.
	LookupByName(name string) (UserInfo, bool)

	// LookupByUID resolves a user by the User's metadata.uid.
	LookupByUID(uid string) (UserInfo, bool)
}

// NoopUserResolver is a UserResolver that always misses. Useful in tests and
// anywhere enrichment is deliberately disabled.
type NoopUserResolver struct{}

// LookupByName implements UserResolver.
func (NoopUserResolver) LookupByName(string) (UserInfo, bool) { return UserInfo{}, false }

// LookupByUID implements UserResolver.
func (NoopUserResolver) LookupByUID(string) (UserInfo, bool) { return UserInfo{}, false }

// lookupActor resolves the User behind an audit actor by the audit user.UID
// only: first as metadata.name (what Milo's authenticator sets), then as
// metadata.uid. There is deliberately no email fallback: a username that
// merely equals some User's email (for example from another authenticator)
// must not borrow that User's identity.
func lookupActor(resolver UserResolver, uid string) (UserInfo, bool) {
	if resolver == nil || uid == "" {
		return UserInfo{}, false
	}
	if info, ok := resolver.LookupByName(uid); ok {
		return info, true
	}
	return resolver.LookupByUID(uid)
}
