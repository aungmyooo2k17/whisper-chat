<script lang="ts">
	import { onMount } from 'svelte';

	let count = $state<number | null>(null);

	const API_URL =
		import.meta.env.VITE_API_URL ||
		`${window.location.protocol}//${window.location.host}`;

	async function fetchCount() {
		try {
			const res = await fetch(`${API_URL}/api/online`);
			if (res.ok) {
				const data = await res.json();
				count = data.count;
			}
		} catch {
			// Silently fail — counter is non-critical
		}
	}

	onMount(() => {
		fetchCount();
		const interval = setInterval(fetchCount, 10_000);
		return () => clearInterval(interval);
	});
</script>

{#if count !== null}
	<div class="online-counter">
		<span class="online-dot"></span>
		<span class="online-text">{count.toLocaleString()} {count === 1 ? 'user' : 'users'} online</span>
	</div>
{/if}

<style>
	.online-counter {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 0.5rem;
		padding: 0.4rem 0;
	}

	.online-dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: #22c55e;
		box-shadow: 0 0 6px rgba(34, 197, 94, 0.4);
		animation: pulse 2s ease-in-out infinite;
	}

	@keyframes pulse {
		0%, 100% { opacity: 1; }
		50% { opacity: 0.5; }
	}

	.online-text {
		font-size: 0.85rem;
		color: var(--color-text-dimmed);
		font-weight: 500;
	}
</style>
