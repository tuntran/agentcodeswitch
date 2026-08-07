// Command acs (Wails entry) is the desktop UI.
//
// app.go is a binding layer and nothing more: every method translates a call into
// one internal/ call and a DTO. Business rules belong in internal/, so that the UI
// and the CLI cannot drift -- if the dashboard ever disagrees with `acs quota`,
// that is a bug rather than a UI decision.
package main

import (
	"context"
	"sync"
	"time"

	"github.com/tuntran/agentcodeswitch/internal/doctor"
	"github.com/tuntran/agentcodeswitch/internal/profile"
	"github.com/tuntran/agentcodeswitch/internal/quota"
	"github.com/tuntran/agentcodeswitch/internal/version"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// quotaRefreshInterval is how often the dashboard refreshes itself. The UI process
// is long-lived, so real stale-while-revalidate is safe here -- unlike the CLI,
// where a background refresh dies with the process.
const quotaRefreshInterval = 5 * time.Minute

// QuotaUpdatedEvent is emitted for each refreshed profile.
const QuotaUpdatedEvent = "quota:updated"

// App holds the bindings exposed to the frontend.
type App struct {
	ctx   context.Context
	store profile.CredStore
	quota *quota.Reader

	// mu guards stopRefresh: bindings are invoked from the webview and are not
	// promised to arrive on one goroutine.
	mu          sync.Mutex
	stopRefresh context.CancelFunc
}

// NewApp builds the binding layer.
func NewApp() *App {
	store := profile.DefaultCredStore()
	return &App{store: store, quota: quota.NewReader(store, version.Current)}
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// ListProfiles returns every profile with its cached identity.
func (a *App) ListProfiles() ([]ProfileView, error) {
	profiles, err := profile.List()
	out := make([]ProfileView, 0, len(profiles))
	for _, p := range profiles {
		out = append(out, toProfileView(p, a.store))
	}
	// Both are returned: a broken entry is worth reporting without hiding the
	// profiles that are fine.
	return out, err
}

// GetQuota fetches one profile's quota, synchronously.
func (a *App) GetQuota(name string, force bool) (QuotaView, error) {
	p, err := profile.Get(name)
	if err != nil {
		return QuotaView{}, err
	}
	return toQuotaView(a.quota.Get(a.ctx, p, force)), nil
}

// CachedQuota returns what is on disk without a network call or a Keychain prompt,
// so the dashboard can paint immediately.
func (a *App) CachedQuota(name string) QuotaView {
	return toQuotaView(quota.Cached(name, time.Now()))
}

// SubscribeQuota starts the background refresh loop, emitting QuotaUpdatedEvent per
// profile.
//
// Calling it again REPLACES the running loop instead of adding one. The dashboard
// calls it whenever it mounts, and stacking loops would multiply the request rate
// against an endpoint that rate limits -- while leaking a goroutine per navigation.
// Restarting is also what picks up a profile added since the last call.
func (a *App) SubscribeQuota() error {
	profiles, err := profile.List()
	if err != nil && len(profiles) == 0 {
		return err
	}

	a.mu.Lock()
	if a.stopRefresh != nil {
		a.stopRefresh()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.stopRefresh = cancel
	a.mu.Unlock()

	results := a.quota.Subscribe(ctx, profiles, quotaRefreshInterval)
	go func() {
		for r := range results {
			wailsruntime.EventsEmit(a.ctx, QuotaUpdatedEvent, toQuotaView(r))
		}
	}()
	return nil
}

// CreateProfileEntry registers a profile without authenticating it.
//
// Deliberately only half of adding an account: the login half needs a terminal, so
// it goes through LaunchLoginTerminal.
func (a *App) CreateProfileEntry(name, label string) (ProfileView, error) {
	p, err := profile.Create(name, label)
	if err != nil {
		return ProfileView{}, err
	}
	return toProfileView(p, a.store), nil
}

// LaunchLoginTerminal opens Terminal.app running `acs login <name>`.
func (a *App) LaunchLoginTerminal(name string) error { return launchLoginTerminal(name) }

// LaunchProfileTerminal opens Terminal.app running `acs <name>`.
//
// This is the fix for an expired access token: Claude Code refreshes it on start, so
// starting Claude Code is the whole remedy. `acs` itself stays read-only on
// credentials.
func (a *App) LaunchProfileTerminal(name string) error { return launchProfileTerminal(name) }

// PollLoginDone reports whether a login has completed.
//
// Both halves are required: a Keychain item can exist while the config dir has no
// identity yet, and reporting success on the first one alone would show an account
// with a blank email.
func (a *App) PollLoginDone(name string) (bool, error) {
	p, err := profile.Get(name)
	if err != nil {
		return false, err
	}
	exists, err := a.store.Exists(a.store.ServiceName(p.Literal))
	if err != nil || !exists {
		return false, err
	}
	if _, err := profile.ReadIdentity(p.Literal); err != nil {
		return false, nil
	}
	return true, nil
}

// CancelAdd rolls back a profile whose login never finished.
//
// The Keychain is left alone: a partly finished login can have produced a real
// credential for a real account, and tidying that away would destroy something the
// user has.
func (a *App) CancelAdd(name string) error { return profile.DiscardNew(name) }

// InspectPurge reports what a purge would delete, so the UI can show it before
// asking.
func (a *App) InspectPurge(name string) (PurgeView, error) {
	p, err := profile.Get(name)
	if err != nil {
		return PurgeView{}, err
	}
	plan, err := profile.InspectPurge(p)
	if err != nil {
		return PurgeView{}, err
	}
	view := PurgeView{Dir: plan.Dir, JSONLCount: plan.JSONLCount}
	if !plan.Oldest.IsZero() {
		view.Oldest = plan.Oldest.Format(time.DateOnly)
		view.Newest = plan.Newest.Format(time.DateOnly)
	}
	return view, nil
}

// RemoveProfile removes a profile, mapping straight onto profile.Remove.
//
// typedName is passed through rather than interpreted. profile.Remove requires it to
// equal name before it will purge, which is what stops the UI from reducing the
// confirmation to a single button: <dir>/projects/ is the only copy of that
// account's Claude Code history.
func (a *App) RemoveProfile(name string, purge bool, typedName string) (string, error) {
	out, err := profile.Remove(a.store, name, purge, typedName)
	if err != nil {
		return "", err
	}
	return out.HistoryKept, nil
}

// RunDoctor self-checks the installation. deep reads secrets, so the frontend warns
// about the Keychain prompt before offering it.
func (a *App) RunDoctor(deep bool) DoctorView {
	return toDoctorView(doctor.Run(a.store, deep))
}
