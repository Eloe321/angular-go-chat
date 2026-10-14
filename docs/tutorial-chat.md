# Try the chat in two tabs

Run the application, exchange your first message, then see how rooms and history work.

You need Git, Node.js 24, npm 11, and Go 1.27.2 or a later toolchain compatible with `go.mod`. Ports 4200 and 8080 should be available. Use a checkout path without a colon (`:`).

## 1. Get the project and install dependencies

```sh
git clone https://github.com/Eloe321/angular-go-chat.git
cd angular-go-chat
npm ci
go mod download
```

## 2. Start the application

In the first terminal, from the repository root:

```sh
npm run start:api
```

In a second terminal, from the same repository:

```sh
npm start
```

Open [localhost:4200](http://localhost:4200). You should see the guest name field and the General, Angular, and Go rooms. The API's health endpoint at [127.0.0.1:8080/healthz](http://127.0.0.1:8080/healthz) returns `{"status":"ok"}`.

## 3. Chat as Alice and Bob

1. In the first tab, enter **Alice** and join **General**.
2. Open a second tab at the same address, enter **Bob**, and join **General**.
3. Wait for the connection status to become **Connected**. Both guests appear in the participant list.
4. Send `Hello, Bob!` from Alice. Both tabs display the accepted message with Alice's name and a timestamp.
5. Reply from Bob. Use **Shift+Enter** to insert a newline and **Enter** to send.
6. Switch Bob to **Angular**, then send another message from Alice in General. Bob does not receive that room's live messages.
7. Return Bob to **General**. The recent message appears in the initial history snapshot.

![General room with Alice and Bob](images/chat-desktop.jpg)

## What you built

You have a browser client communicating with a Go chat service over WebSockets. Go assigns session and message IDs, stores accepted messages, and broadcasts only to the selected room. Angular receives an initial snapshot, then renders live message and presence events.

The latest 100 messages per room stay available while Go runs. Restarting Go clears them. Guest names are unverified and may be duplicated.

Read [Architecture and design decisions](architecture.md) to follow the message flow, [HTTP and WebSocket reference](protocol.md) to inspect the event payloads, or [How to develop and verify the chat](how-to-develop.md) to add behavior with Godog.
