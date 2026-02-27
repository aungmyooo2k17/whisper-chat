<script lang="ts">
	import { app } from '$lib/stores.svelte';
</script>

<div class="ended">
	<div class="icon">
		{#if app.partnerLeft}
			<svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
				<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
				<polyline points="16 17 21 12 16 7" />
				<line x1="21" y1="12" x2="9" y2="12" />
			</svg>
		{:else}
			<svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
				<circle cx="12" cy="12" r="10" />
				<line x1="8" y1="12" x2="16" y2="12" />
			</svg>
		{/if}
	</div>

	<h2 class="title">
		{#if app.partnerLeft}
			Your partner left
		{:else}
			Chat ended
		{/if}
	</h2>

	<p class="subtitle">
		{#if app.partnerLeft}
			Your chat partner has disconnected.
		{:else}
			You ended the conversation.
		{/if}
	</p>

	<button class="new-match-btn" onclick={() => app.findNewMatch()}>
		Find New Match
	</button>
</div>

<style>
	.ended {
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
		color: var(--color-text-dimmed);
		margin-bottom: 0.5rem;
	}

	.title {
		font-size: 1.5rem;
		font-weight: 700;
		color: var(--color-text);
	}

	.subtitle {
		color: var(--color-text-muted);
		font-size: 0.95rem;
		max-width: 300px;
	}

	.new-match-btn {
		margin-top: 1.5rem;
		padding: 0.85rem 2.5rem;
		font-size: 1rem;
		font-weight: 700;
		border-radius: var(--radius-md);
		background: var(--color-accent);
		color: #0a0a0a;
		transition: all var(--transition-fast);
	}

	.new-match-btn:hover {
		background: var(--color-accent-hover);
		transform: translateY(-1px);
		box-shadow: 0 4px 20px rgba(0, 212, 170, 0.3);
	}

	.new-match-btn:active {
		transform: translateY(0);
	}
</style>
