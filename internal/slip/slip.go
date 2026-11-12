// Package slip chooses the deterministic "slip of the day" for the ledger.
//
// The pick is a hash of the local calendar date over the ascending live entry
// ids, pinned in the existing settings table under daily_slip.<YYYY-MM-DD> so
// every visitor and every process serves the same entry for the whole day.
// There is no scheduler and no dedicated table.
package slip

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"time"

	"vandyke.thor.edu/vandyke/internal/store"
)

const (
	// KeyPrefix namespaces the pins inside the shared settings table.
	KeyPrefix = "daily_slip."
	// hashSeed keeps the ledger's slip hash distinct from any other FNV use.
	hashSeed  = "vandyke-daily-slip\x00"
	dayLayout = "2006-01-02"
)

// ErrNoSlip reports that the ledger has no live entry, so there is nothing to
// choose. It is deliberately distinct from a transport or storage error.
var ErrNoSlip = errors.New("the ledger has no entry for today")

// Slip is one day's chosen entry.
type Slip struct {
	Date  string
	Entry store.Entry
}

// Resolver computes and pins the day's slip against a store.
type Resolver struct {
	store *store.Store
}

// New builds a resolver. It takes the *store.Store the web layer and the JSON
// API already hold, so no constructor signature changes.
func New(st *store.Store) *Resolver {
	return &Resolver{store: st}
}

// LocalDay formats t as the server's local calendar date. The process time
// zone is deployment configuration (TZ); a container without TZ uses UTC.
func LocalDay(t time.Time) string {
	return t.In(time.Local).Format(dayLayout)
}

// Today resolves the slip for the current local calendar date.
func (r *Resolver) Today(ctx context.Context) (Slip, error) {
	return r.ForDay(ctx, LocalDay(time.Now()))
}

// PinKey is the settings key that holds day's entry id.
func PinKey(day string) string {
	return KeyPrefix + day
}

// ForDay resolves the slip for one YYYY-MM-DD date. The first resolution pins
// the candidate; later resolutions serve the pin, replacing it only when the
// pinned entry no longer resolves through GetEntry.
func (r *Resolver) ForDay(ctx context.Context, day string) (Slip, error) {
	if _, err := time.Parse(dayLayout, day); err != nil {
		return Slip{}, fmt.Errorf("invalid slip day %q: %w", day, err)
	}
	key := PinKey(day)

	// Two passes cover the one race the design allows for: a pin that another
	// request inserted between our read and our insert can disappear (be
	// trashed) before we read it back. The second pass overwrites it.
	for attempt := 0; attempt < 2; attempt++ {
		ids, err := r.store.AllEntryIDs(ctx)
		if err != nil {
			return Slip{}, err
		}
		if len(ids) == 0 {
			return Slip{}, ErrNoSlip
		}

		stored, exists, err := r.store.GetSetting(ctx, key)
		if err != nil {
			return Slip{}, err
		}
		if exists {
			if id, ok := parsePin(stored); ok {
				entry, err := r.store.GetEntry(ctx, id)
				switch {
				case err == nil:
					return Slip{Date: day, Entry: entry}, nil
				case !errors.Is(err, store.ErrNotFound):
					return Slip{}, err
				}
				// The pin was trashed or purged: recompute from the survivors
				// and overwrite the key. A corrupt value takes the same path.
			}
			entry, err := pick(ctx, r.store, day, ids)
			if err != nil {
				return Slip{}, err
			}
			if err := r.store.SetSettings(ctx, map[string]string{key: pinValue(entry.ID)}); err != nil {
				return Slip{}, err
			}
			if err := r.prune(ctx, key); err != nil {
				return Slip{}, err
			}
			return Slip{Date: day, Entry: entry}, nil
		}

		candidate, err := pick(ctx, r.store, day, ids)
		if err != nil {
			return Slip{}, err
		}
		winning, err := r.store.SetSettingIfAbsent(ctx, key, pinValue(candidate.ID))
		if err != nil {
			return Slip{}, err
		}
		id, ok := parsePin(winning)
		if !ok {
			// Another writer left an unreadable value; overwrite it next pass.
			continue
		}
		entry, err := r.store.GetEntry(ctx, id)
		switch {
		case err == nil:
			if err := r.prune(ctx, key); err != nil {
				return Slip{}, err
			}
			return Slip{Date: day, Entry: entry}, nil
		case !errors.Is(err, store.ErrNotFound):
			return Slip{}, err
		}
	}

	return Slip{}, fmt.Errorf("resolve slip for %s: the pinned entry kept disappearing", day)
}

// pick chooses the candidate by hashing the date and walking forward over the
// sorted live ids until an entry still resolves. The walk only matters during
// a race; in the ordinary case the first id is live.
func pick(ctx context.Context, st *store.Store, day string, ids []int64) (store.Entry, error) {
	if len(ids) == 0 {
		return store.Entry{}, ErrNoSlip
	}
	start := candidateIndex(day, len(ids))
	for offset := 0; offset < len(ids); offset++ {
		id := ids[(start+offset)%len(ids)]
		entry, err := st.GetEntry(ctx, id)
		switch {
		case err == nil:
			return entry, nil
		case errors.Is(err, store.ErrNotFound):
			continue
		default:
			return store.Entry{}, err
		}
	}
	return store.Entry{}, ErrNoSlip
}

// candidateIndex maps a date onto an index in a list of n entries. The date is
// part of the seed so the same date and the same id set always agree.
func candidateIndex(day string, n int) int {
	if n <= 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write([]byte(hashSeed + day))
	return int(h.Sum64() % uint64(n))
}

func parsePin(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func pinValue(id int64) string {
	return strconv.FormatInt(id, 10)
}

// prune deletes every other daily pin so the table holds at most one row.
func (r *Resolver) prune(ctx context.Context, keep string) error {
	_, err := r.store.PruneSettingsPrefix(ctx, KeyPrefix, keep)
	return err
}
