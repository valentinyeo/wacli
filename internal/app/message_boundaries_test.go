package app

import (
	"bytes"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/out"
	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestHardenHistorySyncNotificationIgnoresOtherSenders(t *testing.T) {
	notif := &waE2E.HistorySyncNotification{SyncType: waE2E.HistorySyncType_INITIAL_BOOTSTRAP.Enum()}
	evt := &events.Message{
		Info:    types.MessageInfo{MessageSource: types.MessageSource{IsFromMe: false}},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{HistorySyncNotification: notif}},
	}
	if got := historySyncNotificationFromMessage(evt); got != nil {
		t.Errorf("notification from another sender = %p, want nil", got)
	}
	evt.Info.IsFromMe = true
	if got := historySyncNotificationFromMessage(evt); got != notif {
		t.Fatalf("own-device notification = %p, want %p", got, notif)
	}
}

func TestHardenPollAddOptionStaysInItsChat(t *testing.T) {
	a := newTestApp(t)
	f := newFakeWA()
	a.wa = f
	ctx := t.Context()
	chat := types.NewJID("15550000001", types.DefaultUserServer)
	other := types.NewJID("15550000002", types.DefaultUserServer)
	alias := types.NewJID("100000000001", types.HiddenUserServer)
	f.lids[alias] = chat
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	creation := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
			ID:            "poll-original",
			Timestamp:     base,
		},
		Message: &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{
			Name:    proto.String("Dinner?"),
			Options: []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Yes")}, {OptionName: proto.String("No")}},
		}},
	}
	var stored atomic.Int64
	a.handleLiveSyncMessage(ctx, SyncOptions{}, creation, &stored, nil, nil)
	if poll, err := a.db.GetPoll(chat.String(), creation.Info.ID); err != nil || !sameStringSet(poll.Options, []string{"Yes", "No"}) {
		t.Fatalf("initial poll = %+v, error = %v", poll, err)
	}
	for _, tc := range []struct {
		id, option string
		envelope   types.JID
		reference  types.JID
		want       []string
	}{
		{"other-chat", "Maybe", other, chat, []string{"Yes", "No"}},
		{"other-chat-fallback", "Later", other, other, []string{"Yes", "No"}},
		{"same-chat", "Maybe", chat, chat, []string{"Yes", "No", "Maybe"}},
		{"same-chat-alias", "Later", alias, alias, []string{"Yes", "No", "Maybe", "Later"}},
	} {
		evt := &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{Chat: tc.envelope, Sender: tc.envelope},
				ID:            tc.id,
				Timestamp:     base.Add(time.Minute),
			},
			Message: &waE2E.Message{PollAddOptionMessage: &waE2E.PollAddOptionMessage{
				PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String(creation.Info.ID), RemoteJID: proto.String(tc.reference.String())},
				AddOption:              &waE2E.PollCreationMessage_Option{OptionName: proto.String(tc.option)},
			}},
		}
		a.handleLiveSyncMessage(ctx, SyncOptions{}, evt, &stored, nil, nil)
		poll, err := a.db.GetPoll(chat.String(), creation.Info.ID)
		if err != nil {
			t.Fatalf("GetPoll after %s: %v", tc.id, err)
		}
		if !sameStringSet(poll.Options, tc.want) {
			t.Errorf("poll options after %s = %v, want %v", tc.id, poll.Options, tc.want)
		}
	}

	original, err := a.db.GetMessage(chat.String(), creation.Info.ID)
	if err != nil {
		t.Fatalf("GetMessage original: %v", err)
	}
	pm := wa.ParseLiveMessage(creation)
	pm.Text = "updated body"
	pm.SenderJID = other.String()
	if err := a.storeParsedMessage(ctx, pm, messageStoreOptions{}); err == nil {
		t.Errorf("different sender replaced an existing message")
	}
	pm.SenderJID = chat.String()
	pm.FromMe = true
	if err := a.storeParsedMessage(ctx, pm, messageStoreOptions{}); err == nil {
		t.Errorf("different FromMe replaced an existing message")
	}
	if got, err := a.db.GetMessage(chat.String(), pm.ID); err != nil || got.Text != original.Text || got.FromMe != original.FromMe || got.SenderJID != original.SenderJID {
		t.Errorf("existing message changed: %+v, error = %v", got, err)
	}
	pm.FromMe = false
	pm.SenderJID = alias.String()
	if err := a.storeParsedMessage(ctx, pm, messageStoreOptions{}); err != nil {
		t.Fatalf("same sender update through alias: %v", err)
	}
	if got, err := a.db.GetMessage(chat.String(), pm.ID); err != nil || got.Text != pm.Text || got.FromMe || got.SenderJID != chat.String() {
		t.Fatalf("same sender update = %+v, error = %v", got, err)
	}
}

func TestHardenWarningStripsTerminalControls(t *testing.T) {
	a := newTestApp(t)
	message := "warning: missing\x1b[2J\x1b[H status"
	want := out.SanitizeHuman(message) + "\n"
	raw := captureStderr(t, func() {
		a.emitWarning("input_warning", message, nil)
	})
	if strings.ContainsRune(raw, '\x1b') || raw != want {
		t.Errorf("human warning = %q, want %q without ESC bytes", raw, want)
	}
	var buf bytes.Buffer
	a.status = newSyncStatus(&buf)
	a.emitWarning("input_warning", message, nil)
	if raw := buf.String(); strings.ContainsRune(raw, '\x1b') || raw != want {
		t.Errorf("status warning = %q, want %q without ESC bytes", raw, want)
	}
}
