<script lang="ts">
	import { app } from '$lib/stores.svelte';

	let remaining = $state(app.banDuration);

	$effect(() => {
		remaining = app.banDuration;
		const interval = setInterval(() => {
			remaining = Math.max(0, remaining - 1);
			if (remaining <= 0) {
				clearInterval(interval);
				app.isBanned = false;
				app.screen = 'idle';
			}
		}, 1000);
		return () => clearInterval(interval);
	});

	function formatDuration(seconds: number): string {
		const h = Math.floor(seconds / 3600);
		const m = Math.floor((seconds % 3600) / 60);
		const s = seconds % 60;
		if (h > 0) return `${h}h ${m}m ${s}s`;
		if (m > 0) return `${m}m ${s}s`;
		return `${s}s`;
	}
</script>

<div class="banned">
	<div class="icon">
		<svg width="56" height="56" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
			<circle cx="12" cy="12" r="10" />
			<line x1="4.93" y1="4.93" x2="19.07" y2="19.07" />
		</svg>
	</div>

	<h2 class="title">You have been banned</h2>

	{#if app.banReason}
		<p class="reason">Reason: {app.banReason}</p>
	{/if}

	<div class="countdown">
		<span class="countdown-label">Time remaining</span>
		<span class="countdown-value">{formatDuration(remaining)}</span>
	</div>

	<p class="notice">Please wait until the ban expires to use Whisper again.</p>
</div>

<style>
	.banned {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		min-height: 80dvh;
		padding: 2rem 1.25rem;
		text-align: center;
		gap: 1rem;
		animation: fadeIn 0.3s ease-out;
	}

	@keyframes fadeIn {
		from { opacity: 0; transform: translateY(10px); }
		to { opacity: 1; transform: translateY(0); }
	}

	.icon {
		color: #ff6b6b;
		margin-bottom: 0.5rem;
	}

	.title {
		font-size: 1.5rem;
		font-weight: 700;
		color: #ff6b6b;
	}

	.reason {
		color: var(--color-text-muted);
		font-size: 0.95rem;
		max-width: 350px;
		padding: 0.5rem 1rem;
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-md);
	}

	.countdown {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 0.35rem;
		margin-top: 0.5rem;
		padding: 1rem 2rem;
		background: var(--color-bg-elevated);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
	}

	.countdown-label {
		font-size: 0.8rem;
		font-weight: 500;
		color: var(--color-text-dimmed);
		text-transform: uppercase;
		letter-spacing: 0.05em;
	}

	.countdown-value {
		font-size: 2rem;
		font-weight: 800;
		color: var(--color-text);
		font-variant-numeric: tabular-nums;
		letter-spacing: -0.02em;
	}

	.notice {
		color: var(--color-text-dimmed);
		font-size: 0.85rem;
		max-width: 300px;
		margin-top: 0.5rem;
	}
</style>
