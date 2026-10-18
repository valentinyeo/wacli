package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/store"
	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/types"
)

func seedStatusSeen(t *testing.T, a *App) {
	t.Helper()
	for _, m := range []store.UpsertStatusMessageParams{
		{MsgID: "a1", SenderJID: "111@s.whatsapp.net"},
		{MsgID: "b1", SenderJID: "222@lid"},
		{MsgID: "a2", SenderJID: "111@s.whatsapp.net"},
		{MsgID: "own", SenderJID: "333@s.whatsapp.net", FromMe: true},
		{MsgID: "no-poster"},
		{MsgID: "empty-poster", SenderJID: "@lid"},
		{MsgID: "group-poster", SenderJID: "123@g.us"},
	} {
		m.Timestamp = time.Now()
		if err := a.db.UpsertStatusMessage(m); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStatusSeenBatchesPostersWithoutGroupLookup(t *testing.T) {
	for _, kind := range []types.ReceiptType{wa.ReadReceiptUnknown, types.ReceiptTypeReadSelf} {
		t.Run(string(kind), func(t *testing.T) {
			a := newTestApp(t)
			f := newFakeWA()
			f.readReceiptType = kind
			f.groupInfoErr = errors.New("status is not a group")
			a.wa = f
			seedStatusSeen(t, a)
			result, err := a.StatusSeen(t.Context(), []string{"a1", "b1", "a2", "a1"}, false)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(result.IDs, []string{"a1", "b1", "a2"}) || !slices.Equal(result.Senders, []string{"111@s.whatsapp.net", "222@lid"}) || result.DryRun || result.Receipt != kind {
				t.Fatalf("result = %+v", result)
			}
			calls := readReceiptCallsOf(f)
			if len(calls) != 2 || f.groupInfoCalls != 0 {
				t.Fatalf("calls = %+v, group lookups = %d", calls, f.groupInfoCalls)
			}
			for i, want := range [][]types.MessageID{{"a1", "a2"}, {"b1"}} {
				if calls[i].chat != types.StatusBroadcastJID || calls[i].sender.String() != result.Senders[i] || calls[i].addressing != types.AddressingModeLID || !slices.Equal(calls[i].ids, want) {
					t.Fatalf("batch %d = %+v", i, calls[i])
				}
			}
		})
	}
}

func TestStatusSeenValidatesEntireSelectionBeforeSending(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"missing", "unknown status message ID"},
		{"own", "your own status"},
		{"no-poster", "no valid poster JID"},
		{"empty-poster", "no valid poster JID"},
		{"group-poster", "no valid poster JID"},
		{" a1", "exact nonempty"},
		{"", "exact nonempty"},
	} {
		for _, dryRun := range []bool{false, true} {
			a := newTestApp(t)
			f := newFakeWA()
			a.wa = f
			seedStatusSeen(t, a)
			_, err := a.StatusSeen(t.Context(), []string{"a1", tc.id}, dryRun)
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(readReceiptCallsOf(f)) != 0 {
				t.Fatalf("id=%q dry=%t err=%v calls=%v", tc.id, dryRun, err, readReceiptCallsOf(f))
			}
		}
	}
}

func TestStatusSeenDryRunDoesNotSend(t *testing.T) {
	a := newTestApp(t)
	seedStatusSeen(t, a)
	result, err := a.StatusSeen(context.Background(), []string{"a1", "b1"}, true)
	if err != nil || !result.DryRun || len(result.Senders) != 2 || result.Receipt != wa.ReadReceiptUnknown {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := a.StatusSeen(t.Context(), nil, true); err == nil {
		t.Fatal("empty selection accepted")
	}
}

func TestStatusSeenSendFailureReportsPartialProgress(t *testing.T) {
	a := newTestApp(t)
	seedStatusSeen(t, a)
	f := newFakeWA()
	f.onReadReceipt = func(n int) (types.ReceiptType, error) {
		if n == 1 {
			return "", errors.New("offline")
		}
		return types.ReceiptTypeReadSelf, nil
	}
	a.wa = f
	_, err := a.StatusSeen(t.Context(), []string{"a1", "a2", "b1"}, false)
	if err == nil || !strings.Contains(err.Error(), "2 already sent") || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("error=%v", err)
	}
}
