# Angular and Go public room chat

A learning chat application with an Angular 21 interface and an independently runnable Go service built with Gin. Godog scenarios exercise the real HTTP and WebSocket transport and run alongside ordinary Go tests.

Choose a display name, join General, Angular, or Go, and exchange messages with other guests in the same room. The interface shows connected participants, recent history, and connection status. It reconnects after temporary interruptions.

## Run locally

Requirements: the installed Node 24 and npm 11 toolchain, and Go 1.27.2 or later compatible with this module. Use the versions declared in `package.json` and `go.mod` rather than the older tutorial versions.

Use a checkout whose filesystem path has no colon (`:`). The installed Vite development server rejects such paths even when they are in its file allowlist. Tests and production builds still work from those paths, but the browser development server cannot load its modules.

For this local project, the verified development checkout is `/Users/geloes/Documents/Codex/2026-10-14/che/work/chat-mvp`. The original repository remains at `/Users/geloes/Angular:Go Project/chat-app`, with the MVP integrated on `master`. Both checkouts share the same Git history.

Install dependencies from the repository root:

```sh
npm ci
go mod download
```

Start the API in one terminal:

```sh
npm run start:api
```

Start Angular in a second terminal:

```sh
npm start
```

Open http://localhost:4200 in two browser tabs and join the same room using different display names. Send a message in either tab; it appears in both. Switch one tab to a different room to try room isolation.

The API binds to `127.0.0.1:8080`. Angular forwards `/api/**` and `/ws` to it through `proxy.conf.json`. If Angular is started before the API, the interface offers a retry when loading rooms fails.

The equivalent direct API command is `go run ./cmd/chat`. The health endpoint is http://127.0.0.1:8080/healthz.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `CHAT_ADDR` | `127.0.0.1:8080` | API listen address. |
| `CHAT_ALLOWED_ORIGINS` | `http://localhost:4200,http://127.0.0.1:4200` | Comma-separated browser origins allowed to open WebSockets. |

For example:

```sh
CHAT_ALLOWED_ORIGINS=http://localhost:4200 go run ./cmd/chat
```

If changing the API address, update the Angular proxy target too. Environment variables are supplied by the shell; no dotenv loader is required. Restart the Angular development server after changing its proxy configuration.

## Verify the application

```sh
npm test -- --watch=false
npm run build
npm run test:api
npm run test:bdd
```

`test:api` runs the entire Go suite with the race detector. `test:bdd` prints the Godog feature results. They can also be run directly:

```sh
go test -race ./...
go vet ./...
go test ./bdd -run TestFeatures -v
```

The frontend tests use Vitest through Angular's unit-test builder. The production build includes the browser bundle and existing Angular SSR host and prerenders the initial screen. The chat connection starts only in the browser.

## Develop a behavior with Godog

Start with the observable outcome in `features/chat.feature`, before changing application behavior. Use names and room actions that a user recognizes. For example:

```gherkin
Scenario: Guests exchange a message
  Given an isolated chat service
  And "Alice" connects to "general" as "alice"
  And "Bob" connects to "general" as "bob"
  When "alice" sends "Hello, Bob"
  Then "bob" receives "Hello, Bob" from "Alice"
```

Use the step wording already defined in `bdd/chat_test.go`; when introducing a new step, register a matching expression in the scenario initializer. Each scenario creates its own service and memory store, connects real WebSocket clients, and closes them after the scenario.

1. Describe the expected behavior in a Gherkin scenario.
2. Run `npm run test:bdd` and confirm the new behavior fails for the expected reason. An undefined step means its step definition still needs to be written.
3. Implement the smallest change in the domain or transport package that satisfies it.
4. Run the feature suite, then `npm run test:api` to catch regressions and concurrency issues.
5. Refactor while the scenarios remain green.

Use ordinary Go tests for focused boundaries such as Unicode length, bounded history, and slow-client queues. Use Godog for the complete user behavior through the public transport. Frontend state and interactions have separate Angular tests.

## Service boundaries

```text
Angular screen
  └─ Chat service: signals, HTTP discovery, WebSocket state and retries
       ├─ GET /api/rooms
       └─ /ws
            └─ Gin transport: validation, origin policy, socket lifecycle
                 └─ Chat domain: rooms, membership, messages and presence
                      └─ Repository interface: bounded memory history
```

`cmd/chat` owns process configuration and graceful shutdown. `internal/chat` owns the chat rules without Gin or WebSocket dependencies. `internal/transport` adapts those rules to Gin and Gorilla WebSocket. `bdd` verifies their behavior together.

Room registration, its initial history snapshot, and live message delivery are coordinated together. There is one reader and one writer per socket. Each client has a bounded outgoing queue; a slow client is disconnected instead of blocking the room.

## HTTP contract

| Route | Response |
| --- | --- |
| `GET /healthz` | `{"status":"ok"}` |
| `GET /api/rooms` | `{"rooms":[...]}` with `general`, `angular`, and `go`. |
| `GET /api/rooms/:roomId/messages` | `{"messages":[...]}` containing up to 100 messages in chronological order. |
| `GET /ws?room=general&name=Alice` | A WebSocket upgrade for an anonymous guest. |

HTTP failures return a JSON `code` and `message`. Invalid name/parameters receive `400`, unknown rooms receive `404`, and a supplied disallowed browser origin receives `403`.

Names are trimmed and limited to 1–32 Unicode characters. Message content is trimmed and limited to 1–1000 Unicode characters. Newlines are supported. Angular renders names and messages as escaped text.

## WebSocket contract

To send a message:

```json
{"type":"message.send","content":"Hello, room!"}
```

The first server event is `session.ready` with the session identity and current room snapshot:

```json
{
  "type": "session.ready",
  "sessionId": "server-generated-session-id",
  "room": {"id": "general", "name": "General", "description": "A place for everyone to chat."},
  "messages": [],
  "participants": [{"id": "server-generated-session-id", "name": "Alice"}]
}
```

Accepted messages are broadcast to the sender and other room members:

```json
{
  "type": "message.created",
  "message": {
    "id": "server-generated-message-id",
    "roomId": "general",
    "senderId": "server-generated-session-id",
    "senderName": "Alice",
    "content": "Hello, room!",
    "createdAt": "2026-10-14T06:00:00Z"
  }
}
```

Presence events have `type: "presence.updated"` and `participants`. Recoverable command errors have `type: "error"`, `code`, and `message`. Message IDs, sender identity, and UTC timestamps come from the server.

The client reconnects after 1, 2, 4, 8, and then at most 15 seconds. Leaving or switching rooms closes the previous connection and cancels its retries. A reconnection receives a fresh snapshot of the latest history. Sending is disabled while disconnected and drafts are preserved. Messages are not automatically resent after an uncertain disconnect.

## Current limits and future services

Display names belong to anonymous guests and are not verified accounts. Duplicate names are allowed; session IDs distinguish participants. Every room is public.

Each room keeps its latest 100 messages in memory. A service restart clears history. A disconnected guest returning after more than 100 newer messages sees the latest snapshot, not a complete archive. Run one chat instance for this version.

The next steps can build on the current boundaries:

- Add PostgreSQL behind the repository interface, migrations, and database-backed history tests.
- Add an identity service and replace anonymous handshake identity with verified credentials. Authorize both room entry and message actions.
- Add message events for a notification service. Define durable delivery, retries, and idempotency before relying on notifications.
- Share live message delivery through a broker before running multiple chat instances. Add persistence and missed-event recovery rather than relying only on transient broadcast.

The Angular development proxy is for local development. A deployed setup needs a reverse proxy that routes `/api` and WebSocket upgrades at `/ws` to Go and serves the Angular application from the same origin. Keep explicit allowed origins and use HTTPS/WSS in that setup.

## Design and references

The approved design is in `docs/superpowers/specs/2026-10-14-chat-mvp-design.md`, with the implementation plan in `docs/superpowers/plans/2026-10-14-chat-mvp.md`.

- [Angular and Go WebSocket tutorial](https://www.thepolyglotdeveloper.com/2016/12/create-real-time-chat-app-golang-angular-2-websockets/)
- [Gin tutorial](https://semaphore.io/community/tutorials/building-go-web-applications-and-microservices-using-gin)
- [Godog tutorial](https://semaphore.io/community/tutorials/how-to-use-godog-for-behavior-driven-development-in-go)
- [Current Godog documentation](https://github.com/cucumber/godog)
- [Current Gin documentation](https://gin-gonic.com/en/docs/)
- [Gorilla WebSocket concurrency and origin policy](https://pkg.go.dev/github.com/gorilla/websocket)
- [Angular development proxy](https://angular.dev/tools/cli/serve#proxying-to-a-backend-server)

The tutorials supply the core approach; this project uses current package APIs, Go modules, and Godog's `go test` integration.
