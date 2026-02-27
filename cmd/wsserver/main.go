package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/whisper/chat-app/internal/chat"
	"github.com/whisper/chat-app/internal/matching"
	"github.com/whisper/chat-app/internal/messaging"
	"github.com/whisper/chat-app/internal/protocol"
	"github.com/whisper/chat-app/internal/session"
	"github.com/whisper/chat-app/internal/ws"
)

func main() {
	config := ws.DefaultServerConfig()

	if addr := os.Getenv("LISTEN_ADDR"); addr != "" {
		config.ListenAddr = addr
	}
	if v := os.Getenv("WORKER_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			config.WorkerPoolSize = n
		}
	}
	if v := os.Getenv("MAX_CONNECTIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			config.MaxConnections = n
		}
	}
	if v := os.Getenv("READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.ReadTimeout = d
		}
	}
	if v := os.Getenv("WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			config.WriteTimeout = d
		}
	}

	// --- NATS ---
	natsConfig := messaging.DefaultNATSConfig()
	if natsURL := os.Getenv("NATS_URL"); natsURL != "" {
		natsConfig.URL = natsURL
	}
	natsClient, err := messaging.NewNATSClient(natsConfig)
	if err != nil {
		log.Fatalf("failed to connect to NATS: %v", err)
	}

	// --- Redis ---
	redisAddr := "localhost:6379"
	if v := os.Getenv("REDIS_ADDR"); v != "" {
		redisAddr = v
	}
	serverName, _ := os.Hostname()
	if v := os.Getenv("SERVER_NAME"); v != "" {
		serverName = v
	}
	if serverName == "" {
		serverName = "ws-1"
	}

	sessionStore, err := session.NewStore(redisAddr, serverName)
	if err != nil {
		log.Fatalf("failed to connect to Redis: %v", err)
	}

	chatStore := chat.NewStore(sessionStore.Client())

	log.Printf("Whisper WebSocket server starting")
	log.Printf("  listen_addr:     %s", config.ListenAddr)
	log.Printf("  worker_pool:     %d", config.WorkerPoolSize)
	log.Printf("  max_connections:  %d", config.MaxConnections)
	log.Printf("  read_timeout:    %s", config.ReadTimeout)
	log.Printf("  write_timeout:   %s", config.WriteTimeout)
	log.Printf("  nats_url:        %s", natsConfig.URL)
	log.Printf("  redis_addr:      %s", redisAddr)
	log.Printf("  server_name:     %s", serverName)

	// Declare server early so closures can capture it.
	var server *ws.Server

	// subscribeToChatNATS sets up NATS subscription for real-time chat messages.
	// It filters out self-sent messages and forwards partner events to the client.
	subscribeToChatNATS := func(localSID, chatID string) {
		log.Printf("[chat-sub] subscribing session=%s to chat=%s", localSID, chatID)
		if err := natsClient.SubscribeToChat(chatID, localSID, func(data []byte) {
			var event chat.ChatEvent
			if err := json.Unmarshal(data, &event); err != nil {
				log.Printf("[chat-sub] unmarshal error for session=%s: %v", localSID, err)
				return
			}
			log.Printf("[chat-sub] session=%s received event type=%s from=%s (self=%v)", localSID, event.Type, event.From, event.From == localSID)
			if event.From == localSID {
				return // don't echo to sender
			}

			switch event.Type {
			case "message":
				resp, _ := protocol.NewServerMessage(protocol.TypeMessage, protocol.ServerChatMsg{
					From: "partner",
					Text: event.Text,
					Ts:   event.Ts,
				})
				if err := server.SendMessage(localSID, resp); err != nil {
					log.Printf("[chat-sub] send message to %s failed: %v", localSID, err)
				}

			case "typing":
				resp, _ := protocol.NewServerMessage(protocol.TypeTyping, protocol.ServerTypingMsg{
					IsTyping: event.IsTyping,
				})
				server.SendMessage(localSID, resp)

			case "partner_left":
				log.Printf("[chat-sub] partner_left -> sending to session=%s", localSID)
				resp, _ := protocol.NewServerMessage(protocol.TypePartnerLeft, protocol.PartnerLeftMsg{})
				server.SendMessage(localSID, resp)
				_ = natsClient.UnsubscribeFromChat(localSID)
				sessionStore.ClearChatID(context.Background(), localSID)
			}
		}); err != nil {
			log.Printf("[chat-sub] subscribe chat=%s for session=%s FAILED: %v", chatID, localSID, err)
		}
	}

	dispatcher := ws.NewMessageDispatcher(nil)

	// -----------------------------------------------------------------------
	// find_match — enter matching queue
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeFindMatch, func(conn *ws.Connection, msg interface{}) {
		findMsg, ok := msg.(protocol.FindMatchMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()

		interests := strings.Join(findMsg.Interests, ",")
		sessionStore.SetInterests(ctx, sid, interests)
		sessionStore.UpdateStatus(ctx, sid, session.StatusMatching)

		// Publish match request to NATS.
		req := matching.MatchRequest{SessionID: sid, Interests: findMsg.Interests}
		data, _ := json.Marshal(req)
		natsClient.PublishMatchRequest(data)

		// Subscribe to match result.
		_ = natsClient.UnsubscribeMatchFound(sid)
		natsClient.SubscribeMatchFound(sid, func(data []byte) {
			var result matching.MatchResult
			if err := json.Unmarshal(data, &result); err != nil {
				return
			}

			if result.Timeout {
				// MATCH-6: 30s timeout, no match found.
				resp, _ := protocol.NewServerMessage(protocol.TypeMatchTimeout, protocol.MatchTimeoutMsg{})
				server.SendMessage(sid, resp)
				sessionStore.UpdateStatus(context.Background(), sid, session.StatusIdle)
			} else {
				// Match found — send match_found and subscribe to lifecycle events.
				resp, _ := protocol.NewServerMessage(protocol.TypeMatchFound, protocol.MatchFoundMsg{
					ChatID:          result.ChatID,
					SharedInterests: result.SharedInterests,
					AcceptDeadline:  result.AcceptDeadline,
				})
				server.SendMessage(sid, resp)

				// Subscribe to match lifecycle notifications (accept/decline/timeout).
				_ = natsClient.UnsubscribeMatchNotify(sid)
				natsClient.SubscribeMatchNotify(sid, func(data []byte) {
					var notif matching.MatchNotification
					if err := json.Unmarshal(data, &notif); err != nil {
						return
					}
					bgCtx := context.Background()

					switch notif.Type {
					case "accepted":
						// Partner accepted (we're the first accepter).
						subscribeToChatNATS(sid, notif.ChatID)
						sessionStore.SetChatID(bgCtx, sid, notif.ChatID)
						resp, _ := protocol.NewServerMessage(protocol.TypeMatchAccepted, protocol.MatchAcceptedMsg{
							ChatID: notif.ChatID,
						})
						server.SendMessage(sid, resp)

					case "declined":
						resp, _ := protocol.NewServerMessage(protocol.TypeMatchDeclined, protocol.MatchDeclinedMsg{})
						server.SendMessage(sid, resp)
						sessionStore.UpdateStatus(bgCtx, sid, session.StatusIdle)

					case "timed_out":
						resp, _ := protocol.NewServerMessage(protocol.TypeMatchDeclined, protocol.MatchDeclinedMsg{})
						server.SendMessage(sid, resp)
						sessionStore.UpdateStatus(bgCtx, sid, session.StatusIdle)
					}

					_ = natsClient.UnsubscribeMatchNotify(sid)
				})
			}

			_ = natsClient.UnsubscribeMatchFound(sid)
		})

		// Send matching_started to client.
		resp, _ := protocol.NewServerMessage(protocol.TypeMatchingStarted, protocol.MatchingStartedMsg{
			Timeout: 30,
		})
		conn.WriteMessage(resp)
		log.Printf("find_match from session=%s interests=%v", sid, findMsg.Interests)
	})

	// -----------------------------------------------------------------------
	// cancel_match — leave matching queue
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeCancelMatch, func(conn *ws.Connection, msg interface{}) {
		sid := conn.ID
		ctx := context.Background()

		req := matching.CancelRequest{SessionID: sid}
		data, _ := json.Marshal(req)
		natsClient.PublishMatchCancel(data)

		_ = natsClient.UnsubscribeMatchFound(sid)
		_ = natsClient.UnsubscribeMatchNotify(sid)
		sessionStore.UpdateStatus(ctx, sid, session.StatusIdle)

		log.Printf("cancel_match from session=%s", sid)
	})

	// -----------------------------------------------------------------------
	// accept_match — accept a proposed match (MATCH-7)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeAcceptMatch, func(conn *ws.Connection, msg interface{}) {
		acceptMsg, ok := msg.(protocol.AcceptMatchMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()
		chatID := acceptMsg.ChatID

		result, err := chatStore.AcceptMatch(ctx, chatID, sid)
		if err != nil {
			log.Printf("accept_match: %v", err)
			return
		}

		switch result {
		case 1:
			// Both accepted — activate chat.
			subscribeToChatNATS(sid, chatID)
			sessionStore.SetChatID(ctx, sid, chatID)

			resp, _ := protocol.NewServerMessage(protocol.TypeMatchAccepted, protocol.MatchAcceptedMsg{
				ChatID: chatID,
			})
			server.SendMessage(sid, resp)

			// Notify partner via NATS.
			cs, _ := chatStore.Get(ctx, chatID)
			if cs != nil {
				partnerID := cs.GetPartner(sid)
				notif, _ := json.Marshal(matching.MatchNotification{
					Type: "accepted", ChatID: chatID,
				})
				natsClient.PublishMatchNotify(partnerID, notif)
			}

			_ = natsClient.UnsubscribeMatchNotify(sid)
			log.Printf("accept_match from session=%s chat=%s (both accepted)", sid, chatID)

		case 0:
			// Waiting for partner — nothing to do, notification handler will fire.
			log.Printf("accept_match from session=%s chat=%s (waiting for partner)", sid, chatID)

		default:
			log.Printf("accept_match from session=%s chat=%s error_code=%d", sid, chatID, result)
		}
	})

	// -----------------------------------------------------------------------
	// decline_match — decline a proposed match (MATCH-7)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeDeclineMatch, func(conn *ws.Connection, msg interface{}) {
		declineMsg, ok := msg.(protocol.DeclineMatchMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()
		chatID := declineMsg.ChatID

		cs, _ := chatStore.Get(ctx, chatID)
		if cs == nil {
			return
		}

		partnerID := cs.GetPartner(sid)

		// Delete the pending chat.
		chatStore.Delete(ctx, chatID)

		// Notify partner.
		notif, _ := json.Marshal(matching.MatchNotification{
			Type: "declined", ChatID: chatID,
		})
		natsClient.PublishMatchNotify(partnerID, notif)

		// Reset own state.
		_ = natsClient.UnsubscribeMatchNotify(sid)
		sessionStore.UpdateStatus(ctx, sid, session.StatusIdle)

		log.Printf("decline_match from session=%s chat=%s", sid, chatID)
	})

	// -----------------------------------------------------------------------
	// message — send a chat message (CHAT-2, CHAT-7)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeMessage, func(conn *ws.Connection, msg interface{}) {
		chatMsg, ok := msg.(protocol.ChatMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()

		// CHAT-7: Validate message content.
		if err := chat.ValidateMessage(chatMsg.Text); err != nil {
			errResp, _ := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
				Code: "invalid_message", Message: err.Error(),
			})
			conn.WriteMessage(errResp)
			return
		}

		// Validate chat ownership.
		cs, err := chatStore.Get(ctx, chatMsg.ChatID)
		if err != nil || cs == nil || !cs.IsParticipant(sid) || cs.Status != chat.StatusActive {
			log.Printf("[message] REJECTED session=%s chat=%s err=%v cs_nil=%v", sid, chatMsg.ChatID, err, cs == nil)
			if cs != nil {
				log.Printf("[message]   status=%s isParticipant=%v", cs.Status, cs.IsParticipant(sid))
			}
			errResp, _ := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
				Code: "invalid_chat", Message: "not in an active chat",
			})
			conn.WriteMessage(errResp)
			return
		}

		log.Printf("[message] session=%s chat=%s text_len=%d", sid, chatMsg.ChatID, len(chatMsg.Text))

		// CHAT-2: Publish message via NATS for delivery to partner.
		event := chat.ChatEvent{
			Type: "message",
			From: sid,
			Text: chatMsg.Text,
			Ts:   time.Now().Unix(),
		}
		data, _ := json.Marshal(event)
		natsClient.PublishChatMessage(chatMsg.ChatID, data)
	})

	// -----------------------------------------------------------------------
	// typing — relay typing indicator (CHAT-3)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeTyping, func(conn *ws.Connection, msg interface{}) {
		typingMsg, ok := msg.(protocol.TypingMsg)
		if !ok {
			return
		}
		sid := conn.ID

		event := chat.ChatEvent{
			Type:     "typing",
			From:     sid,
			IsTyping: typingMsg.IsTyping,
		}
		data, _ := json.Marshal(event)
		natsClient.PublishChatMessage(typingMsg.ChatID, data)
	})

	// -----------------------------------------------------------------------
	// end_chat — end an active chat (CHAT-4)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeEndChat, func(conn *ws.Connection, msg interface{}) {
		endMsg, ok := msg.(protocol.EndChatMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()
		chatID := endMsg.ChatID

		cs, _ := chatStore.Get(ctx, chatID)
		if cs == nil || !cs.IsParticipant(sid) {
			return
		}

		// Publish partner_left event via NATS.
		event := chat.ChatEvent{Type: "partner_left", From: sid}
		data, _ := json.Marshal(event)
		natsClient.PublishChatMessage(chatID, data)

		// Cleanup.
		_ = natsClient.UnsubscribeFromChat(sid)
		chatStore.Delete(ctx, chatID)
		sessionStore.ClearChatID(ctx, sid)

		log.Printf("end_chat from session=%s chat=%s", sid, chatID)
	})

	// -----------------------------------------------------------------------
	// report — placeholder (Sprint 7+)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeReport, func(conn *ws.Connection, msg interface{}) {
		log.Printf("report from session=%s", conn.ID)
	})

	server = ws.NewServer(config, sessionStore, dispatcher.Dispatch)
	dispatcher.SetServer(server)

	// CHAT-5: Handle disconnects — notify partner if user was in a chat.
	server.SetOnDisconnect(func(connID string) {
		log.Printf("[disconnect] session=%s triggered", connID)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		sess, err := sessionStore.Get(ctx, connID)
		if err != nil || sess == nil {
			log.Printf("[disconnect] session=%s not found in redis (err=%v)", connID, err)
			return
		}
		log.Printf("[disconnect] session=%s status=%s chat_id=%s", connID, sess.Status, sess.ChatID)

		// Clean up matching state.
		if sess.Status == session.StatusMatching {
			log.Printf("[disconnect] session=%s was matching, cancelling", connID)
			req := matching.CancelRequest{SessionID: connID}
			data, _ := json.Marshal(req)
			natsClient.PublishMatchCancel(data)
			_ = natsClient.UnsubscribeMatchFound(connID)
			_ = natsClient.UnsubscribeMatchNotify(connID)
		}

		// If in an active chat, notify partner and clean up.
		if sess.ChatID != "" {
			log.Printf("[disconnect] session=%s was in chat=%s, publishing partner_left", connID, sess.ChatID)
			cs, _ := chatStore.Get(ctx, sess.ChatID)
			if cs != nil && cs.IsParticipant(connID) {
				event := chat.ChatEvent{Type: "partner_left", From: connID}
				data, _ := json.Marshal(event)
				natsClient.PublishChatMessage(sess.ChatID, data)
				_ = natsClient.UnsubscribeFromChat(connID)
				chatStore.Delete(ctx, sess.ChatID)
			}
		}

		log.Printf("disconnect cleanup for session=%s status=%s", connID, sess.Status)
	})

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, initiating graceful shutdown...", sig)
		natsClient.Close()
		if err := server.Shutdown(); err != nil {
			log.Printf("shutdown error: %v", err)
		}
		if err := sessionStore.Close(); err != nil {
			log.Printf("session store close error: %v", err)
		}
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
