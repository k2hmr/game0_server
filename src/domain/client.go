package domain

import (
	"log"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"encoding/json"
	"net/http"
)

type Client struct {
	ws     *websocket.Conn
	wsServer *Hub
	sendCh chan []byte
	Rooms    map[*Room]bool
	ID       uuid.UUID `json:"id"`
	Name     string `json:"name"`
}

func NewClient(ws *websocket.Conn, name string) *Client {
	return &Client{
		ID:       uuid.New(),
		ws:     ws,
		sendCh: make(chan []byte),
		Rooms:    make(map[*Room]bool),
		Name:     name,
	}
}

func (client *Client) GetName() string {
	return client.Name
}

func (c *Client) ReadLoop(broadCast chan<- []byte, unregister chan<- *Client) {
	defer func() {
		c.disconnect(unregister)
	}()

	for {
		_, jsonMsg, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("unexpected close error: %v", err)
			}
			break
		}

		// broadCast <- jsonMsg
		c.handleNewMessage(jsonMsg)
	}

}

func (c *Client) WriteLoop() {
	defer func() {
		c.ws.Close()
	}()

	for {
		message := <-c.sendCh

		w, err := c.ws.NextWriter(websocket.TextMessage)
		if err != nil {
			return
		}
		w.Write(message)

		for i := 0; i < len(c.sendCh); i++ {
			w.Write(<-c.sendCh)
		}

		if err := w.Close(); err != nil {
			return
		}
	}
}

func (client *Client) handleJoinRoomMessage(message Message) {
	roomName := message.Message

	room := client.wsServer.findRoomByName(roomName)
	if room == nil {
			room = client.wsServer.createRoom(roomName)
	}

	client.Rooms[room] = true

	room.register <- client
}

func (client *Client) handleLeaveRoomMessage(message Message) {
	room := client.wsServer.findRoomByName(message.Message)
	if _, ok := client.Rooms[room]; ok {
			delete(client.Rooms, room)
	}

	room.unregister <- client
}

// ServeWs handles websocket requests from clients requests.
func ServeWs(wsServer *Hub, w http.ResponseWriter, r *http.Request) {

	name, ok := r.URL.Query()["name"]

	if !ok || len(name[0]) < 1 {
		log.Println("Url Param 'name' is missing")
		return
	}

	upgrader := &websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	client := NewClient(conn, name[0])

	go client.WriteLoop()
	go client.ReadLoop(wsServer.BroadcastCh, wsServer.UnRegisterCh)

	wsServer.RegisterCh <- client
}

func (c *Client) handleNewMessage(jsonMessage []byte) {

	var message Message
	if err := json.Unmarshal(jsonMessage, &message); err != nil {
			log.Printf("Error on unmarshal JSON message %s", err)
	}

	// Attach the client object as the sender of the messsage.
	message.Sender = c

	switch message.Action {
	case SendMessageAction:
			// The send-message action, this will send messages to a specific room now.
			// Which room wil depend on the message Target
			roomName := message.Target.GetId()
			// Use the ChatServer method to find the room, and if found, broadcast!
			if room := c.wsServer.findRoomByName(roomName); room != nil {
					room.broadcast <- &message
			}
	// We delegate the join and leave actions. 
	case JoinRoomAction:
			c.handleJoinRoomMessage(message)

	case LeaveRoomAction:
			c.handleLeaveRoomMessage(message)
	}
}

func (c *Client) disconnect(unregister chan<- *Client) {
	unregister <- c
	for room := range c.Rooms {
		room.unregister <- c
	}
	c.ws.Close()
}