package app

import (
	"context"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestArchiveSurvivesOlderRecoverySnapshot(t *testing.T) {
	a := newTestApp(t)
	a.wa = newFakeWA()
	ctx := context.Background()
	chat := types.JID{User: "123456789-111", Server: types.GroupServer}
	t1 := time.Date(2024, 2, 1, 12, 0, 0, 0, time.UTC)
	t0 := t1.Add(-time.Hour)
	t2 := t1.Add(time.Hour)
	if err := a.db.UpsertChat(chat.String(), "group", "Test group", t0); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}

	if err := a.handleChatStateEvent(ctx, &events.Archive{
		JID:       chat,
		Timestamp: t1,
		Action:    &waSyncAction.ArchiveChatAction{Archived: proto.Bool(true)},
	}); err != nil {
		t.Fatalf("archive: %v", err)
	}
	if err := a.handleChatStateEvent(ctx, &events.Archive{
		JID:       chat,
		Timestamp: t0,
		Action:    &waSyncAction.ArchiveChatAction{Archived: proto.Bool(false)},
	}); err != nil {
		t.Fatalf("older recovery snapshot: %v", err)
	}
	stored, err := a.db.GetChat(chat.String())
	if err != nil {
		t.Fatalf("GetChat after recovery: %v", err)
	}
	if !stored.Archived {
		t.Fatalf("older recovery snapshot undid archive: %+v", stored)
	}

	if err := a.handleChatStateEvent(ctx, &events.Archive{
		JID:       chat,
		Timestamp: t2,
		Action:    &waSyncAction.ArchiveChatAction{Archived: proto.Bool(false)},
	}); err != nil {
		t.Fatalf("newer unarchive: %v", err)
	}
	stored, err = a.db.GetChat(chat.String())
	if err != nil {
		t.Fatalf("GetChat after newer unarchive: %v", err)
	}
	if stored.Archived {
		t.Fatalf("newer unarchive did not apply: %+v", stored)
	}
}
