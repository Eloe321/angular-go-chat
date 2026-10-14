# How to develop and verify the chat

Use these tasks after you have run the application using the [two-tab tutorial](tutorial-chat.md). All commands run from the repository root.

## Run the checks

```sh
npm test -- --watch=false
npm run test:bdd
go test -race ./...
go vet ./...
npm run build
```

- Angular tests exercise the connection service, retries, teardown, snapshots, rendering, and keyboard input without a live API.
- Godog starts an isolated HTTP server and connects real WebSocket clients for each scenario. It needs no separately running Go service or Godog CLI installation.
- Go race tests include the behavior suite and focused domain, transport, and process tests.
- The build creates browser and Node SSR bundles and prerenders the initial route.

The existing tests and browser checks are recorded in the [README](../README.md#verification). The commands above verify automated behavior; browser interactions still need an actual browser.

## Develop behavior with Godog

1. Describe the expected outcome in [features/chat.feature](../features/chat.feature). Keep the scenario about an observable user action.
2. Run `npm run test:bdd`. Confirm the new scenario fails for the intended missing behavior. An undefined step means you need to register its definition first.
3. Implement the smallest change that passes the scenario, then run the focused unit tests and full Go race suite.
4. Refactor while the checks stay green. Update the protocol reference if you change the public contract.

This example uses existing step definitions and can be added directly:

```gherkin
Scenario: A guest sees the accepted version of their message
  Given an isolated chat service
  And "Alice" connects to "general" as "alice"
  When "alice" sends "  Hello from Go  "
  Then "alice" receives "Hello from Go" from "Alice"
```

The definitions live in [bdd/chat_test.go](../bdd/chat_test.go). The test suite uses `TestingT: t`, `Strict: true`, and `Paths: []string{"../features"}`. Its cleanup closes clients and the test server after each scenario.

For a new step, register a matching pattern in the suite's `ScenarioInitializer` and implement a Go function that returns an error when the expected outcome is absent. Use deadlines for socket reads so a failed expectation terminates promptly.

## Choose the right regression test

Use a Godog scenario for externally visible behavior, such as room isolation or recovering history after reconnecting.

Use domain tests for Unicode limits, history retention, ordering, and full client queues. Transport tests cover malformed commands, origins, oversize frames, and handshake/shutdown races. Frontend service tests use controllable sockets and timers; component tests exercise rendered controls and input events.

The [architecture guide](architecture.md) explains the invariants these tests protect. Avoid changing the event vocabulary in one layer without updating Go, TypeScript models, scenarios, and the [protocol reference](protocol.md).

## Change local ports and origins

To move Go to port 8081:

```sh
CHAT_ADDR=127.0.0.1:8081 npm run start:api
```

Set both `target` values in [proxy.conf.json](../proxy.conf.json) to `http://127.0.0.1:8081`, then restart Angular. Keep `"ws": true` on the WebSocket route.

To keep Go on its default port and move Angular to port 4201, start Go with the matching browser origins:

```sh
CHAT_ALLOWED_ORIGINS=http://localhost:4201,http://127.0.0.1:4201 npm run start:api
```

In a separate terminal:

```sh
npm start -- --port 4201
```

Open `http://localhost:4201`. Origins match exactly, including scheme, hostname, and port. Stop your previous development process before starting another on the same port.

All backend variables and defaults are listed in the [configuration reference](protocol.md#configuration).

## Inspect the API directly

```sh
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/api/rooms
curl http://127.0.0.1:8080/api/rooms/general/messages
```

The HTTP history endpoint is useful for inspection. The Angular conversation initializes from `session.ready` so it has an atomic boundary between history and live events.

## Build the application shell

```sh
npm run build
npm run serve:ssr:chat-app
```

The Node SSR host listens on port 4000 by default; `PORT` can override it. It renders the initial Angular screen and serves built assets. Live chat initializes in the browser.

A working deployment also needs Go and a reverse proxy that routes `/api` to Go and forwards WebSocket upgrades on `/ws`. Use HTTPS/WSS and set `CHAT_ALLOWED_ORIGINS` to the application's public origin. The Angular development proxy is not part of the production output. Deployment configuration is a future addition.

## Troubleshoot development

| Symptom                                              | Check                                                                                           |
| ---------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Room loading fails or chat keeps retrying            | Confirm Go is running and `/healthz` responds. Check both development proxy targets.            |
| WebSocket upgrade returns 403                        | Set the exact browser origin in `CHAT_ALLOWED_ORIGINS` and restart Go.                          |
| A port is already in use                             | Stop the previous process you started, or choose a new port and update proxy/origins as needed. |
| Vite rejects local modules with 403                  | Use a checkout path without a colon (`:`).                                                      |
| History disappears after a Go restart                | Expected: this version stores only the latest 100 messages per room in memory.                  |
| Two guests have the same name                        | Expected: names are anonymous labels; separate server session IDs identify their connections.   |
| Send is unavailable after reconnect starts           | Wait for `session.ready`. An open socket alone does not establish a ready chat session.         |
| A message's delivery is uncertain after interruption | Check recovered history. The client never automatically resends submitted content.              |

## Related

- [Try the chat in two tabs](tutorial-chat.md)
- [Architecture and design decisions](architecture.md)
- [HTTP and WebSocket reference](protocol.md)
- [Project overview](../README.md)
