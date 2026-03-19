# BackupManager — Web Layer

The web layer consists of the React SPA (`frontend/`) and the Go HTTP handlers (`internal/api/`). It is the only part of the system that deals with HTTP, cookies, and WebSockets.

---

## React App Structure

```
frontend/src/
├── main.tsx              Root render, React Router setup
├── App.tsx               Route definitions, auth guard
├── api/                  One module per REST resource
│   ├── client.ts         Base fetch wrapper (CSRF header injection)
│   ├── assistant.ts
│   ├── audit.ts
│   ├── dashboard.ts
│   ├── docs.ts
│   ├── jobs.ts
│   ├── recovery.ts
│   ├── servers.ts
│   ├── settings.ts
│   └── snapshots.ts
├── components/           Shared UI components
│   ├── Layout.tsx         Sidebar + main content wrapper
│   ├── Sidebar.tsx        Navigation with route links
│   ├── AddServerWizard.tsx Multi-step server creation
│   ├── CreateJobModal.tsx  Backup job creation dialog
│   ├── JobCard.tsx         Job summary card
│   ├── ServerCard.tsx      Server list card
│   ├── ServerStatusCard.tsx Health status display
│   ├── SnapshotCalendar.tsx Calendar heat-map of backup activity
│   ├── SnapshotDetail.tsx   Single snapshot inspector
│   ├── SnapshotFilters.tsx  Filter bar for snapshots list
│   ├── BackupTimeline.tsx   Run history timeline
│   ├── RunHistory.tsx       Paginated run list
│   ├── ScheduleSelector.tsx Cron expression builder
│   ├── DiskUsageChart.tsx   Recharts bar chart
│   ├── AlertsList.tsx       Active alerts panel
│   ├── ChatMessage.tsx      LLM conversation bubble
│   └── PlaybookWizard.tsx   Recovery playbook generator UI
├── hooks/
│   ├── useAuth.ts         Login / logout state + localStorage persistence
│   └── useWebSocket.ts    WebSocket with exponential-backoff reconnection
├── pages/
│   ├── LoginPage.tsx
│   ├── DashboardPage.tsx
│   ├── ServersPage.tsx
│   ├── ServerDetailPage.tsx
│   ├── JobsPage.tsx
│   ├── SnapshotsPage.tsx
│   ├── RecoveryPage.tsx
│   ├── AssistantPage.tsx
│   ├── AuditLogPage.tsx
│   ├── DocsPage.tsx
│   └── SettingsPage.tsx
└── types/index.ts         Shared TypeScript interfaces
```

---

## Authentication Flow

BackupManager uses **JWT tokens stored in httpOnly cookies**. This pattern prevents JavaScript from accessing the token (XSS-safe) while the browser sends it automatically on every request.

### Login

```
Client                              Server
  │                                   │
  │  POST /api/auth/login             │
  │  { "username": "…", "password": "…" }
  │ ─────────────────────────────────>│
  │                                   │  1. bcrypt.Compare(password, hash)
  │                                   │  2. JWT.GenerateToken(userID, username, isAdmin)
  │                                   │     expires: 24h, signed with HS256
  │  200 OK                           │
  │  Set-Cookie: token=<jwt>; HttpOnly; SameSite=Strict; Path=/
  │  Set-Cookie: csrf_token=<hex>; SameSite=Strict; Path=/
  │  { "id": 1, "username": "…", "is_admin": true }
  │ <─────────────────────────────────│
  │                                   │
  │  (client stores user in           │
  │   localStorage for UI display,    │
  │   NOT the token)                  │
```

### Automatic Token Refresh

The `RefreshMiddleware` (`internal/auth/middleware.go`) intercepts every response. If the JWT has less than 1 hour remaining, a new token is issued and the cookie is updated transparently. The client never sees this.

### Logout

`POST /api/auth/logout` clears the `token` cookie by setting `MaxAge=-1`. The frontend removes the `bm_user` localStorage entry.

### Auth Middleware (`RequireAuth`)

Reads the `token` httpOnly cookie, validates the JWT signature and expiry, and attaches the parsed `Claims` to the request context. Returns `401` if missing or invalid.

```go
// internal/auth/middleware.go
func (s *Service) RequireAuth(next http.Handler) http.Handler {
    // reads r.Cookie("token"), calls ValidateToken, stores claims in ctx
}
```

Protected routes are wrapped:

```go
mux.Handle("/api/servers", authSvc.RequireAuth(protected))
```

---

## CSRF Protection

BackupManager implements the **double-submit cookie** pattern via `CSRFMiddleware` in `internal/api/csrf.go`.

### How it works

1. On **every response**, the server sets a `csrf_token` cookie (not httpOnly, so JavaScript can read it):
   ```
   Set-Cookie: csrf_token=<32-byte-hex>; SameSite=Strict; Path=/; MaxAge=86400
   ```

2. For **state-changing requests** (POST, PUT, DELETE), the client must echo the token in the `X-CSRF-Token` request header.

3. The middleware reads both values and rejects (403) if they do not match.

4. **`/api/auth/login` is exempt** — no cookie exists on the first visit.

5. **GET, HEAD, OPTIONS are exempt** — read-only.

### Frontend implementation

The `api/client.ts` fetch wrapper reads `document.cookie` to extract `csrf_token` and adds it to every mutating request:

```typescript
// api/client.ts (pattern)
const csrfToken = getCookie('csrf_token');
headers['X-CSRF-Token'] = csrfToken;
```

---

## WebSocket Protocol

Two endpoints are available:

| Path | Purpose |
|---|---|
| `/ws/logs` | Real-time backup run log streaming |
| `/ws/status` | Server health and job status updates |

### Connection and Authentication

WebSocket connections are not behind the `RequireAuth` middleware. Instead, `Hub.HandleWebSocket` reads the `token` httpOnly cookie directly before accepting the upgrade (browsers send cookies automatically on WebSocket handshake).

```go
cookie, err := r.Cookie("token")
if _, err := h.authSvc.ValidateToken(cookie.Value); err != nil {
    http.Error(w, "invalid token", http.StatusUnauthorized)
    return
}
```

### Message Format

All messages are JSON:

```json
{
  "type": "log",
  "server_id": 42,
  "data": { ... },
  "timestamp": "2026-03-19T14:00:00Z"
}
```

| `type` value | Meaning |
|---|---|
| `log` | A line of backup run output |
| `status` | A server or job status change |
| `health` | A health check result update |

### Keepalive

The server sends a **ping** every 30 seconds (`pingPeriod`). Clients that do not respond with a pong within 10 seconds (`pongWait`) are disconnected and cleaned up. The server's write timeout is also 10 seconds (`writeWait`).

### Reconnection (client-side)

`useWebSocket.ts` implements **exponential backoff** reconnection:
- Initial delay: 1 s
- Each failure doubles the delay
- Maximum delay: 30 s
- On successful reconnect the delay resets to 1 s

```typescript
ws.onclose = () => {
  const delay = Math.min(reconnectDelay.current, 30000);
  reconnectDelay.current = delay * 2;
  reconnectTimer.current = setTimeout(connect, delay);
};
```

---

## Frontend Tech Stack

| Package | Version | Role |
|---|---|---|
| React | 19 | UI framework |
| React Router DOM | 7 | Client-side routing |
| TanStack Query | 5 | Server-state caching, background refetch, automatic retry |
| Tailwind CSS | 4 | Utility-first styling via Vite plugin (no PostCSS config needed) |
| Recharts | 3 | Declarative charts (DiskUsageChart, backup timeline) |
| Lucide React | latest | Icon set |
| Vite | 5 | Build tool with HMR, TypeScript support, asset fingerprinting |
| TypeScript | 5.9 | Static typing |

### Build and Embedding

```
npm ci && npm run build
```

Vite outputs to `frontend/dist/`. The Makefile `embed-frontend` target copies this to `cmd/server/static/`. The Go binary embeds this directory via `//go:embed static`. At runtime, the SPA is served from memory — no separate web server needed.

### SPA Routing

The Go server intercepts all non-asset paths (no dot in the path) and rewrites to `index.html`, enabling React Router to handle client-side navigation:

```go
// cmd/server/main.go
if path != "/" && !strings.Contains(path, ".") {
    r2.URL.Path = "/"
    fileServer.ServeHTTP(w, r2)
    return
}
```

---

## Rate Limiting

Login is protected by a per-IP rate limiter (`internal/api/ratelimit.go`). Configuration is stored in the `settings` table (`login_rate_limit_requests` and `login_rate_limit_window_seconds`). Exceeding the limit returns `429 Too Many Requests`.

---

## API Response Format

All API responses use a consistent envelope defined in `internal/api/response.go`:

**Success:**
```json
{ "data": { ... } }
```

**Error:**
```json
{ "error": "descriptive message" }
```

Helper functions:
- `JSON(w, statusCode, payload)` — marshals any value as `{"data": payload}`
- `Error(w, statusCode, message)` — marshals `{"error": message}`
