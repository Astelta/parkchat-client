# 🗨️ parkchat-client

A lightweight terminal chat client written in Go, using WebSockets and HTTP for history fetching.
It connects to a chat server with authentication, supports multiple rooms, and allows customization via a configuration file.

---

## ✨ Features

* 🔑 User authentication via Basic Auth (`nickname:password`)
* 🌐 Multiple chat rooms with `/room <name>` command
* ⏳ Fetches room history when joining
* 🎨 Configurable settings:

  * nickname & password
  * default room
  * server address
  * timestamp format
  * message prefix
  * custom prompt symbol
* 📜 Message display with timestamps

---

## 📦 Installation

Clone the repository and build the client:

```bash
git clone https://github.com/Astelta/parkchat-client.git
cd parkchat-client
go build -o parkchat-client
```

Or download a binary from the release page: [release page](https://github.com/Astelta/parkchat-client/releases/).

Run the client:

```bash
./parkchat-client
```


---

## 👤 Registering a User

Before using the client, you must create an account on the server.
This is done with a `POST` request to the `/register` endpoint.

### [**Or via this site**](https://parkchat.astelta.world)

### Linux/macOS (using `curl`):

```bash
curl -X POST http://chat.astelta.world/register \
  -H "Content-Type: application/json" \
  -d '{"nickname":"YourNickname","password":"YourPassword"}'
```

### Windows (using PowerShell):

```powershell
Invoke-WebRequest -Uri "http://chat.astelta.world/register" `
  -Method POST `
  -Headers @{ "Content-Type" = "application/json" } `
  -Body '{"nickname":"YourNickname","password":"YourPassword"}'
```

---

## ⚙️ Configuration

You can provide a `config.json` file in the same directory.
Default configuration:

```json
{
  "nickname": "",
  "password": "",
  "start_room": "Ogolny",
  "server_ip": "chat.astelta.world",
  "message_prefix": "",
  "timestamp_format": "02/01 15:04",
  "prompt": ""
}
```

If no config file is found, the client will ask for credentials and room name interactively.

---

## 💻 Usage

* Type messages and press **Enter** to send
* Commands:

  * `/room <name>` → switch to another chat room
  * `/exit` → logout and quit the client

Example session:

```
✅ Joined room 'Ogolny' as Alice
📜 Room:
[08/09 14:30] Bob: Hello!
[08/09 14:31] Alice: Hi everyone!

> /room New
✅ Joined room 'New' as Alice
```

---

## 📝 Notes

* The client requires a running chat server that provides:

  * **WebSocket endpoint** at `/ws/{room}`
  * **HTTP history endpoint** at `/history/{room}`
  * **Registration endpoint** at `/register`
* Authentication is done via Basic Auth headers (`nickname:password`).
