package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/lock"
	"github.com/openclaw/wacli/internal/store"
	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/types"
)

func newStatusSeenFixture(t *testing.T, dir string) *app.App {
	t.Helper()
	a, err := app.New(app.Options{StoreDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	if err := a.DB().UpsertStatusMessage(store.UpsertStatusMessageParams{MsgID: "status-id", SenderJID: "111@s.whatsapp.net", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStatusSeenIPCDispatchAndDryRun(t *testing.T) {
	a := newStatusSeenFixture(t, t.TempDir())
	resp, err := executeDelegatedSend(context.Background(), a, sendDelegateRequest{
		Version: sendDelegateVersion, Kind: statusSeenKind, IDs: []string{"status-id"}, DryRun: true,
	})
	if err != nil || !resp.OK || resp.StatusSeen == nil || !resp.StatusSeen.DryRun || !slices.Equal(resp.StatusSeen.IDs, []string{"status-id"}) || !slices.Equal(resp.StatusSeen.Senders, []string{"111@s.whatsapp.net"}) {
		t.Fatalf("response=%+v err=%v", resp, err)
	}
	for _, ids := range [][]string{nil, {"status-id", "unknown"}} {
		if _, err := executeDelegatedSend(t.Context(), a, sendDelegateRequest{Version: sendDelegateVersion, Kind: statusSeenKind, IDs: ids, DryRun: true}); err == nil {
			t.Fatalf("invalid IDs accepted: %v", ids)
		}
	}
}

func TestStatusSeenDirectDryRunReadOnly(t *testing.T) {
	dir := t.TempDir()
	a := newStatusSeenFixture(t, dir)
	a.Close()
	stdout, stderr, err := runPresenceDelegateHelper(t, []string{"--read-only", "--store", dir, "--json", "status", "seen", "--id", "status-id", "--dry-run"})
	if err != nil {
		t.Fatalf("error=%v stdout=%s stderr=%s", err, stdout, stderr)
	}
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.NewDecoder(strings.NewReader(stdout)).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	result := envelope.Data
	if result["ok"] != true || result["dry_run"] != true || result["delegated"] != false || result["sender"] != "111@s.whatsapp.net" || result["receipt"] != "unknown" || result["sender_notified"] != nil {
		t.Fatalf("result=%v", result)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "session.db*")); err != nil || len(matches) != 0 {
		t.Fatalf("dry-run opened a session: files=%v err=%v", matches, err)
	}
}

func TestStatusSeenRejectsReadOnly(t *testing.T) {
	for _, env := range []bool{false, true} {
		args := []string{"--store", t.TempDir(), "status", "seen", "--id", "status-id"}
		if env {
			t.Setenv("WACLI_READONLY", "1")
		} else {
			args = append([]string{"--read-only"}, args...)
		}
		err := execute(args)
		if err == nil || !strings.Contains(err.Error(), "read-only") {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestStatusSeenDelegatesDryRunAndSend(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	for _, dry := range []bool{false, true} {
		dir := shortPresenceDelegateStoreDir(t)
		a := newStatusSeenFixture(t, dir)
		lk, err := lock.Acquire(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer lk.Release()
		server := startPresenceDelegateTestSocket(t, dir, func(req sendDelegateRequest) sendDelegateResponse {
			if req.DryRun {
				resp, err := executeDelegatedSend(t.Context(), a, req)
				if err != nil {
					return sendDelegateResponse{Error: err.Error()}
				}
				return resp
			}
			return sendDelegateResponse{OK: true, StatusSeen: &app.StatusSeenResult{IDs: req.IDs, Senders: []string{"111@s.whatsapp.net"}, Receipt: types.ReceiptTypeReadSelf}}
		})
		defer server.stop()
		args := []string{"--store", dir, "--json", "--timeout", "2s", "status", "seen", "--id", "status-id"}
		if dry {
			args = append(args, "--dry-run", "--read-only")
		}
		stdout, stderr, err := runPresenceDelegateHelper(t, args)
		if err != nil {
			t.Fatalf("dry=%t err=%v stdout=%s stderr=%s", dry, err, stdout, stderr)
		}
		req := server.nextRequest(t)
		if req.Kind != statusSeenKind || req.DryRun != dry || !slices.Equal(req.IDs, []string{"status-id"}) {
			t.Fatalf("request=%+v", req)
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(strings.NewReader(stdout)).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		result := envelope.Data
		if result["delegated"] != true || result["dry_run"] != dry || result["sender"] != "111@s.whatsapp.net" {
			t.Fatalf("result=%v", result)
		}
		if !dry && (result["receipt"] != "read-self" || result["sender_notified"] != false) {
			t.Fatalf("privacy result=%v", result)
		}
	}
}

func TestStatusSeenOldSyncError(t *testing.T) {
	skipPresenceDelegateSocketTestOnUnsupportedOS(t)
	dir := shortPresenceDelegateStoreDir(t)
	lk, err := lock.Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lk.Release()
	server := startPresenceDelegateTestSocket(t, dir, func(req sendDelegateRequest) sendDelegateResponse {
		return sendDelegateResponse{Error: fmt.Sprintf("unsupported send kind %q", req.Kind)}
	})
	defer server.stop()
	_, stderr, err := runPresenceDelegateHelper(t, []string{"--store", dir, "status", "seen", "--id", "status-id", "--dry-run"})
	if err == nil || !strings.Contains(stderr, "restart `wacli sync` after upgrading") {
		t.Fatalf("err=%v stderr=%s", err, stderr)
	}
	other := errors.New("offline")
	if explainStatusSeenDelegateError(other) != other || explainStatusSeenDelegateError(nil) != nil {
		t.Fatal("unrelated errors changed")
	}
}

func TestStatusSeenOutput(t *testing.T) {
	for _, senders := range [][]string{{"111@s.whatsapp.net"}, {"111@s.whatsapp.net", "222@lid"}} {
		var writeErr error
		stdout := captureRootStdout(t, func() {
			writeErr = writeStatusSeenResult(&rootFlags{asJSON: true}, &app.StatusSeenResult{IDs: []string{"a", "b"}, Senders: senders, Receipt: wa.ReadReceiptUnknown}, false)
		})
		if writeErr != nil {
			t.Fatal(writeErr)
		}
		var envelope struct {
			Data map[string]any `json:"data"`
		}
		if err := json.NewDecoder(strings.NewReader(stdout)).Decode(&envelope); err != nil {
			t.Fatal(err)
		}
		result := envelope.Data
		if result["sender_notified"] != nil || result["ok"] != true || result["dry_run"] != false || result["delegated"] != false {
			t.Fatalf("result=%v", result)
		}
		if len(senders) == 1 && (result["sender"] != senders[0] || result["senders"] != nil) {
			t.Fatalf("single poster=%v", result)
		}
		if len(senders) == 2 && (result["sender"] != nil || len(result["senders"].([]any)) != 2) {
			t.Fatalf("multiple posters=%v", result)
		}
	}
}

func TestStatusSeenHumanOutputIsOneLine(t *testing.T) {
	for _, dry := range []bool{false, true} {
		var err error
		stdout := captureRootStdout(t, func() {
			err = writeStatusSeenResult(&rootFlags{}, &app.StatusSeenResult{IDs: []string{"a", "b"}, Senders: []string{"111@s.whatsapp.net", "222@lid"}, Receipt: wa.ReadReceiptUnknown, DryRun: dry}, true)
		})
		if err != nil || strings.Count(stdout, "\n") != 1 || !strings.Contains(stdout, "111@s.whatsapp.net, 222@lid") || (dry && !strings.Contains(stdout, "would send")) {
			t.Fatalf("dry=%t output=%q error=%v", dry, stdout, err)
		}
	}
}

func TestStatusSeenUnknownIDRejectedBeforeOpeningSession(t *testing.T) {
	dir := t.TempDir()
	a := newStatusSeenFixture(t, dir)
	a.Close()
	_, stderr, err := runPresenceDelegateHelper(t, []string{"--store", dir, "status", "seen", "--id", "status-id", "--id", "missing"})
	if err == nil || !strings.Contains(stderr, "unknown status message ID") {
		t.Fatalf("error=%v stderr=%s", err, stderr)
	}
	if matches, err := filepath.Glob(filepath.Join(dir, "session.db*")); err != nil || len(matches) != 0 {
		t.Fatalf("invalid selection opened a session: files=%v error=%v", matches, err)
	}
}
