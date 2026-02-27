<script lang="ts">
	import { app } from '$lib/stores.svelte';

	let elapsed = $state(0);
	let intervalId: ReturnType<typeof setInterval> | null = null;

	let remaining = $derived(Math.max(0, app.matchTimeout - elapsed));

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

<div class="matching">
	<div class="spinner-container">
		<div class="spinner"></div>
		<div class="pulse"></div>
	</div>

	<h2 class="title">Looking for someone...</h2>
	<p class="subtitle">Finding someone who shares your interests</p>

	<div class="timer" class:timer-urgent={remaining <= 10}>
		{remaining}s remaining
	</div>

	<div class="progress-track">
		<div
			class="progress-bar"
			style="width: {app.matchTimeout > 0 ? ((app.matchTimeout - remaining) / app.matchTimeout) * 100 : 0}%"
		></div>
	</div>

	<button class="cancel-btn" onclick={() => app.cancelMatching()}>
		Cancel
	</button>
</div>

<style>
	.matching {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		min-height: 80dvh;
		padding: 2rem 1.25rem;
		text-align: center;
		gap: 1rem;
	}

	.spinner-container {
		position: relative;
		width: 80px;
		height: 80px;
		margin-bottom: 1rem;
	}

	.spinner {
		width: 80px;
		height: 80px;
		border: 3px solid var(--color-border);
		border-top-color: var(--color-accent);
		border-radius: 50%;
		animation: spin 1s linear infinite;
	}

	.pulse {
		position: absolute;
		top: 50%;
		left: 50%;
		transform: translate(-50%, -50%);
		width: 40px;
		height: 40px;
		background: var(--color-accent-muted);
		border-radius: 50%;
		animation: pulse 2s ease-in-out infinite;
	}

	@keyframes spin {
		to { transform: rotate(360deg); }
	}

	@keyframes pulse {
		0%, 100% { transform: translate(-50%, -50%) scale(1); opacity: 0.6; }
		50% { transform: translate(-50%, -50%) scale(1.3); opacity: 0.2; }
	}

	.title {
		font-size: 1.5rem;
		font-weight: 700;
		color: var(--color-text);
	}

	.subtitle {
		color: var(--color-text-muted);
		font-size: 0.95rem;
	}

	.timer {
		font-size: 1.1rem;
		font-weight: 600;
		color: var(--color-accent);
		padding: 0.35rem 1rem;
		border-radius: var(--radius-full);
		background: var(--color-accent-muted);
		border: 1px solid var(--color-accent-border);
		transition: all var(--transition-normal);
	}

	.timer-urgent {
		color: #ff6b6b;
		background: rgba(255, 107, 107, 0.12);
		border-color: rgba(255, 107, 107, 0.3);
		animation: urgentPulse 1s ease-in-out infinite;
	}

	@keyframes urgentPulse {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.7; }
	}

	.progress-track {
		width: 100%;
		max-width: 300px;
		height: 4px;
		background: var(--color-surface);
		border-radius: 2px;
		overflow: hidden;
	}

	.progress-bar {
		height: 100%;
		background: var(--color-accent);
		border-radius: 2px;
		transition: width 1s linear;
	}

	.cancel-btn {
		margin-top: 1.5rem;
		padding: 0.75rem 2.5rem;
		font-size: 0.95rem;
		font-weight: 600;
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
		background: var(--color-surface);
		color: var(--color-text-muted);
		transition: all var(--transition-fast);
	}

	.cancel-btn:hover {
		border-color: var(--color-border-hover);
		color: var(--color-text);
		background: var(--color-bg-hover);
	}
</style>
