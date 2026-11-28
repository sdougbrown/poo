#!/usr/bin/env node
// Generates docs/public/llms.txt and docs/public/llms-full.txt from the docs tree.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const docsRoot = path.resolve(__dirname, '..')
const publicDir = path.join(docsRoot, 'public')
const baseUrl = 'https://poo.douggo.com'

// Every doc page must be registered both here and in docs/.vitepress/config.mts.
// Missing or unregistered pages fail docs:build instead of being silently omitted.
const sections = [
  {
    title: 'Getting Started',
    pages: [
      'getting-started/why-poo.md',
      'getting-started/install.md',
      'getting-started/quickstart.md',
      'getting-started/repl.md',
    ],
  },
  {
    title: 'Configuration & Models',
    pages: [
      'configuration/settings-discovery.md',
      'configuration/models-and-catalog.md',
      'configuration/reasoning-and-sampling.md',
      'configuration/providers-and-auth.md',
      'configuration/profiles-and-prompts.md',
    ],
  },
  {
    title: 'Capabilities',
    pages: [
      'capabilities/feeds-and-daemon.md',
      'capabilities/compaction.md',
      'capabilities/decision-model.md',
      'capabilities/cli-registry.md',
      'capabilities/mcp-servers.md',
      'capabilities/codemode.md',
      'capabilities/skills.md',
      'capabilities/tools-and-jobs.md',
    ],
  },
  {
    title: 'Safety & Operations',
    pages: [
      'operations/permissions-and-safety.md',
      'operations/sandbox.md',
      'operations/sessions-and-durability.md',
      'operations/limits-and-exit-codes.md',
    ],
  },
  {
    title: 'Extending & Embedding',
    pages: [
      'extending/architecture.md',
      'extending/custom-binary.md',
      'extending/custom-tools-and-prompts.md',
      'extending/wire-protocol.md',
      'extending/conformance-and-testing.md',
      'extending/prior-art.md',
    ],
  },
  {
    title: 'Reference',
    pages: [
      'reference/cli.md',
      'reference/settings.md',
      'reference/models-file.md',
      'reference/manifests.md',
      'reference/session-log-v0.md',
      'reference/wire-api-mapping.md',
      'reference/evals.md',
    ],
  },
]

function parseFrontmatterAndBody(raw) {
  let frontmatter = {}
  let body = raw
  if (raw.startsWith('---\n')) {
    const end = raw.indexOf('\n---\n', 4)
    if (end !== -1) {
      const fmBlock = raw.slice(4, end)
      for (const line of fmBlock.split('\n')) {
        const idx = line.indexOf(':')
        if (idx > 0) {
          const k = line.slice(0, idx).trim()
          const v = line.slice(idx + 1).trim().replace(/^["']|["']$/g, '')
          frontmatter[k] = v
        }
      }
      body = raw.slice(end + 5).trimStart()
    }
  }
  return { frontmatter, body }
}

function extractTitle(frontmatter, body, relPath) {
  if (frontmatter.title) return frontmatter.title
  const match = body.match(/^#\s+(.+)$/m)
  if (match) return match[1].trim()
  return path.basename(relPath, '.md')
}

function extractSummary(frontmatter, body) {
  if (frontmatter.description) return frontmatter.description
  const lines = body.split('\n')
  const para = []
  let inCode = false
  for (const line of lines) {
    if (line.trim().startsWith('```')) {
      inCode = !inCode
      continue
    }
    if (inCode) continue
    const t = line.trim()
    if (!t) {
      if (para.length > 0) break
      continue
    }
    if (t.startsWith('#') || t.startsWith('>') || t.startsWith('|') || t.startsWith(':::')) {
      if (para.length > 0) break
      continue
    }
    para.push(t)
  }
  return para.join(' ').slice(0, 220)
}

const llmsLines = [
  '# poo',
  '',
  '> Modular Go agent harness — headless CLI and embeddable Go library built for engineered agents.',
  '',
  'Full concatenated documentation: ' + `${baseUrl}/llms-full.txt`,
  '',
]

const fullChunks = [
  '# poo — Complete Documentation',
  '',
  `Source: ${baseUrl}`,
  '',
]

function listDocPages(dir, prefix = '') {
  const found = []
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === 'node_modules' || entry.name === '.vitepress' || entry.name === 'public' || entry.name === 'scripts') {
      continue
    }
    const rel = prefix ? `${prefix}/${entry.name}` : entry.name
    if (entry.isDirectory()) {
      found.push(...listDocPages(path.join(dir, entry.name), rel))
    } else if (entry.name.endsWith('.md') && rel !== 'index.md') {
      found.push(rel)
    }
  }
  return found.sort()
}

const registered = new Set()
const missing = []
for (const section of sections) {
  for (const relPath of section.pages) {
    registered.add(relPath)
    if (!fs.existsSync(path.join(docsRoot, relPath))) {
      missing.push(relPath)
    }
  }
}
const unregistered = listDocPages(docsRoot).filter((relPath) => !registered.has(relPath))
if (missing.length > 0 || unregistered.length > 0) {
  if (missing.length > 0) {
    console.error(`generate-llms: registered pages not found on disk: ${missing.join(', ')}`)
  }
  if (unregistered.length > 0) {
    console.error(`generate-llms: doc pages missing from sections list: ${unregistered.join(', ')}`)
  }
  process.exit(1)
}

for (const section of sections) {
  if (section.pages.length === 0) continue

  llmsLines.push(`## ${section.title}`, '')
  for (const relPath of section.pages) {
    const abs = path.join(docsRoot, relPath)
    const raw = fs.readFileSync(abs, 'utf8')
    const { frontmatter, body } = parseFrontmatterAndBody(raw)
    const title = extractTitle(frontmatter, body, relPath)
    const summary = extractSummary(frontmatter, body)
    const urlPath = '/' + relPath.replace(/\.md$/, '')
    llmsLines.push(`- [${title}](${baseUrl}${urlPath})${summary ? `: ${summary}` : ''}`)

    fullChunks.push('---', '', `<!-- Source: ${relPath} -->`, '', body.trim(), '')
  }
  llmsLines.push('')
}

fs.mkdirSync(publicDir, { recursive: true })
fs.writeFileSync(path.join(publicDir, 'llms.txt'), llmsLines.join('\n'), 'utf8')
fs.writeFileSync(path.join(publicDir, 'llms-full.txt'), fullChunks.join('\n'), 'utf8')
console.log(`Generated llms.txt and llms-full.txt (${fullChunks.length} blocks)`)
