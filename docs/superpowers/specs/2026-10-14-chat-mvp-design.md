# Angular and Go Chat MVP Design

Date: 2026-10-14

Build a working learning MVP in the existing Angular 21 and Go project. Users choose a display name, enter a public room, and exchange messages immediately. Gin provides the HTTP and WebSocket transport; Godog scenarios guide backend development. The initial backend is one independently runnable chat service with boundaries that support adding other services later.

## Scope and success criteria

The first version offers three fixed public rooms: General, Angular, and Go. Users can see room history, connected participants, and connection status, send plain-text messages, switch rooms, and reconnect after a temporary interruption. Messages and presence stay within their room. Two separate browser tabs must be able to demonstrate the complete flow.

Display names identify anonymous guests. They are not verified accounts and may be duplicated. Each connection receives a server-generated session ID. The server supplies message IDs, sender identity, and UTC timestamps; clients cannot choose another sender's ID.

Each room retains its latest 100 messages in memory. History survives browser reconnects while the service remains running and resets when the Go process restarts. Account registration, private conversations, file uploads, message editing, and distributed deployment belong to later iterations.

## Architecture and package boundaries

Keep the existing Angular project at the repository root and the existing Go module. Add `cmd/chat` as the service executable. Go packages under `internal/chat` own the room and message behavior, while `internal/transport` owns Gin routes and WebSocket connection handling. Keep the bounded memory store behind an interface that can later receive a PostgreSQL implementation. Do not expose Gin or WebSocket types in domain rules.

The chat service owns rooms, messages, and live connections. It runs independently from Angular. A later identity service can issue authenticated identities, and a later notification service can consume message events. Splitting live connections across multiple instances will require shared event delivery and an explicit persistence/delivery contract; the MVP runs one chat instance.

Angular uses standalone components and signals for rendered state. Keep protocol types and the chat connection service separate from the screen component. The Angular development proxy forwards `/api` and `/ws` to the Go service, including WebSocket upgrades. Browser code derives HTTP and WebSocket URLs from the current origin instead of embedding a backend hostname.

The existing SSR and prerender configuration remains valid. Browser-only connection initialization starts after the client renders. No WebSocket, storage, or location global is accessed while Angular renders on the server.

## HTTP and WebSocket contracts

The Go service listens on `127.0.0.1:8080` by default, with its address configurable through an environment variable.

| Route | Behavior |
| --- | --- |
| `GET /healthz` | Returns `200` and a JSON health status while the service is running. |
| `GET /api/rooms` | Returns the three rooms with stable IDs `general`, `angular`, and `go`, names, and descriptions. |
| `GET /api/rooms/:roomId/messages` | Returns the room's latest 100 messages in chronological order; an unknown room returns `404`. |
| `GET /ws?room=<roomId>&name=<displayName>` | Validates the guest and room, then upgrades the request to a WebSocket connection. |

Invalid names or missing room/name parameters receive `400` before upgrade. Unknown rooms receive `404`. Disallowed browser origins receive `403`. Errors use a consistent JSON code and readable message.

Names are trimmed and contain 1 to 32 Unicode characters. Message content is trimmed, must be valid UTF-8, and contains 1 to 1000 Unicode characters. Newlines inside a message remain valid. Render all names and content as escaped text in Angular.

The client sends a JSON object with `type: "message.send"` and `content`. Each persisted message contains `id`, `roomId`, `senderId`, `senderName`, `content`, and `createdAt`.

Server events use the following types:

| Event | Payload and purpose |
| --- | --- |
| `session.ready` | Session ID, selected room, current participants, and the latest history. This is the first event sent to a newly registered connection. |
| `message.created` | One accepted message, broadcast to all clients in the selected room, including the sender. |
| `presence.updated` | The current list of connected session IDs and display names in the room. |
| `error` | A stable error code and readable message for an invalid command. The connection remains usable for recoverable validation errors. |

Registration, initial history, and live publication are coordinated so a joining client cannot miss a message between its history snapshot and subsequent events. The REST history endpoint supports inspection; the Angular screen initializes from `session.ready` rather than racing a separate history request against the socket.

## Connection lifecycle and error handling

Use one WebSocket reader and one writer per connection. The writer owns data frames and periodic pings. Set read and write deadlines, a read limit of 8 KiB, a ping interval of 25 seconds, and a pong timeout of 60 seconds. Each client has a bounded outbound queue of 64 events. Disconnect a client whose queue fills so it cannot block other room members.

Gin handles health, room, and history routes plus WebSocket upgrade. Disable trusted proxies by default. Allow the configured frontend origins explicitly; development defaults include `http://localhost:4200`. An originless non-browser client may connect, but any supplied Origin must be allowed. The service supports graceful termination: stop accepting connections, close existing sockets, stop the room hub, and allow HTTP shutdown to finish.

The Angular service exposes disconnected, connecting, connected, and reconnecting states. Retry unexpected disconnects after 1, 2, 4, 8, and then 15 seconds, capped at 15 seconds. Explicitly leaving a room cancels retries and closes the previous socket. Switching rooms starts a fresh connection and replaces room state. Ignore callbacks from superseded sockets.

Sending is available only while connected. Preserve the user's draft during a disconnect, but do not silently queue or automatically resend messages. Only show an accepted message after `message.created`; a connection failure before acknowledgement leaves delivery uncertain and avoids accidental automatic duplicates. Reconnection replaces history with the server's latest bounded snapshot, deduplicated by message ID.

## Angular experience

Use a responsive chat screen with a room navigation area, a conversation area, a participant list, and a message composer. On narrow screens, place room selection and participant details above or alongside the conversation without obscuring the composer. Provide clear empty-room, loading, backend-unavailable, validation-error, and reconnecting states.

Use labeled inputs, visible keyboard focus, accessible button names, and a connection-status live region. Enter sends a message and Shift+Enter inserts a newline; do not submit during an input-method composition. Auto-scroll when the user is already near the newest messages, preserving their position when reading older messages. Display timestamps in the user's locale, with UTC retained in the protocol.

## Godog and verification

Use the current `github.com/cucumber/godog` package integrated with `go test`. Each scenario owns an isolated service and store, real HTTP requests, real WebSocket clients, bounded waits, and cleanup. Start with failing behavior scenarios, implement the smallest behavior needed, and refactor after they pass.

Required scenarios cover room listing, two guests exchanging a message, isolation between rooms, late-join history, invalid/blank messages, invalid names and unknown rooms, rejected origins, presence after disconnect, and reconnecting to retrieve missed messages. Add Go unit tests for message/name boundaries, bounded history, and slow-client behavior where these are clearer below the transport level. Run the backend suite with the race detector.

Angular tests cover connection transitions, retries and cancellation, stale-socket callbacks, snapshot/message handling, validation feedback, and disconnected composer behavior. The production build verifies that browser-only code remains compatible with SSR and prerendering. A two-tab browser check verifies the end-to-end chat flow and room isolation.

Completion requires passing `go test -race ./...`, `go vet ./...`, `npm test -- --watch=false`, and `npm run build`, plus the two-tab check. Update the README with local startup, the Godog workflow, protocol examples, memory-history limits, and the next service-extraction steps.

## References

- [Original Angular and Go WebSocket tutorial](https://www.thepolyglotdeveloper.com/2016/12/create-real-time-chat-app-golang-angular-2-websockets/)
- [Gin tutorial supplied by the user](https://semaphore.io/community/tutorials/building-go-web-applications-and-microservices-using-gin)
- [Godog tutorial supplied by the user](https://semaphore.io/community/tutorials/how-to-use-godog-for-behavior-driven-development-in-go)
- [Current Gin documentation](https://gin-gonic.com/en/docs/)
- [Current Godog documentation](https://github.com/cucumber/godog)
- [Gorilla WebSocket concurrency and origin requirements](https://pkg.go.dev/github.com/gorilla/websocket)
- [Angular server-compatible components](https://angular.dev/guide/ssr#authoring-server-compatible-components)

Use the supplied tutorials for their core communication and BDD patterns while following the installed Angular version and current library APIs.
