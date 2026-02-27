<script lang="ts">
	import { categories, MIN_INTERESTS, MAX_INTERESTS } from '$lib/interests';
	import { app } from '$lib/stores.svelte';
	import MatchingScreen from '$lib/components/MatchingScreen.svelte';
	import MatchFoundScreen from '$lib/components/MatchFoundScreen.svelte';
	import ChatScreen from '$lib/components/ChatScreen.svelte';
	import ChatEndedScreen from '$lib/components/ChatEndedScreen.svelte';

	let selectedTags: Set<string> = $state(new Set());

	let selectedCount = $derived(selectedTags.size);
	let isMaxSelected = $derived(selectedCount >= MAX_INTERESTS);
	let canStart = $derived(selectedCount >= MIN_INTERESTS);

	function toggleTag(tag: string) {
		const next = new Set(selectedTags);
		if (next.has(tag)) {
			next.delete(tag);
		} else if (!isMaxSelected) {
			next.add(tag);
		}
		selectedTags = next;
	}

	function isSelected(tag: string): boolean {
		return selectedTags.has(tag);
	}

	function isDimmed(tag: string): boolean {
		return isMaxSelected && !selectedTags.has(tag);
	}

	function startMatching() {
		const interests = Array.from(selectedTags);
		app.startMatching(interests);
	}
</script>

<svelte:head>
	<title>Whisper - Anonymous Chat</title>
	<meta name="description" content="Anonymous real-time chat with people who share your interests" />
</svelte:head>

{#if app.screen === 'matching'}
	<MatchingScreen />
{:else if app.screen === 'match_found'}
	<MatchFoundScreen />
{:else if app.screen === 'chatting'}
	<ChatScreen />
{:else if app.screen === 'chat_ended'}
	<ChatEndedScreen />
{:else}
	<!-- idle: landing / interest selection -->
	<main class="landing">
		<header class="hero">
			<h1 class="logo">Whisper</h1>
			<p class="tagline">Anonymous real-time chat with people who share your interests</p>
		</header>

		<section class="interests-section">
			<div class="selection-header">
				<h2 class="section-title">Choose your interests</h2>
				<span class="counter" class:counter-active={selectedCount > 0}>
					{selectedCount}/{MAX_INTERESTS} selected
				</span>
			</div>

			<div class="categories">
				{#each categories as category (category.name)}
					<div class="category">
						<h3 class="category-name">
							<span class="category-icon">{category.icon}</span>
							{category.name}
						</h3>
						<div class="tags">
							{#each category.tags as tag (tag)}
								<button
									class="tag"
									class:tag-selected={isSelected(tag)}
									class:tag-dimmed={isDimmed(tag)}
									disabled={isDimmed(tag)}
									onclick={() => toggleTag(tag)}
									aria-pressed={isSelected(tag)}
								>
									{tag}
								</button>
							{/each}
						</div>
					</div>
				{/each}
			</div>
		</section>

		<div class="action-bar">
			<button
				class="start-button"
				disabled={!canStart}
				onclick={startMatching}
			>
				{#if canStart}
					Start Matching ({selectedCount} {selectedCount === 1 ? 'interest' : 'interests'})
				{:else}
					Select at least {MIN_INTERESTS} interest to start
				{/if}
			</button>
		</div>
	</main>
{/if}

<style>
	.landing {
		max-width: 720px;
		margin: 0 auto;
		padding: 2rem 1.25rem 8rem;
		min-height: 100dvh;
	}

	/* --- Hero --- */
	.hero {
		text-align: center;
		padding: 3rem 0 2.5rem;
	}

	.logo {
		font-size: 3.5rem;
		font-weight: 800;
		letter-spacing: -0.03em;
		background: linear-gradient(135deg, var(--color-accent), #00f0d4);
		background-clip: text;
		-webkit-background-clip: text;
		-webkit-text-fill-color: transparent;
		margin-bottom: 0.5rem;
	}

	.tagline {
		color: var(--color-text-muted);
		font-size: 1.05rem;
		max-width: 400px;
		margin: 0 auto;
		line-height: 1.5;
	}

	/* --- Interests Section --- */
	.interests-section {
		margin-top: 1rem;
	}

	.selection-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 1.5rem;
	}

	.section-title {
		font-size: 1.15rem;
		font-weight: 600;
		color: var(--color-text);
	}

	.counter {
		font-size: 0.85rem;
		color: var(--color-text-dimmed);
		font-weight: 500;
		padding: 0.25rem 0.75rem;
		border-radius: var(--radius-full);
		background: var(--color-surface);
		border: 1px solid var(--color-border);
		transition: all var(--transition-normal);
	}

	.counter-active {
		color: var(--color-accent);
		border-color: var(--color-accent-border);
		background: var(--color-accent-muted);
	}

	/* --- Categories --- */
	.categories {
		display: flex;
		flex-direction: column;
		gap: 1.5rem;
	}

	.category {
		background: var(--color-bg-elevated);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: 1rem 1.25rem;
	}

	.category-name {
		font-size: 0.9rem;
		font-weight: 600;
		color: var(--color-text-muted);
		margin-bottom: 0.75rem;
		display: flex;
		align-items: center;
		gap: 0.5rem;
	}

	.category-icon {
		font-size: 1.1rem;
	}

	/* --- Tags --- */
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: 0.5rem;
	}

	.tag {
		padding: 0.4rem 0.85rem;
		font-size: 0.85rem;
		font-weight: 500;
		border-radius: var(--radius-full);
		border: 1px solid var(--color-border);
		background: var(--color-bg);
		color: var(--color-text-muted);
		transition: all var(--transition-fast);
		user-select: none;
		-webkit-user-select: none;
	}

	.tag:hover:not(:disabled) {
		border-color: var(--color-border-hover);
		background: var(--color-bg-hover);
		color: var(--color-text);
	}

	.tag-selected {
		background: var(--color-accent-muted);
		border-color: var(--color-accent-border);
		color: var(--color-accent);
	}

	.tag-selected:hover:not(:disabled) {
		background: rgba(0, 212, 170, 0.22);
		border-color: var(--color-accent);
		color: var(--color-accent-hover);
	}

	.tag-dimmed {
		opacity: 0.35;
	}

	/* --- Action Bar --- */
	.action-bar {
		position: fixed;
		bottom: 0;
		left: 0;
		right: 0;
		padding: 1rem 1.25rem;
		padding-bottom: max(1rem, env(safe-area-inset-bottom));
		background: linear-gradient(to top, var(--color-bg) 60%, transparent);
		display: flex;
		justify-content: center;
		z-index: 10;
	}

	.start-button {
		width: 100%;
		max-width: 720px;
		padding: 1rem 2rem;
		font-size: 1.05rem;
		font-weight: 700;
		border-radius: var(--radius-md);
		background: var(--color-accent);
		color: #0a0a0a;
		transition: all var(--transition-fast);
		letter-spacing: -0.01em;
	}

	.start-button:hover:not(:disabled) {
		background: var(--color-accent-hover);
		transform: translateY(-1px);
		box-shadow: 0 4px 20px rgba(0, 212, 170, 0.3);
	}

	.start-button:active:not(:disabled) {
		transform: translateY(0);
	}

	.start-button:disabled {
		background: var(--color-surface);
		color: var(--color-text-dimmed);
		border: 1px solid var(--color-border);
	}

	/* --- Responsive --- */
	@media (min-width: 640px) {
		.landing {
			padding: 3rem 2rem 8rem;
		}

		.hero {
			padding: 4rem 0 3rem;
		}

		.logo {
			font-size: 4.5rem;
		}

		.tagline {
			font-size: 1.15rem;
		}
	}
</style>
