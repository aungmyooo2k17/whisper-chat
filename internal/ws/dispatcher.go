package ws

import (
	"log"
	"time"

	"github.com/whisper/chat-app/internal/protocol"
)

// MessageHandler is the callback signature for handling a parsed client message.
// The msg parameter is the concrete struct returned by protocol.ParseClientMessage
// (e.g., protocol.FindMatchMsg, protocol.ChatMsg, etc.).
type MessageHandler func(conn *Connection, msg interface{})

// MessageDispatcher routes incoming WebSocket messages to registered handlers
// based on the message type. It handles the built-in ping/pong keepalive
// internally and sends structured error responses for malformed or unsupported
// messages.
type MessageDispatcher struct {
	handlers map[string]MessageHandler
	server   *Server
}

// NewMessageDispatcher creates a MessageDispatcher bound to the given server.
// The server reference is used to send responses back to clients.
func NewMessageDispatcher(server *Server) *MessageDispatcher {
	return &MessageDispatcher{
		handlers: make(map[string]MessageHandler),
		server:   server,
	}
}

// SetServer assigns the Server reference on the dispatcher. This supports the
// initialization pattern where the dispatcher is created before the server
// (since NewServer requires the Dispatch callback).
func (d *MessageDispatcher) SetServer(server *Server) {
	d.server = server
}

// Register associates a MessageHandler with a message type. If a handler was
// already registered for the given type, it is silently replaced.
func (d *MessageDispatcher) Register(msgType string, handler MessageHandler) {
	d.handlers[msgType] = handler
}

// Dispatch is the onMessage callback implementation. It parses the raw bytes
// into a typed message, handles ping internally, and routes all other types to
// the registered handler. Parse errors and unregistered types result in an
// error message sent back to the client.
func (d *MessageDispatcher) Dispatch(conn *Connection, data []byte) {
	msgType, msg, err := protocol.ParseClientMessage(data)
	if err != nil {
		log.Printf("ws: dispatch parse error session=%s: %v", conn.ID, err)
		d.sendError(conn, "parse_error", "invalid message format")
		return
	}

	// Built-in ping handler — respond immediately without requiring registration.
	if msgType == protocol.TypePing {
		d.sendPong(conn)
		return
	}

	handler, ok := d.handlers[msgType]
	if !ok {
		log.Printf("ws: unsupported message type=%q session=%s", msgType, conn.ID)
		d.sendError(conn, "unsupported_type", "unsupported message type")
		return
	}

	handler(conn, msg)
}

// sendError sends a structured error message back to the client. Errors during
// message construction or transmission are logged but not propagated.
func (d *MessageDispatcher) sendError(conn *Connection, code string, message string) {
	data, err := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
		Code:    code,
		Message: message,
	})
	if err != nil {
		log.Printf("ws: failed to build error message session=%s: %v", conn.ID, err)
		return
	}

	if err := conn.WriteMessage(data); err != nil {
		log.Printf("ws: failed to send error message session=%s: %v", conn.ID, err)
	}
}

// sendPong responds to a client ping with a pong message and updates the
// connection's LastPing timestamp to reflect the most recent keepalive.
func (d *MessageDispatcher) sendPong(conn *Connection) {
	conn.LastPing = time.Now()

	data, err := protocol.NewServerMessage(protocol.TypePong, protocol.PongMsg{})
	if err != nil {
		log.Printf("ws: failed to build pong message session=%s: %v", conn.ID, err)
		return
	}

	if err := conn.WriteMessage(data); err != nil {
		log.Printf("ws: failed to send pong message session=%s: %v", conn.ID, err)
	}
}
