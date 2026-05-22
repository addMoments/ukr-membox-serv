# Source Code Recovery Log

## Context

Last git commit: `e325b51` — Wed Apr 8 15:38:22 2026 +0300

Source code was lost after this commit. Recovery was done by parsing Claude Code session logs (JSONL files) to find edits that were made but never committed.

---

## Lost Changes (post-commit, not in codebase)

### 1. `src/routes/download.go` — New file

**What it does:** Backend proxy for S3 file downloads. Fixes a CORS issue where S3 with `AllowedOrigins: ["*"]` doesn't return CORS headers when an `Authorization` header is present. Instead of fetching S3 directly from the browser, the frontend calls `/api/download?url=...` and the Go server fetches the file server-side and pipes it back.

**Root cause of the bug:** S3 anonymous GET requests can't use response-specific headers (`response-content-disposition`), and authenticated S3 URLs with CORS config `AllowedOrigins: ["*"]` intermittently drop CORS headers in production.

**Handler logic:**
1. Read `?url=` query param — 400 if missing
2. Decode the S3 URL to an internal path via `s3wrap.Public_s3.Decode_url()`  — 400 if invalid
3. Set `Content-Disposition: attachment; filename="<basename>"`
4. Stream the file back via `s3wrap.Public_s3.Serve()`

---

### 2. `main.go` — Route registration

Added one line to register the download proxy route:

```go
apiRoutes.HandleFunc("/download", routes.DownloadProxy).Methods("GET")
```

Inserted before the `/form/{formName}` route.

---

## Changes Already in Codebase (from older sessions, already committed)

These were found in the logs but were already present — no action needed:

| File | Change |
|------|--------|
| `src/routes/orders.go` | `AdminCheck` refactored to manually parse token instead of relying on auth middleware — allows unauthenticated callers to get `{"is_admin":false}` instead of a redirect |
| `main.go` | `/admin/check` route changed from `auth.AuthMiddleware(routes.OrderRoutes.AdminCheck, "auth")` to `routes.OrderRoutes.AdminCheck` (no auth wrapper) |
| `src/routes/qr.go` | Removed `Has_feature` / `has_qr` gate — QR customization no longer requires a purchased feature |
| `src/send_email/html.go` | Removed broken logo image table and unused `.maillogo` CSS from email template |
| `src/routes/form.go` | Removed hardcoded `"aasillisppb@gmail.com"` recipient; removed the entire newsletter email send block; removed unused `io` and `sendemail` imports |
| `src/routes/collab.go` | Removed `"aasillisppb@gmail.com"` from collaborator invite email recipients (was CC'd on every invite) |

---

## Recovery Steps

1. Located session log files in `~/.claude/projects/`
2. Read `only_changes.txt` (frontend project logs) and `only_changes_secondary.txt` (backend project logs) line by line
3. Parsed each JSONL line to find `toolUseResult` entries with `filePath` ending in `.go` — these are confirmed-applied edits
4. Identified which session each edit came from and cross-checked against the last commit date
5. Verified each change against the current codebase (`grep` / file reads)
6. Applied the 2 missing changes manually
7. Ran `go build ./...` — clean build confirmed
