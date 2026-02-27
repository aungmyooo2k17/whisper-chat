<script lang="ts">
	import { app, ws } from '$lib/stores.svelte';

	let { onClose }: { onClose: () => void } = $props();

	let reason = $state('');
	let submitting = $state(false);
	let submitted = $state(false);

	const reasons = [
		{ value: 'harassment', label: 'Harassment' },
		{ value: 'spam', label: 'Spam' },
		{ value: 'explicit', label: 'Explicit Content' },
		{ value: 'other', label: 'Other' },
	];

	function submitReport() {
		if (!reason || !app.chatId) return;
		submitting = true;
		ws.report(app.chatId, reason);
		submitted = true;
		submitting = false;
		setTimeout(onClose, 1500);
	}

	function handleBackdropClick(e: MouseEvent) {
		if (e.target === e.currentTarget) {
			onClose();
		}
	}

	function handleKeyDown(e: KeyboardEvent) {
		if (e.key === 'Escape') {
			onClose();
		}
	}
</script>

<svelte:window onkeydown={handleKeyDown} />

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="overlay" onclick={handleBackdropClick}>
	<div class="dialog" role="dialog" aria-modal="true" aria-labelledby="report-title">
		{#if submitted}
			<div class="confirmation">
				<div class="check-icon">&#10003;</div>
				<p class="confirmation-text">Report submitted</p>
				<p class="confirmation-sub">Thank you for helping keep the community safe.</p>
			</div>
		{:else}
			<h2 id="report-title" class="dialog-title">Report User</h2>
			<p class="dialog-desc">Select a reason for reporting this user.</p>

			<div class="reasons">
				{#each reasons as r (r.value)}
					<button
						class="reason-btn"
						class:reason-selected={reason === r.value}
						onclick={() => (reason = r.value)}
						type="button"
					>
						<span class="radio" class:radio-checked={reason === r.value}></span>
						{r.label}
					</button>
				{/each}
			</div>

			<div class="actions">
				<button class="cancel-btn" onclick={onClose} type="button">
					Cancel
				</button>
				<button
					class="submit-btn"
					disabled={!reason || submitting}
					onclick={submitReport}
					type="button"
				>
					{submitting ? 'Submitting...' : 'Submit Report'}
				</button>
			</div>
		{/if}
	</div>
</div>

<style>
	.overlay {
		position: fixed;
		inset: 0;
		z-index: 100;
		display: flex;
		align-items: center;
		justify-content: center;
		background: rgba(0, 0, 0, 0.6);
		backdrop-filter: blur(4px);
		padding: 1rem;
	}

	.dialog {
		width: 100%;
		max-width: 400px;
		background: var(--color-bg-elevated);
		border: 1px solid var(--color-border);
		border-radius: var(--radius-lg);
		padding: 1.5rem;
	}

	.dialog-title {
		font-size: 1.1rem;
		font-weight: 700;
		color: var(--color-text);
		margin-bottom: 0.25rem;
	}

	.dialog-desc {
		font-size: 0.85rem;
		color: var(--color-text-muted);
		margin-bottom: 1.25rem;
	}

	/* Reason buttons */
	.reasons {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		margin-bottom: 1.5rem;
	}

	.reason-btn {
		display: flex;
		align-items: center;
		gap: 0.75rem;
		width: 100%;
		padding: 0.7rem 0.9rem;
		font-size: 0.9rem;
		font-weight: 500;
		border-radius: var(--radius-md);
		border: 1px solid var(--color-border);
		background: var(--color-bg);
		color: var(--color-text-muted);
		text-align: left;
		transition: all var(--transition-fast);
	}

	.reason-btn:hover {
		border-color: var(--color-border-hover);
		background: var(--color-bg-hover);
		color: var(--color-text);
	}

	.reason-selected {
		border-color: var(--color-accent-border);
		background: var(--color-accent-muted);
		color: var(--color-accent);
	}

	.reason-selected:hover {
		border-color: var(--color-accent);
		color: var(--color-accent);
	}

	/* Radio indicator */
	.radio {
		width: 16px;
		height: 16px;
		border-radius: 50%;
		border: 2px solid var(--color-border-hover);
		flex-shrink: 0;
		position: relative;
		transition: border-color var(--transition-fast);
	}

	.radio-checked {
		border-color: var(--color-accent);
	}

	.radio-checked::after {
		content: '';
		position: absolute;
		top: 3px;
		left: 3px;
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--color-accent);
	}

	/* Action buttons */
	.actions {
		display: flex;
		gap: 0.75rem;
		justify-content: flex-end;
	}

	.cancel-btn {
		padding: 0.55rem 1rem;
		font-size: 0.85rem;
		font-weight: 600;
		border-radius: var(--radius-sm);
		border: 1px solid var(--color-border);
		background: var(--color-surface);
		color: var(--color-text-muted);
		transition: all var(--transition-fast);
	}

	.cancel-btn:hover {
		border-color: var(--color-border-hover);
		color: var(--color-text);
	}

	.submit-btn {
		padding: 0.55rem 1rem;
		font-size: 0.85rem;
		font-weight: 600;
		border-radius: var(--radius-sm);
		border: 1px solid rgba(255, 107, 107, 0.3);
		background: rgba(255, 107, 107, 0.1);
		color: #ff6b6b;
		transition: all var(--transition-fast);
	}

	.submit-btn:hover:not(:disabled) {
		background: rgba(255, 107, 107, 0.2);
		border-color: rgba(255, 107, 107, 0.5);
	}

	.submit-btn:disabled {
		opacity: 0.4;
		cursor: not-allowed;
	}

	/* Confirmation state */
	.confirmation {
		text-align: center;
		padding: 1rem 0;
	}

	.check-icon {
		width: 48px;
		height: 48px;
		border-radius: 50%;
		background: var(--color-accent-muted);
		color: var(--color-accent);
		font-size: 1.5rem;
		font-weight: 700;
		display: flex;
		align-items: center;
		justify-content: center;
		margin: 0 auto 1rem;
	}

	.confirmation-text {
		font-size: 1rem;
		font-weight: 600;
		color: var(--color-text);
		margin-bottom: 0.25rem;
	}

	.confirmation-sub {
		font-size: 0.85rem;
		color: var(--color-text-muted);
	}
</style>
