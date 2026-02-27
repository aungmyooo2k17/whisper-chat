<script lang="ts">
	import { app } from '$lib/stores.svelte';

	let remaining = $state(app.rateLimitRetryAfter);

	$effect(() => {
		remaining = app.rateLimitRetryAfter;
		const interval = setInterval(() => {
			remaining = Math.max(0, remaining - 1);
			if (remaining <= 0) {
				clearInterval(interval);
			}
		}, 1000);
		return () => clearInterval(interval);
	});
</script>

{#if app.isRateLimited}
	<div class="toast" role="alert">
		<svg class="toast-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
			<circle cx="12" cy="12" r="10" />
			<polyline points="12 6 12 12 16 14" />
		</svg>
		<span class="toast-text">Slow down! Try again in {remaining}s</span>
	</div>
{/if}

<style>
	.toast {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.5rem 1rem;
		background: rgba(255, 171, 0, 0.12);
		border: 1px solid rgba(255, 171, 0, 0.3);
		border-radius: var(--radius-md);
		color: #ffab00;
		font-size: 0.8rem;
		font-weight: 600;
		animation: slideIn 0.25s ease-out;
	}

	@keyframes slideIn {
		from { opacity: 0; transform: translateY(-8px); }
		to { opacity: 1; transform: translateY(0); }
	}

	.toast-icon {
		flex-shrink: 0;
	}

	.toast-text {
		white-space: nowrap;
	}
</style>
