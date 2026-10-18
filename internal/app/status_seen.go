package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/openclaw/wacli/internal/wa"
	"go.mau.fi/whatsmeow/types"
)

type StatusSeenResult struct {
	IDs     []string          `json:"ids"`
	Senders []string          `json:"senders"`
	Receipt types.ReceiptType `json:"receipt"`
	DryRun  bool              `json:"dry_run"`
}

func (a *App) StatusSeen(ctx context.Context, ids []string, dryRun bool) (*StatusSeenResult, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("at least one --id is required")
	}
	result := &StatusSeenResult{Receipt: wa.ReadReceiptUnknown, DryRun: dryRun}
	var batches []readReceiptBatch
	senders := make(map[string]int)
	seen := make(map[string]bool)
	for _, id := range ids {
		if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
			return nil, fmt.Errorf("--id requires an exact nonempty status message ID")
		}
		if seen[id] {
			continue
		}
		m, err := a.db.GetStatusMessage(id)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("unknown status message ID %q; sync incoming statuses first", id)
		}
		if err != nil {
			return nil, fmt.Errorf("look up status %q: %w", id, err)
		}
		if m.FromMe {
			return nil, fmt.Errorf("status %q is your own status; only incoming statuses can be marked seen", id)
		}
		sender, err := types.ParseJID(m.SenderJID)
		if err != nil || sender.IsEmpty() || sender.User == "" || m.SenderJID != strings.TrimSpace(m.SenderJID) || (sender.Server != types.DefaultUserServer && sender.Server != types.HiddenUserServer) {
			return nil, fmt.Errorf("status %q has no valid poster JID; sync it before sending receipts", id)
		}
		key := sender.String()
		i, ok := senders[key]
		if !ok {
			i = len(batches)
			senders[key] = i
			batches = append(batches, readReceiptBatch{sender: sender})
			result.Senders = append(result.Senders, key)
		}
		batches[i].ids = append(batches[i].ids, types.MessageID(id))
		result.IDs = append(result.IDs, id)
		seen[id] = true
	}
	if dryRun {
		return result, nil
	}
	if a.opts.ReadOnly {
		return nil, fmt.Errorf("read-only mode: sending status seen receipts requires --dry-run")
	}
	result.Receipt = types.ReceiptTypeReadSelf
	sent := 0
	for _, batch := range batches {
		kind, err := a.wa.MarkRead(ctx, batch.ids, nowUTC(), types.StatusBroadcastJID, batch.sender, types.AddressingModeLID)
		if err != nil {
			return nil, fmt.Errorf("send status seen receipts for %s (%d already sent; retry may resend them): %w", batch.sender, sent, err)
		}
		if kind != types.ReceiptTypeReadSelf {
			result.Receipt = wa.ReadReceiptUnknown
		}
		sent += len(batch.ids)
	}
	return result, nil
}
