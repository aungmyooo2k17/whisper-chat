<script lang="ts">
	import { app } from '$lib/stores.svelte';

	let inputText = $state('');
	let messagesEl: HTMLDivElement | undefined = $state();
	let typingTimeout: ReturnType<typeof setTimeout> | null = null;
	let isTyping = $state(false);

	// Auto-scroll to bottom when messages change
	$effect(() => {
		// Access messages.length to track changes
		if (app.messages.length && messagesEl) {
			messagesEl.scrollTop = messagesEl.scrollHeight;
		}
	});

	function handleSend() {
		if (!inputText.trim()) return;
		app.sendMessage(inputText);
		inputText = '';
		stopTyping();
	}

	function handleKeyDown(e: KeyboardEvent) {
		if (e.key === 'Enter' && !e.shiftKey) {
			e.preventDefault();
			handleSend();
		}
	}

	function handleInput() {
		if (!isTyping) {
			isTyping = true;
			app.sendTyping(true);
		}
		// Reset typing timeout
		if (typingTimeout !== null) {
			clearTimeout(typingTimeout);
		}
		typingTimeout = setTimeout(() => {
			stopTyping();
		}, 2000);
	}

	function stopTyping() {
		if (isTyping) {
			isTyping = false;
			app.sendTyping(false);
		}
		if (typingTimeout !== null) {
			clearTimeout(typingTimeout);
			typingTimeout = null;
		}
	}

	function formatTime(ts: number): string {
		const d = new Date(ts * 1000);
		return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
	}
</script>

<div class="chat">
	<header class="chat-header">
		<div class="header-info">
			<div class="status-dot"></div>
			<span class="header-title">Anonymous Chat</span>
			{#if app.sharedInterests.length > 0}
				<span class="shared-count">{app.sharedInterests.length} shared</span>
			{/if}
		</div>
		<button class="end-btn" onclick={() => app.endChat()}>
			End Chat
		</button>
	</header>

	{#if app.sharedInterests.length > 0}
		<div class="shared-bar">
			{#each app.sharedInterests as interest (interest)}
				<span class="shared-tag">{interest}</span>
			{/each}
		</div>
	{/if}

	<div class="messages" bind:this={messagesEl}>
		{#if app.messages.length === 0}
			<div class="empty-state">
				<p>Say hello to your match!</p>
			</div>
		{/if}

		{#each app.messages as msg, i (i)}
			<div class="message" class:message-me={msg.from === 'me'} class:message-partner={msg.from === 'partner'}>
				<div class="bubble">
					<p class="bubble-text">{msg.text}</p>
					<span class="bubble-time">{formatTime(msg.ts)}</span>
				</div>
			</div>
		{/each}

		{#if app.partnerTyping}
			<div class="message message-partner">
				<div class="bubble typing-bubble">
					<span class="typing-dot"></span>
					<span class="typing-dot"></span>
					<span class="typing-dot"></span>
				</div>
			</div>
		{/if}
	</div>

	<div class="input-bar">
		<input
			type="text"
			class="message-input"
			placeholder="Type a message..."
			bind:value={inputText}
			oninput={handleInput}
			onkeydown={handleKeyDown}
			maxlength={2000}
		/>
		<button
			class="send-btn"
			disabled={!inputText.trim()}
			onclick={handleSend}
		>
			Send
		</button>
	</div>
</div>

<style>
	.chat {
		display: flex;
		flex-direction: column;
		height: 100dvh;
		max-width: 720px;
		margin: 0 auto;
	}

	/* Header */
	.chat-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.75rem 1.25rem;
		border-bottom: 1px solid var(--color-border);
		background: var(--color-bg-elevated);
		flex-shrink: 0;
	}

	.header-info {
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.status-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--color-accent);
		flex-shrink: 0;
	}

	.header-title {
		font-weight: 600;
		font-size: 0.95rem;
	}

	.shared-count {
		font-size: 0.75rem;
		color: var(--color-text-dimmed);
		background: var(--color-surface);
		padding: 0.15rem 0.5rem;
		border-radius: var(--radius-full);
	}

	.end-btn {
		padding: 0.4rem 1rem;
		font-size: 0.8rem;
		font-weight: 600;
		border-radius: var(--radius-sm);
		border: 1px solid rgba(255, 107, 107, 0.3);
		background: rgba(255, 107, 107, 0.1);
		color: #ff6b6b;
		transition: all var(--transition-fast);
	}

	.end-btn:hover {
		background: rgba(255, 107, 107, 0.2);
		border-color: rgba(255, 107, 107, 0.5);
	}

	/* Shared interests bar */
	.shared-bar {
		display: flex;
		gap: 0.35rem;
		padding: 0.5rem 1.25rem;
		border-bottom: 1px solid var(--color-border);
		overflow-x: auto;
		flex-shrink: 0;
	}

	.shared-tag {
		padding: 0.2rem 0.55rem;
		font-size: 0.7rem;
		font-weight: 600;
		border-radius: var(--radius-full);
		background: var(--color-accent-muted);
		color: var(--color-accent);
		white-space: nowrap;
		flex-shrink: 0;
	}

	/* Messages area */
	.messages {
		flex: 1;
		overflow-y: auto;
		padding: 1rem 1.25rem;
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
	}

	.empty-state {
		flex: 1;
		display: flex;
		align-items: center;
		justify-content: center;
		color: var(--color-text-dimmed);
		font-size: 0.95rem;
	}

	.message {
		display: flex;
		max-width: 80%;
	}

	.message-me {
		align-self: flex-end;
	}

	.message-partner {
		align-self: flex-start;
	}

	.bubble {
		padding: 0.6rem 0.9rem;
		border-radius: var(--radius-md);
		max-width: 100%;
		word-break: break-word;
	}

	.message-me .bubble {
		background: var(--color-accent);
		color: #0a0a0a;
		border-bottom-right-radius: 4px;
	}

	.message-partner .bubble {
		background: var(--color-surface);
		color: var(--color-text);
		border-bottom-left-radius: 4px;
	}

	.bubble-text {
		font-size: 0.9rem;
		line-height: 1.45;
		white-space: pre-wrap;
	}

	.bubble-time {
		display: block;
		font-size: 0.65rem;
		margin-top: 0.25rem;
		opacity: 0.6;
		text-align: right;
	}

	/* Typing indicator */
	.typing-bubble {
		display: flex;
		align-items: center;
		gap: 4px;
		padding: 0.7rem 1rem;
	}

	.typing-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--color-text-muted);
		animation: typingBounce 1.4s ease-in-out infinite;
	}

	.typing-dot:nth-child(2) { animation-delay: 0.2s; }
	.typing-dot:nth-child(3) { animation-delay: 0.4s; }

	@keyframes typingBounce {
		0%, 60%, 100% { transform: translateY(0); opacity: 0.4; }
		30% { transform: translateY(-4px); opacity: 1; }
	}

	/* Input bar */
	.input-bar {
		display: flex;
		gap: 0.5rem;
		padding: 0.75rem 1.25rem;
		padding-bottom: max(0.75rem, env(safe-area-inset-bottom));
		border-top: 1px solid var(--color-border);
		background: var(--color-bg-elevated);
		flex-shrink: 0;
	}

	.message-input {
		flex: 1;
		padding: 0.65rem 0.9rem;
		font-size: 0.9rem;
		font-family: inherit;
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
		background: var(--color-bg);
		color: var(--color-text);
		outline: none;
		transition: border-color var(--transition-fast);
	}

	.message-input:focus {
		border-color: var(--color-accent-border);
	}

	.message-input::placeholder {
		color: var(--color-text-dimmed);
	}

	.send-btn {
		padding: 0.65rem 1.25rem;
		font-size: 0.9rem;
		font-weight: 700;
		border-radius: var(--radius-md);
		background: var(--color-accent);
		color: #0a0a0a;
		transition: all var(--transition-fast);
		flex-shrink: 0;
	}

	.send-btn:hover:not(:disabled) {
		background: var(--color-accent-hover);
	}

	.send-btn:disabled {
		background: var(--color-surface);
		color: var(--color-text-dimmed);
	}

	/* Responsive */
	@media (min-width: 640px) {
		.message {
			max-width: 65%;
		}
	}
</style>
