package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime" // <- 1. DODANO IMPORT
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/inconshreveable/go-update"
	"github.com/mattn/go-colorable"
)

type Colors struct {
	Nickname   string `json:"nickname"`
	Text       string `json:"text"`
	Date       string `json:"date"`
	Background string `json:"background"`
}

// Config
type Config struct {
	Nickname        string `json:"nickname"`
	Password        string `json:"password"`
	StartRoom       string `json:"start_room"`
	ServerIP        string `json:"server_ip"`
	MessagePrefix   string `json:"message_prefix"`
	TimestampFormat string `json:"timestamp_format"`
	Prompt          string `json:"prompt"`
	Socket          string `json:"websocket_port"`
	Colors          struct {
		User     Colors `json:"user"`
		Messages Colors `json:"messages"`
		System   Colors `json:"system"`
	} `json:"colors"`
}

// Message
type Message struct {
	ID        int       `json:"id"`
	ChatRoom  string    `json:"chat_room"`
	Nickname  string    `json:"nickname"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
}

// Github response
type ReleaseInfo struct {
	Tagname string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name        string `json:"name"`
	DownloadUrl string `json:"browser_download_url"`
}

// User
type User struct {
	Nickname string
	Password string
}

type UsersMessageColors struct {
	TextColor     string
	NicknameColor string
	DateColor     string
}

var out = colorable.NewColorableStdout()

var fgMap = map[string]string{
	"":              "",
	"black":         "\033[30m",
	"red":           "\033[31m",
	"green":         "\033[32m",
	"yellow":        "\033[33m",
	"blue":          "\033[34m",
	"magenta":       "\033[35m",
	"cyan":          "\033[36m",
	"white":         "\033[37m",
	"brightBlack":   "\033[90m",
	"brightRed":     "\033[91m",
	"brightGreen":   "\033[92m",
	"brightYellow":  "\033[93m",
	"brightBlue":    "\033[94m",
	"brightMagenta": "\033[95m",
	"brightCyan":    "\033[96m",
	"brightWhite":   "\033[97m",
}

var bgMap = map[string]string{
	"":              "",
	"black":         "\033[40m",
	"red":           "\033[41m",
	"green":         "\033[42m",
	"yellow":        "\033[43m",
	"blue":          "\033[44m",
	"magenta":       "\033[45m",
	"cyan":          "\033[46m",
	"white":         "\033[47m",
	"brightBlack":   "\033[100m",
	"brightRed":     "\033[101m",
	"brightGreen":   "\033[102m",
	"brightYellow":  "\033[103m",
	"brightBlue":    "\033[104m",
	"brightMagenta": "\033[105m",
	"brightCyan":    "\033[106m",
	"brightWhite":   "\033[107m",
}

var reset = "\033[0m"
var currentUser User
var conn *websocket.Conn
var reader *bufio.Reader
var displayChan chan Message
var mu sync.Mutex

// Global values
var chat_room = ""
var port = "8080"
var serverIP = "chat.astelta.world"
var timestampFormat = "15:04"
var messagePrefix = ""
var prompt = "> "
var appVersion = "v1.0.3"
var cfg Config

func main() {
	reader = bufio.NewReader(os.Stdin)

	config, err := loadConfig()
	if err == nil {
		cfg = config
		fmt.Println("✅ Successfully applied 'config.json'.")
		currentUser = User{Nickname: config.Nickname, Password: config.Password}

		if config.ServerIP != "" {
			serverIP = config.ServerIP
		}
		if config.TimestampFormat != "" {
			timestampFormat = config.TimestampFormat
		}
		if config.MessagePrefix != "" {
			messagePrefix = config.MessagePrefix
		}
		if config.Prompt != "" {
			prompt = config.Prompt
		}
		if config.Socket != "" {
			port = config.Socket
		}
		if config.StartRoom != "" {
			chat_room = config.StartRoom
		}

		displayChan = make(chan Message, 10)
		connectToRoom(chat_room)
		go readMessages()
		go displayLoop()
	} else {
		log.Println("❌ Error loading config.json:", err)
		log.Println("You will have to type out your data in order to log in.")

		fmt.Print("👤 Nickname: ")
		nick, _ := reader.ReadString('\n')
		nick = strings.TrimSpace(nick)

		fmt.Print("🔒 Password: ")
		pass, _ := reader.ReadString('\n')
		pass = strings.TrimSpace(pass)

		currentUser = User{Nickname: nick, Password: pass}

		fmt.Print("💬 Choose the starting room: ")
		chat_room, _ := reader.ReadString('\n')
		chat_room = strings.TrimSpace(chat_room)

		displayChan = make(chan Message, 10)
		connectToRoom(chat_room)
		go readMessages()
		go displayLoop()
	}
	startPingRoutine()
	chatLoop()
}

func DefaultConfig() Config {
	cfg := Config{
		Nickname:        "",
		Password:        "",
		StartRoom:       "General",
		ServerIP:        "chat.astelta.world",
		MessagePrefix:   "",
		TimestampFormat: "15:04",
		Prompt:          "> ",
		Socket:          "8080",
	}
	cfg.Colors.User = Colors{"blue", "", "green", ""}
	cfg.Colors.Messages = Colors{"yellow", "", "cyan", ""}
	cfg.Colors.System = Colors{"red", "brightCyan", "brightGreen", ""}
	return cfg
}

// default if empty
func mergeString(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

func MergeConfig(user, def Config) Config {
	user.Nickname = mergeString(user.Nickname, def.Nickname)
	user.Password = mergeString(user.Password, def.Password)
	user.StartRoom = mergeString(user.StartRoom, def.StartRoom)
	user.ServerIP = mergeString(user.ServerIP, def.ServerIP)
	user.MessagePrefix = mergeString(user.MessagePrefix, def.MessagePrefix)
	user.TimestampFormat = mergeString(user.TimestampFormat, def.TimestampFormat)
	user.Prompt = mergeString(user.Prompt, def.Prompt)
	user.Socket = mergeString(user.Socket, def.Socket)

	// users colors
	user.Colors.User.Nickname = mergeString(user.Colors.User.Nickname, def.Colors.User.Nickname)

	user.Colors.User.Text = mergeString(user.Colors.User.Text, def.Colors.User.Text)

	user.Colors.User.Date = mergeString(user.Colors.User.Date, def.Colors.User.Date)

	user.Colors.User.Background = mergeString(user.Colors.User.Background, def.Colors.User.Background)

	// messages
	user.Colors.Messages.Nickname = mergeString(user.Colors.Messages.Nickname, def.Colors.Messages.Nickname)

	user.Colors.Messages.Text = mergeString(user.Colors.Messages.Text, def.Colors.Messages.Text)

	user.Colors.Messages.Date = mergeString(user.Colors.Messages.Date, def.Colors.Messages.Date)

	user.Colors.Messages.Background = mergeString(user.Colors.Messages.Background, def.Colors.Messages.Background)

	// system
	user.Colors.System.Nickname = mergeString(user.Colors.System.Nickname, def.Colors.System.Nickname)

	user.Colors.System.Text = mergeString(user.Colors.System.Text, def.Colors.System.Text)

	user.Colors.System.Date = mergeString(user.Colors.System.Date, def.Colors.System.Date)

	user.Colors.System.Background = mergeString(user.Colors.System.Background, def.Colors.System.Background)

	return user
}

func loadConfig() (Config, error) {
	defaultCfg := DefaultConfig()

	exePath, err := os.Executable()
	if err != nil {
		return defaultCfg, fmt.Errorf("couldn't resolve exe path: %w", err)
	}
	exeDir := filepath.Dir(exePath)
	configPath := filepath.Join(exeDir, "config.json")

	file, err := os.Open(configPath)
	if err != nil {
		return defaultCfg, nil
	}
	defer file.Close()

	var userCfg Config
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&userCfg); err != nil {
		return defaultCfg, fmt.Errorf("error decoding config: %w", err)
	}

	finalCfg := MergeConfig(userCfg, defaultCfg)

	return finalCfg, nil
}

func showPrompt() {
	fmt.Print(prompt)
}

func connectToRoom(room string) {
	mu.Lock()
	if conn != nil {
		conn.Close()
	}
	chat_room = room
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(currentUser.Nickname+":"+currentUser.Password))
	header := http.Header{
		"Authorization": {auth},
	}

	u := url.URL{Scheme: "ws", Host: serverIP + ":" + port, Path: "/ws/" + room}
	dialer := websocket.DefaultDialer
	var err error
	conn, _, err = dialer.Dial(u.String(), header)
	if err != nil {
		log.Fatalf("❌ Error while connecting to the server: %v", err)
	}

	fmt.Printf("\n✅ Joined room '%s' as %s\n", room, currentUser.Nickname)
	mu.Unlock() // Odblokuj wcześniej, aby pobieranie historii nie blokowało innych operacji

	historyURL := fmt.Sprintf("http://%s:%s/history/%s", serverIP, port, room)
	req, err := http.NewRequest("GET", historyURL, nil)
	if err != nil {
		log.Println("Error making history request:", err)
		return
	}
	req.SetBasicAuth(currentUser.Nickname, currentUser.Password)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error downloading the history:", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var messages []Message
		if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
			log.Println("Error decoding history:", err)
			return
		}
		fmt.Println("📜 Room:")
		for _, msg := range messages {
			displayMessage(msg, false)
		}
		// mu.Unlock() // Przeniesione wyżej
	} else {
		log.Printf("Error downloading history: %s\n", resp.Status)
		// mu.Unlock() // Przeniesione wyżej
	}
}

func displayLoop() {
	for msg := range displayChan {
		displayMessage(msg, true)
	}
}

func displayMessage(msg Message, clearPrompt bool) {

	// 4. POPRAWIONO TĘ FUNKCJĘ
	// Używamy globalnej zmiennej 'reset' i usuwamy zbędne backslashe

	// timestamp (zostawiamy domyślny niebieski dla uproszczenia,
	// chyba że chcesz go dodać do configa)
	var ts string
	var nick string
	fmt.Fprint(out, "\r\033[K")
	if msg.Type == "system" {
		nick = fmt.Sprintf("%s%s%s%s",
			fgMap[cfg.Colors.System.Nickname],
			"System",
			bgMap[cfg.Colors.System.Background], // <- Poprawiono na System.Background
			reset)
		ts = fmt.Sprintf("%s(%s)%s",
			fgMap[cfg.Colors.System.Date],
			msg.Timestamp.Format(timestampFormat),
			reset) // <- Dodano reset
		fmt.Fprintf(out, "%s%s %s: %s%s\n", messagePrefix, ts, nick, fgMap[cfg.Colors.System.Text], msg.Content)

	} else if msg.Nickname == cfg.Nickname {
		// Wiadomość od bieżącego użytkownika
		nick = fmt.Sprintf("%s%s%s%s",
			fgMap[cfg.Colors.User.Nickname],
			msg.Nickname,
			bgMap[cfg.Colors.User.Background],
			reset) // <- Dodano reset
		ts = fmt.Sprintf("%s(%s)%s",
			fgMap[cfg.Colors.User.Date],
			msg.Timestamp.Format(timestampFormat),
			reset)
		fmt.Fprintf(out, "%s%s %s: %s%s\n", messagePrefix, ts, nick, fgMap[cfg.Colors.User.Text], msg.Content)

	} else {
		// Wiadomość innego użytkownika
		nick = fmt.Sprintf("%s%s%s%s", // <- Usunięto błędne '\\'
			fgMap[cfg.Colors.Messages.Nickname],
			msg.Nickname,
			bgMap[cfg.Colors.Messages.Background],
			reset) // <- Dodano reset
		ts = fmt.Sprintf("%s(%s)%s",
			fgMap[cfg.Colors.Messages.Date],
			msg.Timestamp.Format(timestampFormat),
			reset)
		fmt.Fprintf(out, "%s%s %s: %s%s\n", messagePrefix, ts, nick, fgMap[cfg.Colors.Messages.Text], msg.Content)
	}
	if clearPrompt {
		showPrompt()
	}
}

func startPingRoutine() {
	go func() {
		ticker := time.NewTicker(30 * time.Second) // ping co 30s
		defer ticker.Stop()

		for range ticker.C {
			mu.Lock()
			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
					log.Println("❌ Error sending ping:", err)
				}
			}
			mu.Unlock()
		}
	}()
}

func CheckForupdate() {
	req, err := http.NewRequest("GET", "https://api.github.com/repos/Astelta/parkchat-client/releases/latest", nil)
	if err != nil {
		log.Println("Error creating a web request:", err)
		return
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error fetching the update data:", err)
		return
	}
	defer resp.Body.Close()
	var values ReleaseInfo
	json.NewDecoder(resp.Body).Decode(&values)
	if values.Tagname == appVersion {
		fmt.Print("You have the latest version: ", values.Tagname)
	} else {
		fmt.Print("Your version is: ", appVersion, "\nThere is newer version you can upgrade to: ", values.Tagname, "\nDo you want to upgrade? (Y/N):\n")
		showPrompt()
		for {
			text, _ := reader.ReadString('\n')
			text = strings.TrimSpace(text)

			switch text {
			case "Y":
				i := 0
				for range values.Assets {
					if strings.Contains(values.Assets[i].Name, runtime.GOOS) {
						fmt.Print("Updating... \n")
						doUpdate(values.Assets[i].DownloadUrl)
						fmt.Print("Update complete! The app will close now.")
						time.Sleep(200)
						os.Exit(0)
					}
					i++
				}
				fmt.Print("I couldn't find a binary for your platform. Try compiling the source code from: https://github.com/Astelta/parkchat-client \n!")
				return
			case "N":
				fmt.Print("Sure thing boss!\n")
				showPrompt()
				return
			default:
				fmt.Print("Im not sure what are you trying to do...")
			}
		}
	}
}

func doUpdate(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	err = update.Apply(resp.Body, update.Options{OldSavePath: ""})
	if err != nil {
		// Log błędu, jeśli wystąpi
		log.Println("Update error:", err)
	}
	return err
}

func readMessages() {
	for {
		mu.Lock()
		if conn == nil {
			mu.Unlock()
			time.Sleep(100 * time.Millisecond)
			continue
		}
		currentConn := conn
		mu.Unlock()

		mt, message, err := currentConn.ReadMessage() // <- czytaj surowo
		if err != nil {
			if websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway) || strings.Contains(err.Error(), "use of closed network connection") {
				// Ciche wyjście, jeśli połączenie jest normalnie zamykane
			} else {
				log.Println("❌ Error reading from server:", err)
			}
			// Jeśli jest błąd, prawdopodobnie połączenie zostało zerwane,
			// pętla powinna kontynuować i czekać na ponowne połączenie (jeśli jest taka logika)
			// Na razie po prostu kontynuujemy, aby uniknąć spamowania logami
			time.Sleep(1 * time.Second) // Zapobiegaj szybkiemu pętleniu w razie błędu
			continue
		}

		switch mt {
		case websocket.TextMessage:
			var msg Message
			if err := json.Unmarshal(message, &msg); err != nil {
				log.Println("❌ Error decoding JSON:", err)
				continue
			}
			displayChan <- msg
		case websocket.PingMessage:
			// gorilla/websocket automatycznie odpowiada na Pingi Pongiem,
			// ale jeśli chcesz to zrobić ręcznie:
			mu.Lock()
			_ = currentConn.WriteMessage(websocket.PongMessage, nil)
			mu.Unlock()
		case websocket.PongMessage:
			// Otrzymano pong, można zresetować licznik timeoutu, jeśli jest
		}
	}
}

func chatLoop() {
	for {
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)

		if text == "/exit" {
			fmt.Println("👋 Logged out.")
			if conn != nil {
				conn.Close()
			}
			os.Exit(0)
		} else if strings.HasPrefix(text, "/room ") {
			fmt.Fprint(out, "\033[1A\r\033[K")
			newRoom := strings.TrimSpace(strings.TrimPrefix(text, "/room "))
			if newRoom != "" {
				connectToRoom(newRoom)
			}
		} else if text == "/update" {
			fmt.Fprint(out, "\033[1A\r\033[K")
			CheckForupdate()
		} else if text != "" {
			fmt.Fprint(out, "\033[1A\r\033[K")

			msg := Message{
				Content:   text,
				Nickname:  currentUser.Nickname,
				Timestamp: time.Now(),
				ChatRoom:  chat_room,
				Type:      "chat",
			}

			mu.Lock()
			if conn != nil {
				if err := conn.WriteJSON(msg); err != nil {
					log.Println("❌ Error sending message:", err)
				}
			}
			mu.Unlock()
		}
	}
}
