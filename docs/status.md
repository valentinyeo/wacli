# status

Read when: marking incoming WhatsApp status updates as seen.

```bash
wacli status seen --id STATUS_MESSAGE_ID [--id ANOTHER_ID] [--dry-run]
wacli --json status seen --id STATUS_MESSAGE_ID --dry-run
```

IDs must already be synced into `status_messages`. Unknown IDs, your own statuses,
and missing poster identities reject the entire selection before any receipt is sent.
Repeated IDs are sent once. Receipts are batched by poster, addressed to
`status@broadcast` with the poster as participant, and follow your account's
read-receipt privacy setting. This does not mark an ordinary chat as read.

When `sync --follow` owns the store lock, both real sends and dry-runs run through
that process. Restart `wacli sync` after upgrading if it does not support this command.
`--dry-run` resolves IDs and posters without connecting or sending receipts and is
allowed with `--read-only` or `WACLI_READONLY=1`; real sends are rejected in read-only mode.

The JSON envelope’s `data` is an object with `ok`, `ids`, `sender` (one poster) or `senders`
(multiple posters), `receipt`, `sender_notified`, `dry_run`, and `delegated`.
`receipt` is `read-self` when sender-visible receipts are disabled, otherwise
`unknown` because WhatsApp can apply a newer privacy setting during dispatch.
`sender_notified` is `false` for `read-self`, otherwise `null`; successful dispatch
is not proof of notification. Dry-run reports `receipt: "unknown"` and
`sender_notified: null` because no receipt was dispatched. `delegated: true` means
the running sync executed the request. Human output is one line.
