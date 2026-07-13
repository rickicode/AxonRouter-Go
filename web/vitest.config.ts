import { defineConfig } from 'vitest/config';
import { resolve } from 'path';

export default defineConfig({
	resolve: {
		alias: {
			$lib: resolve(__dirname, './src/lib'),
		},
	},
	test: {
		environment: 'happy-dom',
		globals: true,
	},
});
