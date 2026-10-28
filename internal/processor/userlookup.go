package processor

import (
	"context"
	"sync"
	"time"
)

// UserInfo is the subset of iam User fields needed to enrich activities with
// human-readable display names. Resolvers MUST populate at least one of
// GivenName/FamilyName/Email when returning ok=true; a fully-empty result
// should return ok=false so callers can treat it as a miss.
type UserInfo struct {
	// Name is the User CR's metadata.name. Used to construct a
	// resource reference in the activity Link.
	Name       string
	GivenName  string
	FamilyName string
	Email      string
	UID        string
}

// DisplayName returns a human-readable name from given/family, falling back
// to whichever component is populated, then to the email. Returns "" when
// nothing is available.
func (u UserInfo) DisplayName() string {
	switch {
	case u.GivenName != "" && u.FamilyName != "":
		return u.GivenName + " " + u.FamilyName
	case u.GivenName != "":
		return u.GivenName
	case u.FamilyName != "":
		return u.FamilyName
	default:
		return u.Email
	}
}

// UserResolver looks up User records to enrich activities with display names.
//
// Implementations MUST be safe for concurrent use. When the resolver cannot
// find a matching user (or hits a transient error), it SHOULD return
// (UserInfo{}, false, nil). A non-nil error indicates a non-cacheable failure
// the caller should log; the activity is still emitted without enrichment.
type UserResolver interface {
	// LookupByEmail resolves a user by their email address (typically the
	// audit username for OIDC-authenticated requests).
	LookupByEmail(ctx context.Context, email string) (UserInfo, bool, error)

	// LookupByName resolves a user by the User CR's metadata.name. Used to
	// hydrate user-typed link targets, where audit.objectRef.name is the User
	// resource name.
	LookupByName(ctx context.Context, name string) (UserInfo, bool, error)
}

// NoopUserResolver is a UserResolver that always returns a miss. Used as the
// default when no real resolver is wired (e.g., in unit tests).
type NoopUserResolver struct{}

func (NoopUserResolver) LookupByEmail(context.Context, string) (UserInfo, bool, error) {
	return UserInfo{}, false, nil
}

func (NoopUserResolver) LookupByName(context.Context, string) (UserInfo, bool, error) {
	return UserInfo{}, false, nil
}

// CachedUserResolver wraps an underlying resolver with a TTL cache and
// per-key single-flight to collapse concurrent lookups for the same user.
// Negative results are cached briefly so a missing user doesn't translate to
// a request storm.
type CachedUserResolver struct {
	inner       UserResolver
	posTTL      time.Duration
	negTTL      time.Duration
	now         func() time.Time
	mu          sync.Mutex
	emailCache  map[string]cachedUser
	nameCache   map[string]cachedUser
	emailFlight map[string]*flight
	nameFlight  map[string]*flight
}

type cachedUser struct {
	info    UserInfo
	ok      bool
	expires time.Time
}

type flight struct {
	done chan struct{}
	info UserInfo
	ok   bool
	err  error
}

// NewCachedUserResolver wraps inner with a TTL cache. posTTL applies to hits;
// negTTL to misses (kept short so newly-created users become resolvable
// quickly). When inner is nil, lookups always miss.
func NewCachedUserResolver(inner UserResolver, posTTL, negTTL time.Duration) *CachedUserResolver {
	if inner == nil {
		inner = NoopUserResolver{}
	}
	if posTTL <= 0 {
		posTTL = 5 * time.Minute
	}
	if negTTL <= 0 {
		negTTL = 30 * time.Second
	}
	return &CachedUserResolver{
		inner:       inner,
		posTTL:      posTTL,
		negTTL:      negTTL,
		now:         time.Now,
		emailCache:  make(map[string]cachedUser),
		nameCache:   make(map[string]cachedUser),
		emailFlight: make(map[string]*flight),
		nameFlight:  make(map[string]*flight),
	}
}

func (c *CachedUserResolver) LookupByEmail(ctx context.Context, email string) (UserInfo, bool, error) {
	if email == "" {
		return UserInfo{}, false, nil
	}
	return c.lookup(ctx, email, c.emailCache, c.emailFlight, c.inner.LookupByEmail)
}

func (c *CachedUserResolver) LookupByName(ctx context.Context, name string) (UserInfo, bool, error) {
	if name == "" {
		return UserInfo{}, false, nil
	}
	return c.lookup(ctx, name, c.nameCache, c.nameFlight, c.inner.LookupByName)
}

type lookupFn func(context.Context, string) (UserInfo, bool, error)

func (c *CachedUserResolver) lookup(
	ctx context.Context,
	key string,
	cache map[string]cachedUser,
	flights map[string]*flight,
	fetch lookupFn,
) (UserInfo, bool, error) {
	c.mu.Lock()
	if entry, found := cache[key]; found && c.now().Before(entry.expires) {
		c.mu.Unlock()
		return entry.info, entry.ok, nil
	}

	if f, inflight := flights[key]; inflight {
		c.mu.Unlock()
		<-f.done
		return f.info, f.ok, f.err
	}

	f := &flight{done: make(chan struct{})}
	flights[key] = f
	c.mu.Unlock()

	info, ok, err := fetch(ctx, key)

	c.mu.Lock()
	delete(flights, key)
	if err == nil {
		ttl := c.negTTL
		if ok {
			ttl = c.posTTL
		}
		cache[key] = cachedUser{info: info, ok: ok, expires: c.now().Add(ttl)}
	}
	c.mu.Unlock()

	f.info, f.ok, f.err = info, ok, err
	close(f.done)
	return info, ok, err
}
