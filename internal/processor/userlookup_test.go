package processor

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver is a UserResolver whose responses are configured per-test.
type fakeResolver struct {
	emailCalls atomic.Int64
	nameCalls  atomic.Int64
	emails     map[string]UserInfo
	names      map[string]UserInfo
	wait       chan struct{}
	emailErr   error
	nameErr    error
}

func (f *fakeResolver) LookupByEmail(ctx context.Context, email string) (UserInfo, bool, error) {
	f.emailCalls.Add(1)
	if f.wait != nil {
		<-f.wait
	}
	if f.emailErr != nil {
		return UserInfo{}, false, f.emailErr
	}
	info, ok := f.emails[email]
	return info, ok, nil
}

func (f *fakeResolver) LookupByName(ctx context.Context, name string) (UserInfo, bool, error) {
	f.nameCalls.Add(1)
	if f.wait != nil {
		<-f.wait
	}
	if f.nameErr != nil {
		return UserInfo{}, false, f.nameErr
	}
	info, ok := f.names[name]
	return info, ok, nil
}

func TestUserInfo_DisplayName(t *testing.T) {
	cases := []struct {
		name string
		in   UserInfo
		want string
	}{
		{"both", UserInfo{GivenName: "Smith", FamilyName: "Nelson"}, "Smith Nelson"},
		{"given only", UserInfo{GivenName: "Smith"}, "Smith"},
		{"family only", UserInfo{FamilyName: "Nelson"}, "Nelson"},
		{"email fallback", UserInfo{Email: "smith@datum.net"}, "smith@datum.net"},
		{"empty", UserInfo{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.DisplayName(); got != tc.want {
				t.Fatalf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCachedUserResolver_PositiveHitCached(t *testing.T) {
	inner := &fakeResolver{emails: map[string]UserInfo{
		"smith@datum.net": {GivenName: "Smith", FamilyName: "Nelson", Email: "smith@datum.net"},
	}}
	c := NewCachedUserResolver(inner, time.Minute, time.Minute)

	for i := 0; i < 5; i++ {
		info, ok, err := c.LookupByEmail(context.Background(), "smith@datum.net")
		if err != nil || !ok || info.DisplayName() != "Smith Nelson" {
			t.Fatalf("iteration %d: got info=%+v ok=%v err=%v", i, info, ok, err)
		}
	}
	if got := inner.emailCalls.Load(); got != 1 {
		t.Fatalf("inner LookupByEmail called %d times, want 1", got)
	}
}

func TestCachedUserResolver_NegativeHitCached(t *testing.T) {
	inner := &fakeResolver{emails: map[string]UserInfo{}}
	c := NewCachedUserResolver(inner, time.Minute, time.Minute)

	for i := 0; i < 3; i++ {
		_, ok, err := c.LookupByEmail(context.Background(), "missing@datum.net")
		if err != nil || ok {
			t.Fatalf("iteration %d: ok=%v err=%v", i, ok, err)
		}
	}
	if got := inner.emailCalls.Load(); got != 1 {
		t.Fatalf("inner LookupByEmail called %d times, want 1 (negative cache miss)", got)
	}
}

func TestCachedUserResolver_TTLExpiry(t *testing.T) {
	inner := &fakeResolver{emails: map[string]UserInfo{
		"smith@datum.net": {GivenName: "Smith", Email: "smith@datum.net"},
	}}
	c := NewCachedUserResolver(inner, 50*time.Millisecond, 50*time.Millisecond)

	if _, ok, _ := c.LookupByEmail(context.Background(), "smith@datum.net"); !ok {
		t.Fatal("first lookup must hit")
	}
	now := time.Now()
	c.now = func() time.Time { return now.Add(time.Hour) }
	if _, ok, _ := c.LookupByEmail(context.Background(), "smith@datum.net"); !ok {
		t.Fatal("post-TTL lookup must still resolve via inner")
	}
	if got := inner.emailCalls.Load(); got != 2 {
		t.Fatalf("inner LookupByEmail called %d times, want 2", got)
	}
}

func TestCachedUserResolver_SingleFlight(t *testing.T) {
	inner := &fakeResolver{
		emails: map[string]UserInfo{"smith@datum.net": {GivenName: "Smith", Email: "smith@datum.net"}},
		wait:   make(chan struct{}),
	}
	c := NewCachedUserResolver(inner, time.Minute, time.Minute)

	const concurrent = 20
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = c.LookupByEmail(context.Background(), "smith@datum.net")
		}()
	}

	// Give all goroutines a moment to enqueue behind the in-flight lookup.
	time.Sleep(20 * time.Millisecond)
	close(inner.wait)
	wg.Wait()

	if got := inner.emailCalls.Load(); got != 1 {
		t.Fatalf("inner LookupByEmail called %d times, want 1 (single-flight)", got)
	}
}

func TestCachedUserResolver_ErrorNotCached(t *testing.T) {
	sentinel := errors.New("boom")
	inner := &fakeResolver{emailErr: sentinel}
	c := NewCachedUserResolver(inner, time.Minute, time.Minute)

	for i := 0; i < 3; i++ {
		_, _, err := c.LookupByEmail(context.Background(), "smith@datum.net")
		if !errors.Is(err, sentinel) {
			t.Fatalf("iteration %d: err=%v, want %v", i, err, sentinel)
		}
	}
	if got := inner.emailCalls.Load(); got != 3 {
		t.Fatalf("inner LookupByEmail called %d times, want 3 (errors are not cached)", got)
	}
}

func TestCachedUserResolver_EmptyKeysShortCircuit(t *testing.T) {
	inner := &fakeResolver{}
	c := NewCachedUserResolver(inner, time.Minute, time.Minute)

	if _, ok, err := c.LookupByEmail(context.Background(), ""); ok || err != nil {
		t.Fatalf("empty email should miss without err; ok=%v err=%v", ok, err)
	}
	if _, ok, err := c.LookupByName(context.Background(), ""); ok || err != nil {
		t.Fatalf("empty name should miss without err; ok=%v err=%v", ok, err)
	}
	if got := inner.emailCalls.Load() + inner.nameCalls.Load(); got != 0 {
		t.Fatalf("inner called %d times, want 0", got)
	}
}

func TestNoopUserResolver(t *testing.T) {
	r := NoopUserResolver{}
	if _, ok, err := r.LookupByEmail(context.Background(), "x"); ok || err != nil {
		t.Fatalf("noop email lookup ok=%v err=%v", ok, err)
	}
	if _, ok, err := r.LookupByName(context.Background(), "x"); ok || err != nil {
		t.Fatalf("noop name lookup ok=%v err=%v", ok, err)
	}
}
