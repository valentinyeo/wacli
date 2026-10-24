package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

func TestChatStateRateLimitedFetchRetriesIncrementally(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		for _, action := range []string{"archive", "mark-read"} {
			t.Run(fmt.Sprintf("%s/degraded=%t", action, degraded), func(t *testing.T) {
				a := newTestApp(t)
				f := &appStateContextWA{fakeWA: newFakeWA()}
				a.wa = f
				chat := types.NewJID("15550000758", types.DefaultUserServer)
				if err := a.db.UpsertChat(chat.String(), "dm", "Synthetic", time.Now()); err != nil {
					t.Fatal(err)
				}
				if err := a.db.SetChatUnread(chat.String(), true); err != nil {
					t.Fatal(err)
				}
				name := string(appstate.WAPatchRegularLow)
				var priorGeneration int64
				if degraded {
					a.setAppStateFullSyncResult(appstate.WAPatchRegularLow, true)
					f.hasLocalAppState = true
					var err error
					priorGeneration, err = a.db.MarkAppStateRecoveryGeneration(name)
					if err != nil {
						t.Fatal(err)
					}
				}
				fetches := 0
				rateLimited := true
				fetchErr := fmt.Errorf("failed to fetch app state regular_low patches: %w", whatsmeow.ErrIQRateOverLimit)
				f.fetchEvents = func(_ context.Context, collection string, fullSync, onlyIfNotSynced bool) ([]any, error) {
					fetches++
					if fullSync {
						t.Fatal("rate-limited incremental fetch triggered a full replay")
					}
					if collection != name || onlyIfNotSynced {
						t.Fatalf("fetch = %s, %t; want regular_low, false", collection, onlyIfNotSynced)
					}
					if rateLimited {
						return nil, fetchErr
					}
					return nil, nil
				}
				f.requestAppStateRecovery = func(context.Context, string) (types.MessageID, error) {
					if rateLimited {
						t.Fatal("rate-limited incremental fetch requested primary recovery")
					}
					return "", errors.New("primary offline")
				}
				write := func() error {
					if action == "archive" {
						return a.ArchiveChat(t.Context(), chat, true)
					}
					return a.MarkChatRead(t.Context(), chat, true)
				}
				for range 2 {
					if err := write(); !errors.Is(err, whatsmeow.ErrIQRateOverLimit) {
						t.Fatalf("write error = %v, want rate limit", err)
					}
					select {
					case <-a.chatStateSync:
						a.chatStateSync <- struct{}{}
					default:
						t.Fatal("rate-limited incremental fetch held chat-state semaphore")
					}
					if len(f.archiveCalls) != 0 || len(f.markReadCalls) != 0 {
						t.Fatal("write sent before incremental fetch succeeded")
					}
				}
				if required, err := a.db.AppStateRecoveryRequired(name); err != nil || required != degraded {
					t.Fatalf("recovery required = %t, %v; want %t", required, err, degraded)
				}
				if degraded && !recoveryIntentExists(t, a, name, priorGeneration) {
					t.Fatal("rate-limited fetch cleared earlier replay debt")
				}
				rateLimited = false
				if err := write(); err != nil {
					t.Fatalf("write after rate limit = %v", err)
				}
				if fetches != 3 {
					t.Fatalf("incremental fetches = %d, want 3", fetches)
				}
				stored, err := a.db.GetChat(chat.String())
				if err != nil {
					t.Fatal(err)
				}
				if action == "archive" {
					if len(f.archiveCalls) != 1 || !stored.Archived {
						t.Fatalf("archive calls = %d, archived = %t", len(f.archiveCalls), stored.Archived)
					}
				} else if len(f.markReadCalls) != 1 || stored.Unread {
					t.Fatalf("mark-read calls = %d, marked unread = %t", len(f.markReadCalls), stored.Unread)
				}
			})
		}
	}
}

func TestRetryableAppStateFetchErrors(t *testing.T) {
	for _, err := range []error{
		whatsmeow.ErrIQRateOverLimit,
		&whatsmeow.IQError{Code: 429, Text: "other"},
		&whatsmeow.IQError{Text: "rate-overlimit"},
		whatsmeow.ErrIQResourceLimit,
		whatsmeow.ErrIQInternalServerError,
		whatsmeow.ErrIQServiceUnavailable,
		whatsmeow.ErrIQPartialServerError,
		whatsmeow.ErrIQTimedOut,
		whatsmeow.ErrIQDisconnected,
	} {
		if !isRetryableAppStateFetchError(fmt.Errorf("fetch: %w", err)) {
			t.Errorf("retryable fetch error classified as replay debt: %v", err)
		}
	}
	for _, err := range []error{
		appstate.ErrMismatchingLTHash,
		appstate.ErrKeyNotFound,
		wa.ErrEmptyAppStateKeyShare,
		appstate.ErrMismatchingPatchMAC,
		whatsmeow.ErrIQNotAuthorized,
		errors.New("database failure"),
	} {
		if isRetryableAppStateFetchError(fmt.Errorf("fetch: %w", err)) {
			t.Errorf("non-transient fetch error classified as retryable: %v", err)
		}
	}
}

func TestChatStateRepairErrorsStillReplay(t *testing.T) {
	for _, fetchErr := range []error{
		appstate.ErrMismatchingLTHash,
		appstate.ErrKeyNotFound,
		wa.ErrEmptyAppStateKeyShare,
	} {
		t.Run(fetchErr.Error(), func(t *testing.T) {
			a := newTestApp(t)
			f := newFakeWA()
			a.wa = f
			f.appStateFetchErrs = []error{fmt.Errorf("decode: %w", fetchErr), nil}
			chat := types.NewJID("15550000758", types.DefaultUserServer)
			if err := a.ArchiveChat(t.Context(), chat, true); err != nil {
				t.Fatal(err)
			}
			if len(f.appStateFetches) != 2 || f.appStateFetches[0].fullSync || !f.appStateFetches[1].fullSync {
				t.Fatalf("fetches = %+v, want incremental then full replay", f.appStateFetches)
			}
		})
	}
}
