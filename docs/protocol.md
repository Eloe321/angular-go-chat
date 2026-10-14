# HTTP and WebSocket reference

The Gin service exposes room discovery, bounded history, and a WebSocket session. All examples use the default local address.

## Configuration

| Variable               | Type                           | Default                                       | Effect                                   |
| ---------------------- | ------------------------------ | --------------------------------------------- | ---------------------------------------- |
| `CHAT_ADDR`            | Listen-address string          | `127.0.0.1:8080`                              | Address passed to the Go HTTP listener   |
| `CHAT_ALLOWED_ORIGINS` | Comma-separated origin strings | `http://localhost:4200,http://127.0.0.1:4200` | Replaces the allowed browser-origin list |

A missing or empty environment value uses its default. Origins are trimmed when loading configuration. Origin matching is exact, including scheme and port. Requests without an Origin header are allowed; a supplied Origin must be in the configured list.

If changing the API address, update `proxy.conf.json` too.

## HTTP routes

| Method and route                           | Result                                                       |
| ------------------------------------------ | ------------------------------------------------------------ |
| `GET /healthz`                             | `200` with `{"status":"ok"}`                                 |
| `GET /api/rooms`                           | `200` with `{"rooms":[...]}` in General, Angular, Go order   |
| `GET /api/rooms/:roomId/messages`          | `200` with `{"messages":[...]}`, oldest to newest, up to 100 |
| `GET /ws?room=<roomId>&name=<displayName>` | Validates the request and upgrades to a WebSocket            |

Room IDs are `general`, `angular`, and `go`. A room has string fields `id`, `name`, and `description`. A participant has string fields `id` and `name`. Empty history and participant collections are JSON arrays.

```sh
curl http://127.0.0.1:8080/api/rooms
curl http://127.0.0.1:8080/api/rooms/general/messages
```

### HTTP errors

HTTP errors have string `code` and `message` fields.

```json
{ "code": "unknown_room", "message": "Room does not exist." }
```

| Status     | Code                  | Trigger                                                  |
| ---------- | --------------------- | -------------------------------------------------------- |
| 400        | `invalid_room`        | Missing room parameter                                   |
| 400        | `invalid_name`        | Blank, invalid UTF-8, or overlong display name           |
| 404        | `unknown_room`        | Room ID is not in the catalog                            |
| 403        | `origin_not_allowed`  | Supplied browser Origin is not allowed                   |
| 400 or 426 | `invalid_handshake`   | Invalid WebSocket upgrade headers or version             |
| 503        | `service_unavailable` | Service is shutting down and refuses a valid new session |
| 500        | `storage_unavailable` | Repository cannot return history                         |
| 404        | `not_found`           | Route is not registered                                  |

The WebSocket HTTP request must use a valid upgrade handshake. A plain `curl /ws` request does not establish a session.

## Client command

Open a browser socket through the same origin, for example:

```javascript
const socket = new WebSocket('ws://localhost:4200/ws?room=general&name=Alice');
socket.addEventListener('message', (event) => console.log(JSON.parse(event.data)));
```

For HTTPS pages, use `wss:`. URL-encode query values.

Once `session.ready` arrives, send text JSON frames:

```json
{ "type": "message.send", "content": "Hello, room!" }
```

The client supplies only the command and content. Go creates sender identity, message IDs, and timestamps; additional client metadata does not override them.

## Server events

### session.ready

This is the first event for a registered session.

```json
{
  "type": "session.ready",
  "sessionId": "session-alice",
  "room": {
    "id": "general",
    "name": "General",
    "description": "A place for everyone to chat."
  },
  "messages": [],
  "participants": [{ "id": "session-alice", "name": "Alice" }]
}
```

`sessionId` is an opaque server-generated string. `messages` is a history snapshot and `participants` is the current membership snapshot. History and live publication are coordinated at registration.

### message.created

Every connected member of the room, including the sender, receives accepted messages.

```json
{
  "type": "message.created",
  "message": {
    "id": "message-1",
    "roomId": "general",
    "senderId": "session-alice",
    "senderName": "Alice",
    "content": "Hello, room!",
    "createdAt": "2026-10-14T06:00:00Z"
  }
}
```

All message fields are strings. `createdAt` uses UTC RFC3339 formatting, with fractional seconds when present. IDs are opaque; the example IDs above are illustrative.

### presence.updated

```json
{
  "type": "presence.updated",
  "participants": [{ "id": "session-alice", "name": "Alice" }]
}
```

Participants are sorted by name, with session ID breaking ties. Duplicate names are allowed.

### error

```json
{
  "type": "error",
  "code": "invalid_content",
  "message": "Message must contain 1 to 1000 valid Unicode characters."
}
```

Recoverable command errors keep the connection usable.

| Code                  | Trigger                                                                      |
| --------------------- | ---------------------------------------------------------------------------- |
| `invalid_command`     | Non-text frame, malformed/invalid UTF-8 JSON, or invalid command field types |
| `unsupported_type`    | Command type is not `message.send`                                           |
| `invalid_content`     | Blank, overlong, or invalid UTF-8 message content                            |
| `storage_unavailable` | Repository cannot append the accepted message                                |

An oversized frame closes the connection instead of producing a recoverable command error.

## Limits and lifecycle

| Setting                  | Value                                                                         |
| ------------------------ | ----------------------------------------------------------------------------- |
| Display name             | Trimmed; 1–32 Unicode code points; valid UTF-8                                |
| Message content          | Trimmed; 1–1000 Unicode code points; valid UTF-8; internal newlines preserved |
| History                  | Latest 100 messages per room; chronological; memory only                      |
| Outbound queue           | 64 events per client                                                          |
| Incoming frame limit     | 8 KiB                                                                         |
| Handshake timeout        | 5 seconds                                                                     |
| Socket write deadline    | 10 seconds                                                                    |
| Ping interval            | 25 seconds                                                                    |
| Pong timeout             | 60 seconds                                                                    |
| Process shutdown timeout | 5 seconds                                                                     |
| Client retry delay       | 1, 2, 4, 8, 15 seconds; subsequent attempts capped at 15                      |

Names are guest labels, not authenticated identities. Reconnecting creates a new session ID. Restarting Go clears history. A returning client receives the current bounded snapshot, not an unlimited archive.

## Related

- [Run and exercise the chat](tutorial-chat.md)
- [Development and configuration tasks](how-to-develop.md)
- [Why the protocol works this way](architecture.md)
- [Project overview](../README.md)
