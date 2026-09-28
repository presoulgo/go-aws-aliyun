import { fileURLToPath, URL } from 'node:url'
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { gzipSync } from 'node:zlib'
import { defineConfig, type Plugin } from 'vite'
import vue from '@vitejs/plugin-vue'

const compressible = /\.(js|css|svg|json|html)$/

// goEmbed prepares dist/ for `go:embed`: it restores the .gitkeep placeholder
// removed by emptyOutDir and writes a .gz copy of larger text assets, which
// the Go server sends to browsers that accept gzip.
function goEmbed(): Plugin {
  let outDir = ''
  return {
    name: 'go-embed',
    apply: 'build',
    configResolved(config) {
      outDir = config.build.outDir.startsWith('/') ? config.build.outDir : join(config.root, config.build.outDir)
    },
    closeBundle() {
      const walk = (dir: string) => {
        for (const name of readdirSync(dir)) {
          const file = join(dir, name)
          if (statSync(file).isDirectory()) {
            walk(file)
          } else if (compressible.test(name) && name !== 'index.html' && statSync(file).size >= 1024) {
            writeFileSync(file + '.gz', gzipSync(readFileSync(file), { level: 9 }))
          }
        }
      }
      walk(outDir)
      writeFileSync(join(outDir, '.gitkeep'), '')
    },
  }
}

export default defineConfig({
  plugins: [vue(), goEmbed()],
  resolve: {
    alias: { '@': fileURLToPath(new URL('./src', import.meta.url)) },
  },
  server: {
    port: 5173,
    proxy: { '/api': 'http://127.0.0.1:8080' },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    target: 'es2022',
    chunkSizeWarningLimit: 1600,
  },
})
