<script lang="ts">
	import { app } from '$lib/stores.svelte';

	let elapsed = $state(0);
	let accepted = $state(false);
	let intervalId: ReturnType<typeof setInterval> | null = null;

	let remaining = $derived(Math.max(0, app.acceptDeadline - elapsed));
	let urgencyPct = $derived(app.acceptDeadline > 0 ? remaining / app.acceptDeadline : 1);

	$effect(() => {
		elapsed = 0;
		intervalId = setInterval(() => {
			elapsed++;
		}, 1000);

		return () => {
			if (intervalId !== null) {
				clearInterval(intervalId);
				intervalId = null;
			}
		};
	});
</script>

<div class="match-found">
	<div class="badge">Match Found</div>

	<h2 class="title">Someone wants to chat!</h2>

	{#if app.sharedInterests.length > 0}
		<div class="interests-section">
			<p class="interests-label">Shared interests</p>
			<div class="interests">
				{#each app.sharedInterests as interest (interest)}
					<span class="interest-tag">{interest}</span>
				{/each}
			</div>
		</div>
	{/if}

	<div class="timer-ring" class:timer-urgent={remaining <= 5}>
		<svg viewBox="0 0 80 80" class="timer-svg">
			<circle cx="40" cy="40" r="36" class="track" />
			<circle
				cx="40" cy="40" r="36"
				class="progress"
				style="stroke-dashoffset: {226.2 * (1 - urgencyPct)}"
			/>
		</svg>
		<span class="timer-text">{remaining}s</span>
	</div>

	<div class="actions">
		<button class="accept-btn" disabled={accepted} onclick={() => { accepted = true; app.acceptMatch(); }}>
			{#if accepted}Waiting for partner...{:else}Accept{/if}
		</button>
		<button class="decline-btn" disabled={accepted} onclick={() => app.declineMatch()}>
			Decline
		</button>
	</div>
</div>

<style>
	.match-found {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		min-height: 80dvh;
		padding: 2rem 1.25rem;
		text-align: center;
		gap: 1.25rem;
	}

	.badge {
		font-size: 0.8rem;
		font-weight: 700;
		text-transform: uppercase;
		letter-spacing: 0.08em;
		color: var(--color-accent);
		padding: 0.3rem 1rem;
		border-radius: var(--radius-full);
		background: var(--color-accent-muted);
		border: 1px solid var(--color-accent-border);
		animation: slideDown 0.3s ease-out;
	}

	@keyframes slideDown {
		from { transform: translateY(-10px); opacity: 0; }
		to { transform: translateY(0); opacity: 1; }
	}

	.title {
		font-size: 1.5rem;
		font-weight: 700;
		color: var(--color-text);
	}

	.interests-section {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.5rem;
	}

	.interests-label {
		font-size: 0.85rem;
		color: var(--color-text-dimmed);
		font-weight: 500;
	}

	.interests {
		display: flex;
		flex-wrap: wrap;
		justify-content: center;
		gap: 0.4rem;
	}

	.interest-tag {
		padding: 0.3rem 0.7rem;
		font-size: 0.8rem;
		font-weight: 600;
		border-radius: var(--radius-full);
		background: var(--color-accent-muted);
		border: 1px solid var(--color-accent-border);
		color: var(--color-accent);
	}

	/* Timer ring */
	.timer-ring {
		position: relative;
		width: 80px;
		height: 80px;
		margin: 0.5rem 0;
	}

	.timer-svg {
		width: 80px;
		height: 80px;
		transform: rotate(-90deg);
	}

	.track {
		fill: none;
		stroke: var(--color-surface);
		stroke-width: 4;
	}

	.progress {
		fill: none;
		stroke: var(--color-accent);
		stroke-width: 4;
		stroke-dasharray: 226.2;
		stroke-linecap: round;
		transition: stroke-dashoffset 1s linear, stroke 0.3s ease;
	}

	.timer-urgent .progress {
		stroke: #ff6b6b;
	}

	.timer-text {
		position: absolute;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		font-size: 1.3rem;
		font-weight: 700;
		color: var(--color-text);
	}

	.timer-urgent .timer-text {
		color: #ff6b6b;
		animation: urgentPulse 1s ease-in-out infinite;
	}

	@keyframes urgentPulse {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.6; }
	}

	/* Actions */
	.actions {
		display: flex;
		gap: 1rem;
		width: 100%;
		max-width: 320px;
		margin-top: 0.5rem;
	}

	.accept-btn, .decline-btn {
		flex: 1;
		padding: 0.85rem 1.5rem;
		font-size: 1rem;
		font-weight: 700;
		border-radius: var(--radius-md);
		transition: all var(--transition-fast);
	}

	.accept-btn {
		background: var(--color-accent);
		color: #0a0a0a;
	}

	.accept-btn:hover:not(:disabled) {
		background: var(--color-accent-hover);
		transform: translateY(-1px);
		box-shadow: 0 4px 20px rgba(0, 212, 170, 0.3);
	}

	.accept-btn:disabled {
		background: var(--color-surface);
		color: var(--color-text-muted);
		border: 1px solid var(--color-accent-border);
	}

	.decline-btn {
		border: 1px solid var(--color-border);
		background: var(--color-surface);
		color: var(--color-text-muted);
	}

	.decline-btn:hover {
		border-color: var(--color-border-hover);
		color: var(--color-text);
	}
</style>
