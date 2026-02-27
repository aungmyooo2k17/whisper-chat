package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"github.com/whisper/chat-app/internal/ban"
	"github.com/whisper/chat-app/internal/chat"
	"github.com/whisper/chat-app/internal/database"
	"github.com/whisper/chat-app/internal/matching"
	"github.com/whisper/chat-app/internal/messaging"
	"github.com/whisper/chat-app/internal/metrics"
	"github.com/whisper/chat-app/internal/moderation"
	"github.com/whisper/chat-app/internal/protocol"
	"github.com/whisper/chat-app/internal/ratelimit"
	"github.com/whisper/chat-app/internal/report"
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
	banStore := ban.NewStore(sessionStore.Client())
	msgBuffer := chat.NewMessageBuffer()

	// --- Rate Limiter ---
	rateLimiter := ratelimit.NewLimiter(sessionStore.Client())

	// --- Content Filter ---
	contentFilter := moderation.NewFilter()
	log.Printf("  content_filter: loaded")

	// --- PostgreSQL ---
	databaseURL := "postgres://whisper:whisper_dev@localhost:5432/whisper?sslmode=disable"
	if v := os.Getenv("DATABASE_URL"); v != "" {
		databaseURL = v
	}

	// Resolve migrations path relative to the working directory.
	migrationsPath, err := filepath.Abs("migrations")
	if err != nil {
		log.Fatalf("failed to resolve migrations path: %v", err)
	}
	if err := database.RunMigrations(databaseURL, migrationsPath); err != nil {
		log.Fatalf("failed to run database migrations: %v", err)
	}
	log.Printf("database migrations applied successfully")

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("failed to open database connection: %v", err)
	}
	if err := db.Ping(); err != nil {
		log.Fatalf("failed to ping database: %v", err)
	}
	reportStore := report.NewStore(db)

	log.Printf("Whisper WebSocket server starting")
	log.Printf("  listen_addr:     %s", config.ListenAddr)
	log.Printf("  worker_pool:     %d", config.WorkerPoolSize)
	log.Printf("  max_connections:  %d", config.MaxConnections)
	log.Printf("  read_timeout:    %s", config.ReadTimeout)
	log.Printf("  write_timeout:   %s", config.WriteTimeout)
	log.Printf("  nats_url:        %s", natsConfig.URL)
	log.Printf("  redis_addr:      %s", redisAddr)
	log.Printf("  database_url:    %s", databaseURL)
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
				} else {
					metrics.MessagesTotal.WithLabelValues("received").Inc()
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
	// set_fingerprint — associate browser fingerprint with session (ABUSE-4)
	// Ban check on fingerprint submission (ABUSE-5)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeSetFingerprint, func(conn *ws.Connection, msg interface{}) {
		fpMsg, ok := msg.(protocol.SetFingerprintMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()

		if fpMsg.Fingerprint == "" {
			return
		}

		if err := sessionStore.SetFingerprint(ctx, sid, fpMsg.Fingerprint); err != nil {
			log.Printf("set_fingerprint: failed for session=%s: %v", sid, err)
			return
		}

		// ABUSE-5: Check if fingerprint is banned.
		banned, remaining, reason, err := banStore.IsBanned(ctx, fpMsg.Fingerprint)
		if err != nil {
			log.Printf("[ban] check error for session=%s: %v", sid, err)
			return // fail open — let the user through on Redis errors
		}
		if banned {
			log.Printf("[ban] session=%s fingerprint=%s is banned (remaining=%ds reason=%s)",
				sid, fpMsg.Fingerprint, remaining, reason)
			resp, _ := protocol.NewServerMessage(protocol.TypeBanned, protocol.BannedMsg{
				Duration: remaining,
				Reason:   reason,
			})
			conn.WriteMessage(resp)
			// Disconnect after sending ban notification.
			server.RemoveConnection(conn)
			return
		}

		log.Printf("set_fingerprint session=%s", sid)
	})

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

		// ABUSE-1: Rate limit match requests (10 per minute per session).
		if allowed, _ := rateLimiter.Allow(ctx, sid, ratelimit.RuleMatch); !allowed {
			log.Printf("[ratelimit] find_match rejected session=%s", sid)
			resp, _ := protocol.NewServerMessage(protocol.TypeRateLimited, protocol.RateLimitedMsg{
				RetryAfter: int(ratelimit.RuleMatch.Window.Seconds()),
			})
			conn.WriteMessage(resp)
			return
		}

		// ABUSE-2: Filter offensive interest tags.
		cleanInterests := contentFilter.CheckInterests(findMsg.Interests)
		if len(cleanInterests) != len(findMsg.Interests) {
			log.Printf("[filter] interests filtered session=%s original=%d clean=%d", sid, len(findMsg.Interests), len(cleanInterests))
		}
		findMsg.Interests = cleanInterests

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
						// MOD-2: Subscribe to async moderation results for this session.
						natsClient.SubscribeModerationResult(sid, func(data []byte) {
							var modResult moderation.ModerationResult
							if err := json.Unmarshal(data, &modResult); err != nil {
								return
							}
							if !modResult.Blocked {
								return
							}
							log.Printf("[moderation] async flag session=%s chat=%s reason=%s", sid, modResult.ChatID, modResult.Reason)
							warnResp, _ := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
								Code:    "content_warning",
								Message: "Your message was flagged by our moderation system",
							})
							server.SendMessage(sid, warnResp)
						})
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
			metrics.ActiveChats.Inc()
			subscribeToChatNATS(sid, chatID)
			sessionStore.SetChatID(ctx, sid, chatID)
			// MOD-2: Subscribe to async moderation results for this session.
			natsClient.SubscribeModerationResult(sid, func(data []byte) {
				var modResult moderation.ModerationResult
				if err := json.Unmarshal(data, &modResult); err != nil {
					return
				}
				if !modResult.Blocked {
					return
				}
				log.Printf("[moderation] async flag session=%s chat=%s reason=%s", sid, modResult.ChatID, modResult.Reason)
				warnResp, _ := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
					Code:    "content_warning",
					Message: "Your message was flagged by our moderation system",
				})
				server.SendMessage(sid, warnResp)
			})

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

		// ABUSE-1: Rate limit messages (5 per 10 seconds per session).
		if allowed, _ := rateLimiter.Allow(ctx, sid, ratelimit.RuleMessage); !allowed {
			log.Printf("[ratelimit] message rejected session=%s", sid)
			resp, _ := protocol.NewServerMessage(protocol.TypeRateLimited, protocol.RateLimitedMsg{
				RetryAfter: int(ratelimit.RuleMessage.Window.Seconds()),
			})
			conn.WriteMessage(resp)
			return
		}

		// CHAT-7: Validate message content.
		if err := chat.ValidateMessage(chatMsg.Text); err != nil {
			errResp, _ := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
				Code: "invalid_message", Message: err.Error(),
			})
			conn.WriteMessage(errResp)
			return
		}

		// ABUSE-2: Content filter check.
		if result := contentFilter.Check(chatMsg.Text); result.Blocked {
			metrics.MessagesTotal.WithLabelValues("blocked").Inc()
			log.Printf("[filter] message blocked session=%s reason=%s term=%s", sid, result.Reason, result.Term)
			errResp, _ := protocol.NewServerMessage(protocol.TypeError, protocol.ErrorMsg{
				Code:    "message_blocked",
				Message: "Message contains prohibited content",
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
		metrics.MessagesTotal.WithLabelValues("sent").Inc()

		// CHAT-2: Publish message via NATS for delivery to partner.
		now := time.Now().Unix()
		event := chat.ChatEvent{
			Type: "message",
			From: sid,
			Text: chatMsg.Text,
			Ts:   now,
		}
		data, _ := json.Marshal(event)
		natsClient.PublishChatMessage(chatMsg.ChatID, data)

		// MOD-6: Buffer message for report context.
		msgBuffer.Add(chatMsg.ChatID, chat.BufferedMessage{
			From: sid,
			Text: chatMsg.Text,
			Ts:   now,
		})

		// MOD-2: Async moderation check via NATS.
		modReq := moderation.ModerationRequest{
			SessionID: sid,
			ChatID:    chatMsg.ChatID,
			Text:      chatMsg.Text,
			Ts:        now,
		}
		modData, _ := json.Marshal(modReq)
		natsClient.PublishModerationRequest(modData)
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

		metrics.ActiveChats.Dec()

		// Cleanup.
		_ = natsClient.UnsubscribeFromChat(sid)
		_ = natsClient.UnsubscribeModerationResult(sid) // MOD-2: Stop async moderation results.
		chatStore.Delete(ctx, chatID)
		sessionStore.ClearChatID(ctx, sid)
		msgBuffer.Remove(chatID) // MOD-6: Clean up message buffer.

		log.Printf("end_chat from session=%s chat=%s", sid, chatID)
	})

	// -----------------------------------------------------------------------
	// report — report a chat partner for abuse (ABUSE-6)
	// -----------------------------------------------------------------------
	dispatcher.Register(protocol.TypeReport, func(conn *ws.Connection, msg interface{}) {
		reportMsg, ok := msg.(protocol.ReportMsg)
		if !ok {
			return
		}
		sid := conn.ID
		ctx := context.Background()

		// Look up the chat to identify the partner.
		cs, err := chatStore.Get(ctx, reportMsg.ChatID)
		if err != nil || cs == nil || !cs.IsParticipant(sid) {
			log.Printf("[report] invalid chat session=%s chat=%s", sid, reportMsg.ChatID)
			return
		}

		partnerID := cs.GetPartner(sid)
		if partnerID == "" {
			return
		}

		// Resolve the partner's fingerprint so we can track reports
		// against it.
		partnerSession, err := sessionStore.Get(ctx, partnerID)
		if err != nil || partnerSession == nil || partnerSession.Fingerprint == "" {
			log.Printf("[report] partner session not found or missing fingerprint session=%s partner=%s", sid, partnerID)
			return
		}

		// Resolve the reporter's fingerprint for the PostgreSQL record.
		reporterFP := ""
		reporterSession, err := sessionStore.Get(ctx, sid)
		if err == nil && reporterSession != nil {
			reporterFP = reporterSession.Fingerprint
		}

		// MOD-6: Capture buffered messages for the report.
		buffered := msgBuffer.Get(reportMsg.ChatID)
		reportMessages := make([]report.MessageEntry, len(buffered))
		for i, bm := range buffered {
			reportMessages[i] = report.MessageEntry{
				From: bm.From,
				Text: bm.Text,
				Ts:   bm.Ts,
			}
		}

		// Store the report in PostgreSQL (if reporter fingerprint is available).
		if reporterFP != "" {
			r := &report.Report{
				ReporterFingerprint: reporterFP,
				ReportedFingerprint: partnerSession.Fingerprint,
				ChatID:              reportMsg.ChatID,
				Reason:              reportMsg.Reason,
				Messages:            reportMessages,
			}
			if err := reportStore.Create(ctx, r); err != nil {
				log.Printf("[report] failed to store in postgres: %v", err)
				// Continue — ban logic should still run even if PG write fails.
			}
		} else {
			log.Printf("[report] reporter fingerprint empty, skipping postgres store session=%s", sid)
		}

		// Track the report and check for auto-ban (3 reports in 24h).
		banned, duration, err := banStore.ReportAndCheck(ctx, partnerSession.Fingerprint, reportMsg.Reason)
		if err != nil {
			log.Printf("[report] error tracking report: %v", err)
			// Fail open — the report was not counted, but don't crash.
			return
		}

		if banned {
			// Notify the banned user if they are still connected.
			resp, _ := protocol.NewServerMessage(protocol.TypeBanned, protocol.BannedMsg{
				Duration: int(duration.Seconds()),
				Reason:   "multiple_reports",
			})
			server.SendMessage(partnerID, resp)

			// Disconnect the banned user.
			if partnerConn := server.Connections().Get(partnerID); partnerConn != nil {
				server.RemoveConnection(partnerConn)
			}
		}

		// ABUSE-8: PostgreSQL cross-check — catch bans that Redis missed
		// (e.g. after a Redis restart that lost counters).
		if !banned {
			pgCount, pgErr := reportStore.CountRecent(ctx, partnerSession.Fingerprint, 24*time.Hour)
			if pgErr != nil {
				log.Printf("[report] pg cross-check failed fp=%s: %v", partnerSession.Fingerprint, pgErr)
				// Fail open — don't crash, just skip the PG check.
			} else if pgCount >= ban.AutoBanThreshold {
				log.Printf("[report] pg cross-check triggered ban fp=%s pg_count=%d (redis missed)", partnerSession.Fingerprint, pgCount)
				pgDuration, escErr := banStore.Escalate(ctx, partnerSession.Fingerprint, "multiple_reports")
				if escErr != nil {
					log.Printf("[report] pg cross-check escalate failed fp=%s: %v", partnerSession.Fingerprint, escErr)
				} else {
					banned = true

					// Notify the banned user if they are still connected.
					resp, _ := protocol.NewServerMessage(protocol.TypeBanned, protocol.BannedMsg{
						Duration: int(pgDuration.Seconds()),
						Reason:   "multiple_reports",
					})
					server.SendMessage(partnerID, resp)

					// Disconnect the banned user.
					if partnerConn := server.Connections().Get(partnerID); partnerConn != nil {
						server.RemoveConnection(partnerConn)
					}
				}
			}
		}

		log.Printf("[report] session=%s reported partner=%s fp=%s reason=%s banned=%v",
			sid, partnerID, partnerSession.Fingerprint, reportMsg.Reason, banned)
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
				_ = natsClient.UnsubscribeModerationResult(connID) // MOD-2: Stop async moderation results.
				chatStore.Delete(ctx, sess.ChatID)
			}
			msgBuffer.Remove(sess.ChatID) // MOD-2/MOD-6: Clean up message buffer.
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
		if err := db.Close(); err != nil {
			log.Printf("database close error: %v", err)
		}
		os.Exit(0)
	}()

	if err := server.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
