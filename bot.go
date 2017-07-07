package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync/atomic"

	"golang.org/x/net/websocket"
)

func main() {
	bot, err := New(os.Getenv("SLACK_API_TOKEN"))
	if err != nil {
		log.Fatal(err)
	}
	defer bot.Close()
	fmt.Println("^C exits")

	for {
		msg, err := bot.GetMessage()
		if err != nil {
			log.Printf("receive error, %v", err)
		}

		// 自分の発言は除外する
		if bot.ID == msg.SpeakerID() {
			continue
		}

		if msg.Type == "message" {
			target := regexp.MustCompile(`出勤`)
			if target.MatchString(msg.TextBody()) {
				go func(m ResponseMessage) {
					req := RequestMessage{
						Type:    m.Type,
						Channel: m.Channel,
						Text:    "出勤を検知",
					}
					bot.PostMessage(req)
				}(msg)
			}
		}
	}
}

// Slack と接続した Bot を新しく作る
func New(token string) (*Bot, error) {
	bot := Bot{
		Users:    map[string]string{},
		Channels: map[string]string{},
		Ims:      map[string]string{},
	}

	resp, err := bot.rtmStart(token)
	if err != nil {
		return nil, fmt.Errorf("api connection error, %v", err)
	}
	if !resp.OK {
		return nil, fmt.Errorf("connection error, %v", resp.Error)
	}

	if e := bot.dial(resp.URL); e != nil {
		return nil, e
	}

	// 接続した Bot の情報を保持しておく
	bot.ID = resp.Self.ID
	bot.Name = resp.Self.Name
	for _, u := range resp.Users {
		bot.Users[u.ID] = u.Name
	}
	for _, c := range resp.Channels {
		if c.IsMember {
			bot.Channels[c.ID] = c.Name
		}
	}
	for _, im := range resp.Ims {
		bot.Ims[im.ID] = im.UserID
	}
	return &bot, nil
}

// Slack と接続する Bot
type Bot struct {
	ID       string
	Name     string
	Users    map[string]string
	Channels map[string]string
	Ims      map[string]string
	socket   *websocket.Conn
	counter  uint64
}

// Slack と接続する
func (b Bot) rtmStart(token string) (*connectResponse, error) {
	q := url.Values{}
	q.Set("token", token)
	u := &url.URL{
		Scheme:   "https",
		Host:     "slack.com",
		Path:     "/api/rtm.start",
		RawQuery: q.Encode(),
	}
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with code %d", resp.StatusCode)
	}
	var body connectResponse
	dec := json.NewDecoder(resp.Body)
	if e := dec.Decode(&body); e != nil {
		return nil, fmt.Errorf("response decode error, %v", err)
	}
	return &body, nil
}

// Slack とつなげた時のレスポンス
type connectResponse struct {
	OK    bool                        `json:"ok"`
	Error string                      `json:"error"`
	URL   string                      `json:"url"`
	Self  struct{ ID, Name string }   `json:"self"`
	Users []struct{ ID, Name string } `json:"users"`
	Channels []struct {
		ID, Name string
		IsMember bool `json:"is_member"`
	} `json:"channels"`
	Ims []struct {
		ID     string
		UserID string `json:"user"`
	} `json:"ims"`
}

// Slack と WebSocket で接続する
func (b *Bot) dial(url string) error {
	ws, err := websocket.Dial(url, "", "https://api.slack.com/")
	if err != nil {
		return fmt.Errorf("dial error, %v", err)
	}
	b.socket = ws
	return nil
}

// 接続を切る
func (b *Bot) Close() error {
	return b.socket.Close()
}

// 発言を取得する
func (b *Bot) GetMessage() (ResponseMessage, error) {
	var msg ResponseMessage
	if e := websocket.JSON.Receive(b.socket, &msg); e != nil {
		return msg, e
	}
	return msg, nil
}

// 受信するメッセージ
type ResponseMessage struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Ts      string `json:"ts"`
	User    string `json:"user"`
	Text    string `json:"text"`
}

// メンションを取り除いた本文を取得する
func (m ResponseMessage) TextBody() string {
	matches := reMsg.FindStringSubmatch(m.Text)
	if len(matches) == 3 {
		return matches[2]
	}
	return ""
}

// 発言者の ID を取得する
func (m ResponseMessage) SpeakerID() string {
	matches := reMsg.FindStringSubmatch(m.Text)
	if len(matches) == 3 {
		return matches[1]
	}
	return ""
}

// ID 部分を取得するための正規表現
var (
	reMsg = regexp.MustCompile(`(?:<@(.+)>)?(?::?\s+)?(.*)`)
)

// 発言する
func (b *Bot) PostMessage(m RequestMessage) error {
	m.ID = atomic.AddUint64(&b.counter, 1)
	return websocket.JSON.Send(b.socket, m)
}

// 送信するメッセージ
type RequestMessage struct {
	ID      uint64 `json:"id"`
	Type    string `json:"type"`
	Channel string `json:"channel"`
	Text    string `json:"text"`
}
