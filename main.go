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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/inconshreveable/go-update"
	"github.com/mattn/go-colorable"
)

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

var out = colorable.NewColorableStdout()
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
var appVersion = "v1.0.2"

func main() {

	reader = bufio.NewReader(os.Stdin)

	config, err := loadConfig()
	if err == nil {
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

func loadConfig() (Config, error) {
	var config Config
	file, err := os.Open("./config.json")
	if err != nil {
		return config, fmt.Errorf("couldn't open the config: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	if err != nil {
		return config, fmt.Errorf("error decoding config: %w", err)
	}
	return config, nil
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
		mu.Unlock()
	} else {
		log.Printf("Error downloading history: %s\n", resp.Status)
	}
}

func displayLoop() {
	for msg := range displayChan {
		displayMessage(msg, true)
	}
}

func displayMessage(msg Message, clearPrompt bool) {

	// timestamp w niebieskim
	ts := fmt.Sprintf("\033[34m%s\033[0m", msg.Timestamp.Format(timestampFormat))
	// nickname w zielonym
	nick := fmt.Sprintf("\033[32m%s\033[0m", msg.Nickname)

	fmt.Fprintf(out, "%s[%s] %s: %s\n", messagePrefix, ts, nick, msg.Content)
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
					} else {
						i++
					}
				}
				fmt.Print("I couldn't find a binary for your platform. Try compiling the source code from: https://github.com/Astelta/parkchat-client \n!")
				return
			case "N":
				fmt.Print("Sure thing boss! \n")
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
			} else {
				log.Println("❌ Error reading from server:", err)
			}
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
			// automatycznie odpowiada pong
			mu.Lock()
			_ = currentConn.WriteMessage(websocket.PongMessage, nil)
			mu.Unlock()
		case websocket.PongMessage:
			// możesz obsłużyć jeśli chcesz logować
		}
	}
}

func chatLoop() {
	for {
		text, _ := reader.ReadString('\n')
		text = strings.TrimSpace(text)

		if text == "/exit" {
			fmt.Println("👋 Logged out.")
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
