// WebSocket connection states
export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting';

// Message types matching the Go protocol
export type MessageType =
	| 'find_match'
	| 'cancel_match'
	| 'accept_match'
	| 'decline_match'
	| 'message'
	| 'typing'
	| 'end_chat'
	| 'report'
	| 'ping'
	| 'session_created'
	| 'matching_started'
	| 'match_found'
	| 'match_accepted'
	| 'match_declined'
	| 'match_timeout'
	| 'partner_left'
	| 'rate_limited'
	| 'banned'
	| 'error'
	| 'pong';

// Server message interfaces
export interface SessionCreatedMsg {
	type: 'session_created';
	session_id: string;
}
export interface MatchingStartedMsg {
	type: 'matching_started';
	timeout: number;
}
export interface MatchFoundMsg {
	type: 'match_found';
	chat_id: string;
	shared_interests: string[];
	accept_deadline: number;
}
export interface MatchAcceptedMsg {
	type: 'match_accepted';
	chat_id: string;
}
export interface MatchDeclinedMsg {
	type: 'match_declined';
}
export interface MatchTimeoutMsg {
	type: 'match_timeout';
}
export interface ServerChatMsg {
	type: 'message';
	from: string;
	text: string;
	ts: number;
}
export interface ServerTypingMsg {
	type: 'typing';
	is_typing: boolean;
}
export interface PartnerLeftMsg {
	type: 'partner_left';
}
export interface RateLimitedMsg {
	type: 'rate_limited';
	retry_after: number;
}
export interface BannedMsg {
	type: 'banned';
	duration: number;
	reason: string;
}
export interface ErrorMsg {
	type: 'error';
	code: string;
	message: string;
}
export interface PongMsg {
	type: 'pong';
}

export type ServerMessage =
	| SessionCreatedMsg
	| MatchingStartedMsg
	| MatchFoundMsg
	| MatchAcceptedMsg
	| MatchDeclinedMsg
	| MatchTimeoutMsg
	| ServerChatMsg
	| ServerTypingMsg
	| PartnerLeftMsg
	| RateLimitedMsg
	| BannedMsg
	| ErrorMsg
	| PongMsg;

const PING_INTERVAL_MS = 25_000;
const MAX_RECONNECT_ATTEMPTS = 10;
const BASE_RECONNECT_MS = 1000;
const MAX_RECONNECT_MS = 30_000;
const JITTER_MS = 5000;

export class WebSocketClient {
	// Reactive state using Svelte 5 runes
	private _state = $state<ConnectionState>('disconnected');
	private _sessionId = $state<string | null>(null);

	// Read-only reactive accessors
	get state(): ConnectionState {
		return this._state;
	}
	get sessionId(): string | null {
		return this._sessionId;
	}

	// Internal state
	private ws: WebSocket | null = null;
	private url: string;
	private reconnectAttempts = 0;
	private maxReconnectAttempts = MAX_RECONNECT_ATTEMPTS;
	private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
	private handlers: Map<string, ((msg: never) => void)[]> = new Map();
	private pingInterval: ReturnType<typeof setInterval> | null = null;
	private intentionalDisconnect = false;

	constructor(url: string) {
		this.url = url;

		// Internal handler: capture session_id on session_created
		this.on<SessionCreatedMsg>('session_created', (msg) => {
			this._sessionId = msg.session_id;
		});
	}

	/** Connect to the WebSocket server. */
	connect(): void {
		if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
			return;
		}

		this.intentionalDisconnect = false;
		this._state = this.reconnectAttempts > 0 ? 'reconnecting' : 'connecting';

		const ws = new WebSocket(this.url);

		ws.addEventListener('open', () => {
			this._state = 'connected';
			this.reconnectAttempts = 0;
			this.startPing();
		});

		ws.addEventListener('message', (event: MessageEvent) => {
			this.handleMessage(event);
		});

		ws.addEventListener('close', () => {
			this.cleanup();
			if (!this.intentionalDisconnect) {
				this.scheduleReconnect();
			}
		});

		ws.addEventListener('error', () => {
			// The close event will fire after error, which handles reconnection.
			// Nothing additional needed here.
		});

		this.ws = ws;
	}

	/** Disconnect intentionally (no auto-reconnect). */
	disconnect(): void {
		this.intentionalDisconnect = true;
		this.clearReconnectTimer();
		this.cleanup();
		this._sessionId = null;
	}

	/** Send a typed message object to the server as JSON. */
	send(msg: object): void {
		if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
			return;
		}
		this.ws.send(JSON.stringify(msg));
	}

	/**
	 * Register a handler for a specific server message type.
	 * Returns an unsubscribe function.
	 */
	on<T extends ServerMessage>(type: T['type'], handler: (msg: T) => void): () => void {
		const list = this.handlers.get(type);
		// We store handlers with a generic signature internally
		const wrapped = handler as (msg: never) => void;
		if (list) {
			list.push(wrapped);
		} else {
			this.handlers.set(type, [wrapped]);
		}

		// Return unsubscribe function
		return () => {
			const current = this.handlers.get(type);
			if (current) {
				const idx = current.indexOf(wrapped);
				if (idx !== -1) {
					current.splice(idx, 1);
				}
				if (current.length === 0) {
					this.handlers.delete(type);
				}
			}
		};
	}

	// ----- Convenience methods for client messages -----

	findMatch(interests: string[]): void {
		this.send({ type: 'find_match', interests });
	}

	cancelMatch(): void {
		this.send({ type: 'cancel_match' });
	}

	acceptMatch(chatId: string): void {
		this.send({ type: 'accept_match', chat_id: chatId });
	}

	declineMatch(chatId: string): void {
		this.send({ type: 'decline_match', chat_id: chatId });
	}

	sendMessage(chatId: string, text: string): void {
		this.send({ type: 'message', chat_id: chatId, text });
	}

	sendTyping(chatId: string, isTyping: boolean): void {
		this.send({ type: 'typing', chat_id: chatId, is_typing: isTyping });
	}

	endChat(chatId: string): void {
		this.send({ type: 'end_chat', chat_id: chatId });
	}

	report(chatId: string, reason: string): void {
		this.send({ type: 'report', chat_id: chatId, reason });
	}

	// ----- Private methods -----

	private handleMessage(event: MessageEvent): void {
		let data: { type?: string };
		try {
			data = JSON.parse(event.data as string);
		} catch {
			return;
		}

		if (!data.type) {
			return;
		}

		const list = this.handlers.get(data.type);
		if (list) {
			for (const handler of list) {
				handler(data as never);
			}
		}
	}

	private startPing(): void {
		this.stopPing();
		this.pingInterval = setInterval(() => {
			this.send({ type: 'ping' });
		}, PING_INTERVAL_MS);
	}

	private stopPing(): void {
		if (this.pingInterval !== null) {
			clearInterval(this.pingInterval);
			this.pingInterval = null;
		}
	}

	private cleanup(): void {
		this.stopPing();
		if (this.ws) {
			this.ws.close();
			this.ws = null;
		}
		this._state = 'disconnected';
	}

	private clearReconnectTimer(): void {
		if (this.reconnectTimer !== null) {
			clearTimeout(this.reconnectTimer);
			this.reconnectTimer = null;
		}
	}

	private scheduleReconnect(): void {
		if (this.reconnectAttempts >= this.maxReconnectAttempts) {
			this._state = 'disconnected';
			return;
		}

		this._state = 'reconnecting';

		// Exponential backoff: min(1000 * 2^attempt, 30000) + random jitter up to 5000
		const exponentialDelay = Math.min(BASE_RECONNECT_MS * Math.pow(2, this.reconnectAttempts), MAX_RECONNECT_MS);
		const jitter = Math.random() * JITTER_MS;
		const delay = exponentialDelay + jitter;

		this.reconnectAttempts++;

		this.reconnectTimer = setTimeout(() => {
			this.reconnectTimer = null;
			this.connect();
		}, delay);
	}
}
