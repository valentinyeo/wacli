package wa

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

func TestHardenProtocolKeyKeepsEnvelopeIdentity(t *testing.T) {
	chat := types.NewJID("15550000001", types.DefaultUserServer)
	other := types.NewJID("15550000002", types.DefaultUserServer)
	sender := types.NewJID("15550000003", types.DefaultUserServer)
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: sender, IsFromMe: false},
			ID:            "edit-event",
			Timestamp:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
			Type: waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key: &waCommon.MessageKey{
				ID:          proto.String("original"),
				RemoteJID:   proto.String(other.String()),
				Participant: proto.String(other.String()),
				FromMe:      proto.Bool(true),
			},
			EditedMessage: &waE2E.Message{Conversation: proto.String("updated body")},
		}},
	}
	pm := ParseLiveMessage(evt)
	if pm.Chat != chat || pm.SenderJID != sender.String() || pm.FromMe {
		t.Errorf("incoming edit changed envelope identity: %+v", pm)
	}
	if pm.ID != "original" || pm.Text != "updated body" || !pm.Edited {
		t.Errorf("incoming edit lost its content or target: %+v", pm)
	}

	evt.Info.IsFromMe = true
	pm = ParseLiveMessage(evt)
	if pm.Chat != other || pm.SenderJID != other.String() || !pm.FromMe || pm.ID != "original" || pm.Text != "updated body" || !pm.Edited {
		t.Errorf("own-device edit changed: %+v", pm)
	}
	evt.Message.ProtocolMessage.Type = waE2E.ProtocolMessage_REVOKE.Enum()
	evt.Message.ProtocolMessage.Key.FromMe = proto.Bool(false)
	pm = ParseLiveMessage(evt)
	if pm.Chat != other || pm.SenderJID != other.String() || pm.FromMe || pm.ID != "original" || !pm.Revoked {
		t.Errorf("own-device revoke changed: %+v", pm)
	}

	evt.Message = &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{
		DestinationJID: proto.String(other.String()),
		Message:        &waE2E.Message{Conversation: proto.String("device body")},
	}}
	for _, fromMe := range []bool{false, true} {
		evt.Info.IsFromMe = fromMe
		pm = ParseLiveMessage(evt)
		wantChat := chat
		if fromMe {
			wantChat = other
		}
		if pm.Chat != wantChat || pm.SenderJID != sender.String() || pm.FromMe != fromMe || pm.Text != "device body" {
			t.Errorf("device message with envelope FromMe=%t changed identity: %+v", fromMe, pm)
		}
		evt.Info.DeviceSentMeta = &types.DeviceSentMeta{DestinationJID: other.String()}
		evt.Message = &waE2E.Message{Conversation: proto.String("metadata body")}
		pm = ParseLiveMessage(evt)
		if pm.Chat != wantChat || pm.SenderJID != sender.String() || pm.FromMe != fromMe || pm.Text != "metadata body" {
			t.Errorf("device metadata with envelope FromMe=%t changed identity: %+v", fromMe, pm)
		}
		evt.Info.DeviceSentMeta = nil
		evt.Message = &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{
			DestinationJID: proto.String(other.String()),
			Message:        &waE2E.Message{Conversation: proto.String("device body")},
		}}
	}
}
