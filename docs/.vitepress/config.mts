import { defineConfig } from 'vitepress'

export default defineConfig({
  title: 'poo',
  description: 'A modular Go agent harness — headless CLI and embeddable library for engineered agents.',
  lang: 'en-US',
  cleanUrls: true,
  lastUpdated: true,

  head: [
    ['meta', { name: 'theme-color', content: '#3c8772' }],
  ],

  markdown: {
    config(md) {
      const defaultCodeInline = md.renderer.rules.code_inline!
      md.renderer.rules.code_inline = (tokens, idx, options, env, self) => {
        const rendered = defaultCodeInline(tokens, idx, options, env, self)
        return rendered.includes('{{') ? rendered.replace('<code', '<code v-pre') : rendered
      }
    },
  },

  themeConfig: {
    siteTitle: 'poo',
    search: {
      provider: 'local',
    },

    nav: [
      { text: 'Getting Started', link: '/getting-started/quickstart' },
      { text: 'Configuration', link: '/configuration/models-and-catalog' },
      { text: 'Capabilities', link: '/capabilities/feeds-and-daemon' },
      { text: 'Extending', link: '/extending/architecture' },
      { text: 'Reference', link: '/reference/settings' },
    ],

    sidebar: [
      {
        text: 'Getting Started',
        items: [
          { text: 'Why poo?', link: '/getting-started/why-poo' },
          { text: 'Install', link: '/getting-started/install' },
          { text: 'Quickstart', link: '/getting-started/quickstart' },
          { text: 'Interactive REPL', link: '/getting-started/repl' },
        ],
      },
      {
        text: 'Configuration & Models',
        items: [
          { text: 'Settings Discovery', link: '/configuration/settings-discovery' },
          { text: 'Models & Catalog', link: '/configuration/models-and-catalog' },
          { text: 'Reasoning & Sampling', link: '/configuration/reasoning-and-sampling' },
          { text: 'Providers & Auth', link: '/configuration/providers-and-auth' },
          { text: 'Profiles & Prompts', link: '/configuration/profiles-and-prompts' },
        ],
      },
      {
        text: 'Capabilities',
        items: [
          { text: 'Feeds & Daemon', link: '/capabilities/feeds-and-daemon' },
          { text: 'Context Compaction', link: '/capabilities/compaction' },
          { text: 'Decision Model', link: '/capabilities/decision-model' },
          { text: 'CLI Registry & Script Tools', link: '/capabilities/cli-registry' },
          { text: 'MCP Servers', link: '/capabilities/mcp-servers' },
          { text: 'Codemode', link: '/capabilities/codemode' },
          { text: 'Skills', link: '/capabilities/skills' },
          { text: 'Built-in Tools & Jobs', link: '/capabilities/tools-and-jobs' },
        ],
      },
      {
        text: 'Safety & Operations',
        items: [
          { text: 'Permissions & Safety', link: '/operations/permissions-and-safety' },
          { text: 'Sandboxing (bwrap)', link: '/operations/sandbox' },
          { text: 'Sessions & Durability', link: '/operations/sessions-and-durability' },
          { text: 'Limits & Exit Codes', link: '/operations/limits-and-exit-codes' },
        ],
      },
      {
        text: 'Extending & Embedding',
        items: [
          { text: 'Architecture', link: '/extending/architecture' },
          { text: 'Build Your Own Binary', link: '/extending/custom-binary' },
          { text: 'Custom Tools & Prompts', link: '/extending/custom-tools-and-prompts' },
          { text: 'Wire Protocol', link: '/extending/wire-protocol' },
          { text: 'Conformance & Testing', link: '/extending/conformance-and-testing' },
          { text: 'Prior Art', link: '/extending/prior-art' },
        ],
      },
      {
        text: 'Reference',
        items: [
          { text: 'CLI Reference', link: '/reference/cli' },
          { text: 'Settings Reference', link: '/reference/settings' },
          { text: 'Models File Reference', link: '/reference/models-file' },
          { text: 'Manifests Reference', link: '/reference/manifests' },
          { text: 'Session Log v0', link: '/reference/session-log-v0' },
          { text: 'Wire API Mapping', link: '/reference/wire-api-mapping' },
          { text: 'Evals & Benchmarks', link: '/reference/evals' },
        ],
      },
    ],

    socialLinks: [
      { icon: 'github', link: 'https://github.com/sdougbrown/poo' },
    ],

    editLink: {
      pattern: 'https://github.com/sdougbrown/poo/edit/main/docs/:path',
      text: 'Edit this page on GitHub',
    },

    footer: {
      message: 'Released under the Apache-2.0 License. Machine-readable index: <a href="/llms.txt">llms.txt</a> · <a href="/llms-full.txt">llms-full.txt</a>',
      copyright: 'poo — modular Go agent harness',
    },
  },
})
