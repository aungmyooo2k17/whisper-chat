import { WebSocketClient } from './websocket.svelte';
import type {
	MatchingStartedMsg,
	MatchFoundMsg,
	MatchAcceptedMsg,
	MatchDeclinedMsg,
	MatchTimeoutMsg,
	ServerChatMsg,
	ServerTypingMsg,
	PartnerLeftMsg
} from './websocket.svelte';

// Determine WebSocket URL based on environment
const wsUrl =
	import.meta.env.VITE_WS_URL ||
	`${window.location.protocol === 'https:' ? 'wss:' : 'ws:'}//${window.location.host}/ws`;

export const ws = new WebSocketClient(wsUrl);

// App screens matching the user journey
export type AppScreen = 'idle' | 'matching' | 'match_found' | 'chatting' | 'chat_ended';

export interface ChatMessage {
	from: 'me' | 'partner';
	text: string;
	ts: number;
}

/**
 * AppState manages the full application state machine.
 * Screens: idle -> matching -> match_found -> chatting -> chat_ended -> idle
 */
export class AppState {
	screen = $state<AppScreen>('idle');
	chatId = $state<string | null>(null);
	sharedInterests = $state<string[]>([]);
	acceptDeadline = $state(0);
	matchTimeout = $state(0);
	messages = $state<ChatMessage[]>([]);
	partnerTyping = $state(false);
	partnerLeft = $state(false);

	private unsubs: (() => void)[] = [];

	constructor() {
		this.wireEvents();
	}

	private wireEvents() {
		this.unsubs.push(
			ws.on<MatchingStartedMsg>('matching_started', (msg) => {
				this.screen = 'matching';
				this.matchTimeout = msg.timeout;
			}),

			ws.on<MatchFoundMsg>('match_found', (msg) => {
				this.screen = 'match_found';
				this.chatId = msg.chat_id;
				this.sharedInterests = msg.shared_interests || [];
				this.acceptDeadline = msg.accept_deadline;
			}),

			ws.on<MatchAcceptedMsg>('match_accepted', (msg) => {
				this.screen = 'chatting';
				this.chatId = msg.chat_id;
				this.messages = [];
				this.partnerTyping = false;
				this.partnerLeft = false;
			}),

			ws.on<MatchDeclinedMsg>('match_declined', () => {
				this.screen = 'idle';
				this.resetChat();
			}),

			ws.on<MatchTimeoutMsg>('match_timeout', () => {
				this.screen = 'idle';
				this.resetChat();
			}),

			ws.on<ServerChatMsg>('message', (msg) => {
				if (this.screen === 'chatting') {
					this.messages = [...this.messages, { from: 'partner', text: msg.text, ts: msg.ts }];
				}
			}),

			ws.on<ServerTypingMsg>('typing', (msg) => {
				this.partnerTyping = msg.is_typing;
			}),

			ws.on<PartnerLeftMsg>('partner_left', () => {
				this.partnerLeft = true;
				this.screen = 'chat_ended';
			})
		);
	}

	// ----- Actions -----

	startMatching(interests: string[]) {
		ws.connect();
		// Wait for connection before sending find_match
		const checkAndSend = () => {
			if (ws.state === 'connected') {
				ws.findMatch(interests);
			} else {
				setTimeout(checkAndSend, 100);
			}
		};
		checkAndSend();
	}

	cancelMatching() {
		ws.cancelMatch();
		this.screen = 'idle';
		this.resetChat();
	}

	acceptMatch() {
		if (this.chatId) {
			ws.acceptMatch(this.chatId);
		}
	}

	declineMatch() {
		if (this.chatId) {
			ws.declineMatch(this.chatId);
		}
		this.screen = 'idle';
		this.resetChat();
	}

	sendMessage(text: string) {
		if (this.chatId && text.trim()) {
			ws.sendMessage(this.chatId, text.trim());
			this.messages = [
				...this.messages,
				{ from: 'me', text: text.trim(), ts: Math.floor(Date.now() / 1000) }
			];
		}
	}

	sendTyping(isTyping: boolean) {
		if (this.chatId) {
			ws.sendTyping(this.chatId, isTyping);
		}
	}

	endChat() {
		if (this.chatId) {
			ws.endChat(this.chatId);
		}
		this.partnerLeft = false;
		this.screen = 'chat_ended';
	}

	findNewMatch() {
		this.resetChat();
		this.screen = 'idle';
	}

	private resetChat() {
		this.chatId = null;
		this.sharedInterests = [];
		this.acceptDeadline = 0;
		this.matchTimeout = 0;
		this.messages = [];
		this.partnerTyping = false;
		this.partnerLeft = false;
	}

	destroy() {
		for (const unsub of this.unsubs) {
			unsub();
		}
		this.unsubs = [];
	}
}

export const app = new AppState();
