package wa

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow"
	waStore "go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestRewriteMediaRetryInfoForLIDRewritesDM(t *testing.T) {
	chat := types.NewJID("15551234567", types.DefaultUserServer)
	sender := types.NewJID("15557654321", types.DefaultUserServer)
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: chat, Sender: sender}, ID: "media-id"}
	cli := &whatsmeow.Client{Store: &waStore.Device{LIDMigrationTimestamp: 1}}

	got := rewriteMediaRetryInfoForLID(context.Background(), cli, info, func(_ context.Context, _ *whatsmeow.Client, jid types.JID) types.JID {
		return types.NewJID(jid.User+"lid", types.HiddenUserServer)
	})
	if got.Chat.Server != types.HiddenUserServer || got.Sender.Server != types.HiddenUserServer {
		t.Fatalf("rewritten source = %+v", got.MessageSource)
	}
}

func TestRewriteMediaRetryInfoForLIDUsesGroupAddressingMode(t *testing.T) {
	group := types.NewJID("120363001234567890", types.GroupServer)
	sender := types.NewJID("15557654321", types.DefaultUserServer)
	cli := &whatsmeow.Client{Store: &waStore.Device{LIDMigrationTimestamp: 1}}

	for _, tc := range []struct {
		name string
		mode types.AddressingMode
		want types.JID
	}{
		{name: "lid", mode: types.AddressingModeLID, want: types.NewJID(sender.User+"lid", types.HiddenUserServer)},
		{name: "pn", mode: types.AddressingModePN, want: sender},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := types.MessageInfo{MessageSource: types.MessageSource{Chat: group, Sender: sender, IsGroup: true, AddressingMode: tc.mode}, ID: "media-id"}
			got := rewriteMediaRetryInfoForLID(context.Background(), cli, info, func(_ context.Context, _ *whatsmeow.Client, jid types.JID) types.JID {
				return types.NewJID(jid.User+"lid", types.HiddenUserServer)
			})
			if got.Sender != tc.want {
				t.Fatalf("sender = %s, want %s", got.Sender, tc.want)
			}
		})
	}
}

func TestDecryptMediaRetryNormalizesNotFound(t *testing.T) {
	directPath, code, err := DecryptMediaRetry(&events.MediaRetry{
		Error: &events.MediaRetryError{Code: 2},
	}, nil)
	if err != nil {
		t.Fatalf("DecryptMediaRetry: %v", err)
	}
	if directPath != "" || code != MediaRetryNotFound {
		t.Fatalf("result = (%q, %d), want empty path and not-found code", directPath, code)
	}
}

func TestDecryptMediaRetryPreservesUnknownError(t *testing.T) {
	_, code, err := DecryptMediaRetry(&events.MediaRetry{
		Error: &events.MediaRetryError{Code: 9},
	}, nil)
	if err == nil {
		t.Fatalf("expected unknown retry error")
	}
	if code != MediaRetryGeneralError {
		t.Fatalf("code = %d, want general error", code)
	}
}

func TestStatusReceiptLIDRewriteKeepsBroadcastChatAndPoster(t *testing.T) {
	poster := types.NewJID("15557654321", types.DefaultUserServer)
	lid := types.NewJID("987654321", types.HiddenUserServer)
	for _, migrated := range []int64{0, 1} {
		cli := &whatsmeow.Client{Store: &waStore.Device{LIDMigrationTimestamp: migrated}}
		info := types.MessageInfo{MessageSource: types.MessageSource{
			Chat: types.StatusBroadcastJID, Sender: poster, AddressingMode: types.AddressingModeLID,
		}}
		got := rewriteMediaRetryInfoForLID(t.Context(), cli, info, func(_ context.Context, _ *whatsmeow.Client, jid types.JID) types.JID {
			if jid != poster {
				t.Fatalf("resolving non-poster %s", jid)
			}
			return lid
		})
		want := poster
		if migrated > 0 {
			want = lid
		}
		if got.Chat != types.StatusBroadcastJID || got.Sender != want || got.IsGroup {
			t.Fatalf("migration=%d source=%+v", migrated, got.MessageSource)
		}
	}
}
