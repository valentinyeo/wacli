package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/openclaw/wacli/internal/app"
	"github.com/openclaw/wacli/internal/out"
	"github.com/spf13/cobra"
)

const statusSeenKind = "status_seen"

func newStatusCmd(flags *rootFlags) *cobra.Command {
	cmd := &cobra.Command{Use: "status", Short: "Manage incoming WhatsApp status updates"}
	var ids []string
	var dryRun bool
	seen := &cobra.Command{
		Use:   "seen",
		Short: "Send privacy-aware seen receipts for incoming status updates",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(ids) == 0 {
				return fmt.Errorf("at least one --id is required")
			}
			if !dryRun {
				if err := flags.requireWritable(); err != nil {
					return err
				}
			}
			ctx, cancel := withTimeout(context.Background(), flags)
			defer cancel()
			openFlags := *flags
			if dryRun {
				openFlags.readOnly = true
			}
			a, lk, err := newApp(ctx, &openFlags, true, false)
			if err != nil {
				resp, delegated, delegateErr := tryDelegateSend(ctx, flags, err, sendDelegateRequest{Kind: statusSeenKind, IDs: ids, DryRun: dryRun})
				if !delegated {
					return delegateErr
				}
				if delegateErr != nil {
					return explainStatusSeenDelegateError(delegateErr)
				}
				if resp.StatusSeen == nil || resp.StatusSeen.DryRun != dryRun {
					return fmt.Errorf("the running sync process did not report status seen results; restart `wacli sync` after upgrading")
				}
				return writeStatusSeenResult(flags, resp.StatusSeen, true)
			}
			defer closeApp(a, lk)
			result, err := a.StatusSeen(ctx, ids, true)
			if err != nil {
				return err
			}
			if !dryRun {
				if err := a.EnsureAuthed(ctx); err != nil {
					return err
				}
				if err := a.Connect(ctx, false, nil); err != nil {
					return err
				}
				result, err = a.StatusSeen(ctx, ids, false)
				if err != nil {
					return err
				}
			}
			return writeStatusSeenResult(flags, result, false)
		},
	}
	seen.Flags().StringArrayVar(&ids, "id", nil, "exact stored incoming status message ID (repeat for multiple statuses)")
	seen.Flags().BoolVar(&dryRun, "dry-run", false, "resolve IDs and posters without sending receipts; probe sync delegation when running")
	cmd.AddCommand(seen)
	return cmd
}

func explainStatusSeenDelegateError(err error) error {
	if err == nil || !strings.Contains(err.Error(), "unsupported send kind") || !strings.Contains(err.Error(), statusSeenKind) {
		return err
	}
	return fmt.Errorf("the running sync process does not support status seen; restart `wacli sync` after upgrading, then run this again: %w", err)
}

func writeStatusSeenResult(flags *rootFlags, result *app.StatusSeenResult, delegated bool) error {
	receipts := chatStateReceipts{kind: string(result.Receipt)}
	if flags.asJSON {
		data := map[string]any{
			"ok":              true,
			"ids":             result.IDs,
			"receipt":         result.Receipt,
			"sender_notified": receipts.senderNotified(),
			"dry_run":         result.DryRun,
			"delegated":       delegated,
		}
		if len(result.Senders) == 1 {
			data["sender"] = result.Senders[0]
		} else {
			data["senders"] = result.Senders
		}
		return out.WriteJSON(os.Stdout, data)
	}
	action := "sent"
	if result.DryRun {
		action = "would send"
	}
	fmt.Fprintf(os.Stdout, "status seen: %s receipts for %s from %s (receipt: %s, delegated: %t)\n", action, strings.Join(result.IDs, ", "), strings.Join(result.Senders, ", "), result.Receipt, delegated)
	return nil
}
