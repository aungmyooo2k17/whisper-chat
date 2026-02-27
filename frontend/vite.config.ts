import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [sveltekit()],
	server: {
		proxy: {
			'/ws': {
				target: 'http://localhost:8080',
				ws: true,
				changeOrigin: true,
				configure: (proxy) => {
					proxy.on('error', (err) => {
						console.log('[vite-proxy] WebSocket proxy error:', err.message);
					});
				}
			}
		}
	}
});
