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
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
}

// Message
type Message struct {
	ID        int       `json:"id"`
	ChatRoom  string    `json:"chat_room"`
	Nickname  string    `json:"nickname"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// User
type User struct {
	Nickname string
	Password string
}

var currentUser User
var conn *websocket.Conn
var reader *bufio.Reader
var displayChan chan Message
var mu sync.Mutex

// Global values
var serverIP = "localhost"
var timestampFormat = "15:04"
var messagePrefix = ""
var prompt = "> "

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

		displayChan = make(chan Message, 10)
		go displayLoop()
		go readMessages()
		connectToRoom(config.StartRoom)

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
		room, _ := reader.ReadString('\n')
		room = strings.TrimSpace(room)

		displayChan = make(chan Message, 10)
		go displayLoop()
		go readMessages()
		connectToRoom(room)
	}

	chatLoop()
}

func loadConfig() (Config, error) {
	var config Config
	file, err := os.Open("config.json")
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

func connectToRoom(room string) {
	mu.Lock()
	if conn != nil {
		conn.Close()
	}

	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(currentUser.Nickname+":"+currentUser.Password))
	header := http.Header{
		"Authorization": {auth},
	}

	u := url.URL{Scheme: "ws", Host: serverIP, Path: "/ws/" + room}
	dialer := websocket.DefaultDialer
	var err error
	conn, _, err = dialer.Dial(u.String(), header)
	if err != nil {
		log.Fatalf("❌ Error while connecting to the server: %v", err)
	}

	fmt.Printf("\n✅ Joined room '%s' as %s\n", room, currentUser.Nickname)

	historyURL := fmt.Sprintf("http://%s/history/%s", serverIP, room)
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

	showPrompt()
}

func displayLoop() {
	for msg := range displayChan {
		displayMessage(msg, true)
		showPrompt()
	}
}

func displayMessage(msg Message, clearPrompt bool) {
	if clearPrompt {
		fmt.Print("\r\033[K")
	}
	fmt.Printf("%s[%s] %s: %s\n", messagePrefix, msg.Timestamp.Format(timestampFormat), msg.Nickname, msg.Content)
}

func showPrompt() {
	fmt.Print("\r\033[K")
	fmt.Print(prompt)
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

		var msg Message
		err := currentConn.ReadJSON(&msg)
		if err != nil {
			// If /room was used
			if websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway) || strings.Contains(err.Error(), "use of closed network connection") {
			} else {
				log.Println("❌ Error reading from server:", err)
			}
		}

		displayChan <- msg
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
			newRoom := strings.TrimSpace(strings.TrimPrefix(text, "/room "))
			if newRoom != "" {
				connectToRoom(newRoom)
			}
		} else if text != "" {
			fmt.Print("\033[1A\r\033[K")

			msg := Message{
				Content:   text,
				Nickname:  currentUser.Nickname,
				Timestamp: time.Now(),
			}

			mu.Lock()
			if conn != nil {
				if err := conn.WriteJSON(msg); err != nil {
					log.Println("❌ Error sending message:", err)
				}
			}
			mu.Unlock()

			fmt.Print(prompt)
		}
	}
}
