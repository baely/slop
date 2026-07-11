/* taste — design language picker for bailey's slop apps.
   Every pick writes CSS custom properties onto :root, so the whole page
   (chrome included) wears the current language. Variant cards override
   only their own dimension's vars locally, giving "current language,
   but with this option" in context. */

(() => {

/* ---------- palettes ---------- */

const ACCENTS = {
  orange:  { hex: '#ff4d00', ink: '#ffffff', label: 'international orange' },
  blue:    { hex: '#2563eb', ink: '#ffffff', label: 'workhorse blue' },
  cobalt:  { hex: '#0037ff', ink: '#ffffff', label: 'electric cobalt' },
  acid:    { hex: '#b8e600', ink: '#111111', label: 'acid green' },
  emerald: { hex: '#059669', ink: '#ffffff', label: 'emerald' },
  crimson: { hex: '#d92626', ink: '#ffffff', label: 'crimson' },
  pink:    { hex: '#ec4899', ink: '#ffffff', label: 'hot pink' },
  violet:  { hex: '#7c3aed', ink: '#ffffff', label: 'violet' },
  amber:   { hex: '#f5a623', ink: '#111111', label: 'amber' },
  teal:    { hex: '#0891b2', ink: '#ffffff', label: 'deep teal' },
  mono:    { hex: null,      ink: null,      label: 'no accent (mono)' },
};

/* base neutral ramps: [bg, surface, surface2, text, text2, muted, line] */
const NEUTRALS = {
  pure: {
    label: 'pure', note: 'plain black, white, gray',
    light: ['#fafafa', '#ffffff', '#f1f1f3', '#141417', '#54545e', '#9d9da8', '#e5e5ea'],
    dark:  ['#0a0a0b', '#141417', '#1d1d22', '#f2f2f4', '#a2a2ad', '#5b5b66', '#28282e'],
  },
  warm: {
    label: 'warm', note: 'paper, cream & ink',
    light: ['#faf6ee', '#fffdf7', '#f2ebdd', '#211a13', '#6d6052', '#a39683', '#e6dcc9'],
    dark:  ['#15110c', '#1e1912', '#2a2217', '#f2eadb', '#a89b88', '#6d6052', '#332a1d'],
  },
  cool: {
    label: 'cool', note: 'slate & steel blue-grays',
    light: ['#f4f6fa', '#ffffff', '#e9edf4', '#101828', '#475467', '#98a2b3', '#dce1eb'],
    dark:  ['#0a0f16', '#111a26', '#1a2536', '#e7edf5', '#94a3b8', '#4d5b70', '#233247'],
  },
  tinted: {
    label: 'accent-tinted', note: 'neutrals carry a whisper of the accent',
    light: [
      'color-mix(in oklab, var(--accent) 5%, #fafafa)',
      'color-mix(in oklab, var(--accent) 2%, #ffffff)',
      'color-mix(in oklab, var(--accent) 8%, #f1f1f3)',
      'color-mix(in oklab, var(--accent) 14%, #141417)',
      'color-mix(in oklab, var(--accent) 12%, #54545e)',
      'color-mix(in oklab, var(--accent) 10%, #9d9da8)',
      'color-mix(in oklab, var(--accent) 9%, #e3e3e8)',
    ],
    dark: [
      'color-mix(in oklab, var(--accent) 7%, #0a0a0b)',
      'color-mix(in oklab, var(--accent) 9%, #141417)',
      'color-mix(in oklab, var(--accent) 11%, #1d1d22)',
      'color-mix(in oklab, var(--accent) 10%, #f2f2f4)',
      'color-mix(in oklab, var(--accent) 10%, #a2a2ad)',
      'color-mix(in oklab, var(--accent) 9%, #5b5b66)',
      'color-mix(in oklab, var(--accent) 12%, #28282e)',
    ],
  },
};

const PALETTE_KEYS = ['--bg', '--surface', '--surface2', '--text', '--text2', '--muted', '--line'];

function paletteVars(neutrals, mode) {
  const ramp = NEUTRALS[neutrals][mode];
  return Object.fromEntries(PALETTE_KEYS.map((k, i) => [k, ramp[i]]));
}

/* ---------- fonts ---------- */

const DISPLAY_FONTS = {
  'schibsted':  { css: `'Schibsted Grotesk', sans-serif`, label: 'Schibsted Grotesk', note: 'editorial grotesque' },
  'space-grotesk': { css: `'Space Grotesk', sans-serif`, label: 'Space Grotesk', note: 'techy grotesque' },
  'archivo':    { css: `'Archivo', sans-serif`, label: 'Archivo', note: 'swiss workhorse' },
  'bricolage':  { css: `'Bricolage Grotesque', sans-serif`, label: 'Bricolage Grotesque', note: 'characterful, a bit odd' },
  'sora':       { css: `'Sora', sans-serif`, label: 'Sora', note: 'geometric, future-leaning' },
  'gabarito':   { css: `'Gabarito', sans-serif`, label: 'Gabarito', note: 'friendly geometric' },
  'fraunces':   { css: `'Fraunces', serif`, label: 'Fraunces', note: 'wonky display serif' },
  'instrument-serif': { css: `'Instrument Serif', serif`, label: 'Instrument Serif', note: 'sharp display serif', vars: { '--h-weight': '400' } },
  'newsreader': { css: `'Newsreader', serif`, label: 'Newsreader', note: 'newsprint serif' },
  'jetbrains':  { css: `'JetBrains Mono', monospace`, label: 'JetBrains Mono', note: 'terminal headings' },
  'space-mono': { css: `'Space Mono', monospace`, label: 'Space Mono', note: 'retro terminal' },
};

const BODY_FONTS = {
  'inter':      { css: `'Inter', sans-serif`, label: 'Inter', note: 'the invisible default' },
  'instrument': { css: `'Instrument Sans', sans-serif`, label: 'Instrument Sans', note: 'humanist, warmer than inter' },
  'manrope':    { css: `'Manrope', sans-serif`, label: 'Manrope', note: 'rounded, modern' },
  'dm-sans':    { css: `'DM Sans', sans-serif`, label: 'DM Sans', note: 'geometric, low-contrast' },
  'plex-sans':  { css: `'IBM Plex Sans', sans-serif`, label: 'IBM Plex Sans', note: 'engineered, slightly square' },
  'newsreader': { css: `'Newsreader', serif`, label: 'Newsreader', note: 'serif body — bookish' },
  'jetbrains':  { css: `'JetBrains Mono', monospace`, label: 'JetBrains Mono', note: 'mono body — full terminal' },
};

const MONO_FONTS = {
  'jetbrains': { css: `'JetBrains Mono', monospace`, label: 'JetBrains Mono', note: 'crisp, tall' },
  'plex-mono': { css: `'IBM Plex Mono', monospace`, label: 'IBM Plex Mono', note: 'typewriter-ish' },
  'space-mono':{ css: `'Space Mono', monospace`, label: 'Space Mono', note: 'quirky, retro' },
  'fira-code': { css: `'Fira Code', monospace`, label: 'Fira Code', note: 'the coder classic' },
};

/* ---------- dimension config ---------- */

const DIMENSIONS = [
  {
    id: 'display', title: 'display type', blurb: 'the font for headings, wordmarks, big numbers. the single loudest identity signal.',
    options: Object.entries(DISPLAY_FONTS).map(([key, f]) => ({
      key, label: f.label, note: f.note,
      vars: { '--font-display': f.css, ...(f.vars || {}) },
    })),
    specimen: () => `
      <div class="spec-type">
        <div class="spec-aa">Aa</div>
        <div class="spec-h">Office days, tracked.</div>
      </div>`,
  },
  {
    id: 'body', title: 'body type', blurb: 'paragraphs, labels, table cells. should disappear — or quietly disagree.',
    options: Object.entries(BODY_FONTS).map(([key, f]) => ({
      key, label: f.label, note: f.note,
      vars: { '--font-body': f.css },
    })),
    specimen: () => `
      <div class="spec-body">
        <p>Thursday was your busiest office day this month. Three trips logged, two rolls developed, one long flight home.</p>
        <span class="spec-small">last synced 4 minutes ago</span>
      </div>`,
  },
  {
    id: 'mono', title: 'mono type', blurb: 'stats, timestamps, ids, code. the data voice of the apps.',
    options: Object.entries(MONO_FONTS).map(([key, f]) => ({
      key, label: f.label, note: f.note,
      vars: { '--font-mono': f.css },
    })),
    specimen: () => `
      <div class="spec-mono">
        <span>GET /api/days → 200 · 34ms</span>
        <span class="spec-digits">0123456789 &nbsp; 14:32:07</span>
      </div>`,
  },
  {
    id: 'headings', title: 'heading treatment', blurb: 'how loud a page title is allowed to be.',
    options: [
      { key: 'massive', label: 'massive & tight', note: 'huge, heavy, negative tracking',
        vars: { '--h-weight': '800', '--h-tracking': '-0.03em', '--h-scale': '1.4', '--h-transform': 'none' } },
      { key: 'quiet', label: 'quiet & small', note: 'restrained, lets content lead',
        vars: { '--h-weight': '600', '--h-tracking': '-0.01em', '--h-scale': '0.85', '--h-transform': 'none' } },
      { key: 'editorial', label: 'editorial light', note: 'large but featherweight',
        vars: { '--h-weight': '400', '--h-tracking': '-0.015em', '--h-scale': '1.3', '--h-transform': 'none' } },
      { key: 'caps', label: 'caps label', note: 'small, uppercase, tracked out',
        vars: { '--h-weight': '700', '--h-tracking': '0.09em', '--h-scale': '0.72', '--h-transform': 'uppercase' } },
    ],
    specimen: () => `
      <div class="spec-headings">
        <div class="spec-h2">Six apps, one look</div>
        <p>A shared language across every slop app.</p>
      </div>`,
  },
  {
    id: 'casing', title: 'ui casing', blurb: 'buttons, nav, labels. lowercase-everything is a whole personality.',
    options: [
      { key: 'sentence', label: 'Sentence case', note: 'normal, unremarkable', vars: { '--ui-transform': 'none', '--ui-tracking': '0' } },
      { key: 'title', label: 'Title Case', note: 'a touch formal', vars: { '--ui-transform': 'capitalize', '--ui-tracking': '0' } },
      { key: 'caps', label: 'ALL CAPS', note: 'labels shout, tracked wide', vars: { '--ui-transform': 'uppercase', '--ui-tracking': '0.07em' } },
      { key: 'lower', label: 'all lowercase', note: 'casual, very identifiable', vars: { '--ui-transform': 'lowercase', '--ui-tracking': '0' } },
    ],
    specimen: () => `
      <div class="spec-casing">
        <button class="btn primary spec-noop">Save changes</button>
        <button class="btn spec-noop">Cancel</button>
        <span class="ui-label">Settings</span>
      </div>`,
  },
  {
    id: 'mode', title: 'color mode', blurb: 'the house policy. (flip the ◐ toggle up top any time to preview both.)',
    options: [
      { key: 'dark', label: 'always dark', note: 'every app ships dark', modeCard: 'dark' },
      { key: 'light', label: 'always light', note: 'every app ships light', modeCard: 'light' },
      { key: 'both', label: 'per app / system', note: 'apps pick what suits them, or follow the OS', modeCard: 'split' },
    ],
    specimen: () => `
      <div class="spec-mode">
        <div class="spec-mode-pane">
          <span class="spec-h3">Voyage</span>
          <p>12 flights this year</p>
          <button class="btn primary spec-noop">Log trip</button>
        </div>
      </div>`,
  },
  {
    id: 'neutrals', title: 'neutrals', blurb: 'the grays underneath everything. warm reads analog, cool reads technical.',
    options: Object.entries(NEUTRALS).map(([key, n]) => ({
      key, label: n.label, note: n.note, neutralsCard: key,
    })),
    specimen: () => `
      <div class="spec-neutrals">
        <div class="spec-surface">
          <span class="spec-h3">Surface</span>
          <p>Secondary text on a card.</p>
          <span class="spec-small">muted caption</span>
        </div>
      </div>`,
  },
  {
    id: 'accent', title: 'accent', blurb: 'the one color people should associate with a bailey app.', accentGrid: true,
    options: Object.entries(ACCENTS).map(([key, a]) => ({
      key, label: a.label, note: a.hex || 'text color does the work', swatch: a.hex,
    })),
  },
  {
    id: 'accentUse', title: 'accent intensity', blurb: 'how much of the accent an app is allowed to wear.',
    options: [
      { key: 'sparse', label: 'sparse', note: 'links & focus only; buttons stay ink',
        vars: { '--btn-bg': 'var(--text)', '--btn-ink': 'var(--bg)', '--chip-bg': 'var(--surface2)', '--chip-ink': 'var(--text2)', '--band': 'transparent' } },
      { key: 'moderate', label: 'moderate', note: 'primary buttons & highlights',
        vars: { '--btn-bg': 'var(--accent)', '--btn-ink': 'var(--accent-ink)', '--chip-bg': 'var(--accent-soft)', '--chip-ink': 'var(--accent-strong)', '--band': 'transparent' } },
      { key: 'loud', label: 'loud', note: 'colored bands, filled sections',
        vars: { '--btn-bg': 'var(--accent)', '--btn-ink': 'var(--accent-ink)', '--chip-bg': 'var(--accent-soft)', '--chip-ink': 'var(--accent-strong)', '--band': 'var(--accent-soft)' } },
    ],
    specimen: () => `
      <div class="spec-accentuse">
        <div class="spec-band">
          <span class="spec-h3">Darkroom</span>
          <span class="chip">12 rolls</span>
        </div>
        <p>Push processing <a class="spec-link">looks right</a> at +2.</p>
        <button class="btn primary spec-noop">Develop</button>
      </div>`,
  },
  {
    id: 'radius', title: 'corner radius', blurb: 'sharp reads serious; pill reads friendly. this one decision does a lot.',
    options: [
      { key: 'r0',   label: '0 — sharp',    note: 'engineering drawing', vars: { '--r-ctl': '0px', '--r-card': '0px' } },
      { key: 'r3',   label: '3px — barely', note: 'takes the edge off',  vars: { '--r-ctl': '3px', '--r-card': '4px' } },
      { key: 'r8',   label: '8px — soft',   note: 'the modern default',  vars: { '--r-ctl': '8px', '--r-card': '10px' } },
      { key: 'r14',  label: '14px — round', note: 'friendly, app-like',  vars: { '--r-ctl': '14px', '--r-card': '18px' } },
      { key: 'pill', label: 'pill',         note: 'controls go full pill', vars: { '--r-ctl': '999px', '--r-card': '24px' } },
    ],
    specimen: () => `
      <div class="spec-radius">
        <div class="spec-surface">
          <input class="input spec-noop" value="MEL → HND" readonly>
          <button class="btn primary spec-noop">Search</button>
        </div>
      </div>`,
  },
  {
    id: 'borders', title: 'borders', blurb: 'how surfaces declare their edges.',
    options: [
      { key: 'hairline', label: 'hairline', note: '1px quiet lines on everything',
        vars: { '--bw': '1px', '--line-c': 'var(--line)', '--card-bg': 'var(--surface)' } },
      { key: 'bold', label: 'bold outline', note: '2px ink — comic / brutalist',
        vars: { '--bw': '2px', '--line-c': 'var(--text)', '--card-bg': 'var(--surface)', '--btn-edge': 'var(--text)' } },
      { key: 'elevation', label: 'none — shadow', note: 'edges come from elevation',
        vars: { '--bw': '0px', '--line-c': 'transparent', '--card-bg': 'var(--surface)' } },
      { key: 'flat', label: 'none — flat', note: 'background shifts do the separating',
        vars: { '--bw': '0px', '--line-c': 'transparent', '--card-bg': 'var(--surface2)' } },
    ],
    specimen: () => `
      <div class="spec-borders">
        <div class="spec-surface">
          <span class="spec-h3">Roll 14</span>
          <p>Portra 400 · 36 exp</p>
        </div>
      </div>`,
  },
  {
    id: 'shadow', title: 'shadows', blurb: 'flat, floaty, or punched-out.',
    options: [
      { key: 'none', label: 'none', note: 'flat, honest', vars: { '--shadow': 'none' } },
      { key: 'soft', label: 'soft ambient', note: 'gentle float', vars: { '--shadow': '0 1px 2px rgb(0 0 0 / .06), 0 10px 28px rgb(0 0 0 / .10)' } },
      { key: 'hard', label: 'hard offset', note: 'solid drop — pairs with bold borders', vars: { '--shadow': '4px 4px 0 var(--text)', '--btn-shadow': '3px 3px 0 var(--text)' } },
      { key: 'glow', label: 'accent glow', note: 'neon halo, loves dark mode', vars: { '--shadow': '0 0 0 1px color-mix(in oklab, var(--accent) 30%, transparent), 0 0 28px color-mix(in oklab, var(--accent) 22%, transparent)' } },
    ],
    specimen: () => `
      <div class="spec-shadow">
        <div class="spec-surface">
          <span class="spec-h3">Covers</span>
          <p>album wall, 9×9</p>
        </div>
      </div>`,
  },
  {
    id: 'density', title: 'density', blurb: 'how much air the ui breathes.',
    options: [
      { key: 'compact',     label: 'compact',     note: 'data-dense, terminal energy', vars: { '--sp': '0.72', '--fs': '13.5px' } },
      { key: 'comfortable', label: 'comfortable', note: 'the sensible middle',         vars: { '--sp': '1',    '--fs': '14.5px' } },
      { key: 'airy',        label: 'airy',        note: 'gallery spacing, generous',   vars: { '--sp': '1.45', '--fs': '15.5px' } },
    ],
    specimen: () => `
      <div class="spec-density">
        <div class="spec-surface">
          <div class="spec-row"><span>MEL → SYD</span><span class="spec-small">1h 25m</span></div>
          <div class="spec-row"><span>SYD → HND</span><span class="spec-small">9h 40m</span></div>
          <div class="spec-row"><span>HND → MEL</span><span class="spec-small">10h 05m</span></div>
        </div>
      </div>`,
  },
  {
    id: 'texture', title: 'background texture', blurb: 'what lives behind everything. subtle is fine, but subtle everywhere is a signature.',
    options: [
      { key: 'solid', label: 'solid', note: 'nothing — just the neutral', vars: { '--page-texture': 'none' } },
      { key: 'grain', label: 'film grain', note: 'analog noise, barely there',
        vars: { '--page-texture': `url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='160' height='160'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.9' numOctaves='2'/%3E%3C/filter%3E%3Crect width='160' height='160' filter='url(%23n)' opacity='0.05'/%3E%3C/svg%3E")` } },
      { key: 'dots', label: 'dot grid', note: 'graph-paper for apps',
        vars: { '--page-texture': 'radial-gradient(color-mix(in oklab, var(--text) 14%, transparent) 1px, transparent 1px)', '--texture-size': '22px 22px' } },
      { key: 'grid', label: 'ruled grid', note: 'blueprint lines',
        vars: { '--page-texture': 'linear-gradient(color-mix(in oklab, var(--text) 6%, transparent) 1px, transparent 1px), linear-gradient(90deg, color-mix(in oklab, var(--text) 6%, transparent) 1px, transparent 1px)', '--texture-size': '44px 44px' } },
      { key: 'wash', label: 'accent wash', note: 'a soft pool of the accent',
        vars: { '--page-texture': 'radial-gradient(90rem 60rem at 85% -20%, color-mix(in oklab, var(--accent) 16%, transparent), transparent 60%)', '--texture-size': '100% 100%', '--texture-repeat': 'no-repeat' } },
    ],
    specimen: (opt) => `
      <div class="spec-texture" style="background-color: var(--bg); background-image: var(--page-texture); background-size: var(--texture-size, auto); background-repeat: var(--texture-repeat, repeat);">
        <span class="spec-small">background</span>
      </div>`,
  },
  {
    id: 'fill', title: 'surface fill', blurb: 'what buttons and cards are made of.',
    options: [
      { key: 'flat', label: 'flat', note: 'solid color, no tricks', vars: { '--btn-layer': 'none', '--glass-bg': 'var(--card-bg)', '--glass-blur': 'none' } },
      { key: 'gradient', label: 'subtle gradient', note: 'a quiet sheen on solids',
        vars: { '--btn-layer': 'linear-gradient(180deg, rgb(255 255 255 / .14), rgb(0 0 0 / .10))', '--glass-bg': 'var(--card-bg)', '--glass-blur': 'none' } },
      { key: 'glass', label: 'glass', note: 'translucent, blurred — needs texture behind it',
        vars: { '--btn-layer': 'none', '--glass-bg': 'color-mix(in srgb, var(--surface) 66%, transparent)', '--glass-blur': 'blur(14px)' } },
    ],
    specimen: () => `
      <div class="spec-fill">
        <div class="spec-surface glassy">
          <span class="spec-h3">Now playing</span>
          <button class="btn primary spec-noop">Play</button>
        </div>
      </div>`,
  },
  {
    id: 'motion', title: 'motion', blurb: 'hover the samples. how alive should things feel?',
    options: [
      { key: 'none', label: 'none', note: 'instant, zero animation', vars: { '--dur': '0s', '--ease': 'linear', '--hover-t': 'none' } },
      { key: 'subtle', label: 'subtle', note: '150ms fades, nothing moves', vars: { '--dur': '.15s', '--ease': 'ease-out', '--hover-t': 'none' } },
      { key: 'springy', label: 'springy', note: 'things lift and settle', vars: { '--dur': '.3s', '--ease': 'cubic-bezier(.34,1.56,.64,1)', '--hover-t': 'translateY(-2px)' } },
      { key: 'playful', label: 'playful', note: 'lift + tilt, staggered entrances', vars: { '--dur': '.35s', '--ease': 'cubic-bezier(.34,1.56,.64,1)', '--hover-t': 'translateY(-3px) rotate(-.6deg) scale(1.02)' } },
    ],
    specimen: () => `
      <div class="spec-motion">
        <button class="btn primary hoverable spec-noop">hover me</button>
        <button class="btn hoverable spec-noop">me too</button>
      </div>`,
  },
  {
    id: 'signature', title: 'signature', blurb: 'a tiny shared mark on every app, so people know whose slop it is.',
    options: [
      { key: 'none', label: 'none', note: 'the style is the signature' },
      { key: 'footer', label: 'footer wordmark', note: 'a quiet "bailey" at the very bottom' },
      { key: 'glyph', label: 'corner glyph', note: 'a small "b." pinned in a corner' },
    ],
    specimen: (opt) => ({
      none: `<div class="spec-sig"><span class="spec-small">— nothing. clean exit.</span></div>`,
      footer: `<div class="spec-sig"><div class="sig-footer">bailey · 2026</div></div>`,
      glyph: `<div class="spec-sig"><div class="sig-glyph">b.</div></div>`,
    })[opt.key],
  },
];

/* ---------- presets ---------- */

const DEFAULTS = {
  display: 'schibsted', body: 'instrument', mono: 'jetbrains', headings: 'massive',
  casing: 'sentence', mode: 'dark', neutrals: 'pure', accent: 'orange', accentUse: 'moderate',
  radius: 'r8', borders: 'hairline', shadow: 'none', density: 'comfortable',
  texture: 'solid', fill: 'flat', motion: 'subtle', signature: 'none',
};

const PRESETS = {
  /* the first two approximate the two design families the existing apps
     already cluster into — useful as a "current state" baseline */
  swiss: { label: 'swiss · now', picks: { display: 'schibsted', body: 'inter', mono: 'space-mono', headings: 'quiet', casing: 'caps', mode: 'light', neutrals: 'pure', accent: 'mono', accentUse: 'sparse', radius: 'r0', borders: 'hairline', shadow: 'none', density: 'compact', texture: 'solid', fill: 'flat', motion: 'none', signature: 'none' } },
  instrument: { label: 'instrument · now', picks: { display: 'jetbrains', body: 'plex-sans', mono: 'plex-mono', headings: 'caps', casing: 'caps', mode: 'dark', neutrals: 'warm', accent: 'amber', accentUse: 'moderate', radius: 'r8', borders: 'hairline', shadow: 'none', density: 'compact', texture: 'solid', fill: 'flat', motion: 'subtle', signature: 'none' } },
  terminal: { label: 'terminal', picks: { display: 'jetbrains', body: 'jetbrains', mono: 'jetbrains', headings: 'caps', casing: 'lower', mode: 'dark', neutrals: 'pure', accent: 'acid', accentUse: 'sparse', radius: 'r0', borders: 'hairline', shadow: 'none', density: 'compact', texture: 'grid', fill: 'flat', motion: 'subtle', signature: 'glyph' } },
  paper: { label: 'paper', picks: { display: 'newsreader', body: 'instrument', mono: 'plex-mono', headings: 'editorial', casing: 'sentence', mode: 'light', neutrals: 'warm', accent: 'crimson', accentUse: 'sparse', radius: 'r3', borders: 'hairline', shadow: 'none', density: 'airy', texture: 'grain', fill: 'flat', motion: 'subtle', signature: 'footer' } },
  brut: { label: 'brut', picks: { display: 'archivo', body: 'inter', mono: 'space-mono', headings: 'massive', casing: 'caps', mode: 'light', neutrals: 'pure', accent: 'orange', accentUse: 'loud', radius: 'r0', borders: 'bold', shadow: 'hard', density: 'comfortable', texture: 'solid', fill: 'flat', motion: 'springy', signature: 'glyph' } },
  club: { label: 'soft club', picks: { display: 'sora', body: 'manrope', mono: 'jetbrains', headings: 'massive', casing: 'lower', mode: 'dark', neutrals: 'tinted', accent: 'violet', accentUse: 'moderate', radius: 'r14', borders: 'elevation', shadow: 'glow', density: 'comfortable', texture: 'wash', fill: 'glass', motion: 'springy', signature: 'none' } },
  lab: { label: 'lab', picks: { display: 'space-grotesk', body: 'plex-sans', mono: 'plex-mono', headings: 'quiet', casing: 'sentence', mode: 'light', neutrals: 'cool', accent: 'blue', accentUse: 'moderate', radius: 'r8', borders: 'hairline', shadow: 'soft', density: 'comfortable', texture: 'dots', fill: 'flat', motion: 'subtle', signature: 'footer' } },
  editorial: { label: 'editorial', picks: { display: 'fraunces', body: 'inter', mono: 'jetbrains', headings: 'editorial', casing: 'sentence', mode: 'light', neutrals: 'pure', accent: 'mono', accentUse: 'sparse', radius: 'r0', borders: 'flat', shadow: 'none', density: 'airy', texture: 'solid', fill: 'flat', motion: 'subtle', signature: 'footer' } },
};

/* ---------- state ---------- */

const state = {
  picks: { ...DEFAULTS },
  touched: new Set(),
  preset: null,
  customAccent: null, // {hex, ink}
  previewMode: null,  // overrides mode for viewing only
  notes: '',
};

function currentMode() {
  if (state.previewMode) return state.previewMode;
  return state.picks.mode === 'light' ? 'light' : 'dark';
}

function accentInfo() {
  if (state.picks.accent === 'custom' && state.customAccent) return state.customAccent;
  const a = ACCENTS[state.picks.accent] || ACCENTS.orange;
  if (a.hex === null) {
    // mono: accent = ink
    const mode = currentMode();
    return { hex: mode === 'dark' ? '#f2f2f4' : '#141417', ink: mode === 'dark' ? '#0a0a0b' : '#ffffff', label: a.label };
  }
  return a;
}

/* ---------- apply state to :root ---------- */

function optionByKey(dimId, key) {
  const dim = DIMENSIONS.find(d => d.id === dimId);
  return dim.options.find(o => o.key === key) || null;
}

function applyState() {
  const root = document.documentElement;
  const mode = currentMode();
  root.dataset.mode = mode;

  const vars = {};
  Object.assign(vars, paletteVars(state.picks.neutrals, mode));

  const acc = accentInfo();
  vars['--accent'] = acc.hex;
  vars['--accent-ink'] = acc.ink;
  vars['--accent-soft'] = 'color-mix(in oklab, var(--accent) 14%, var(--bg))';
  vars['--accent-strong'] = 'color-mix(in oklab, var(--accent) 78%, var(--text))';

  // display font last: a 400-only display face (instrument serif) must be able
  // to override the heading treatment's weight
  for (const dim of DIMENSIONS) {
    if (dim.id === 'display') continue;
    const opt = optionByKey(dim.id, state.picks[dim.id]);
    if (opt && opt.vars) Object.assign(vars, opt.vars);
  }
  const displayOpt = optionByKey('display', state.picks.display);
  if (displayOpt && displayOpt.vars) Object.assign(vars, displayOpt.vars);

  root.removeAttribute('style');
  for (const [k, v] of Object.entries(vars)) root.style.setProperty(k, v);

  root.dataset.motion = state.picks.motion;
  root.dataset.accentuse = state.picks.accentUse;

  renderRail();
  renderPreview();
  markSelections();
  syncHash();
  persist();
}

/* ---------- render: presets ---------- */

function renderPresets() {
  const el = document.getElementById('presets');
  el.innerHTML = '<span class="presets-label">start from</span>' + Object.entries(PRESETS)
    .map(([k, p]) => `<button class="preset-chip${state.preset === k ? ' on' : ''}" data-preset="${k}">${p.label}</button>`).join('');
  el.querySelectorAll('[data-preset]').forEach(b => b.addEventListener('click', () => {
    const k = b.dataset.preset;
    state.picks = { ...DEFAULTS, ...PRESETS[k].picks };
    state.preset = k;
    state.touched = new Set(Object.keys(state.picks));
    renderPresets();
    applyState();
  }));
}

/* ---------- render: rail ---------- */

function renderRail() {
  const el = document.getElementById('rail');
  const chosen = state.touched.size;
  el.innerHTML = `<div class="rail-count">${chosen}/${DIMENSIONS.length} chosen</div>` + DIMENSIONS.map(d => {
    const opt = optionByKey(d.id, state.picks[d.id]);
    const label = state.picks[d.id] === 'custom' ? (state.customAccent?.hex || 'custom') : (opt ? opt.label : '—');
    return `<a class="rail-item" href="#dim-${d.id}" data-dim="${d.id}">
      <span class="rail-name">${d.title}</span>
      <span class="rail-pick${state.touched.has(d.id) ? ' picked' : ''}">${label}</span>
    </a>`;
  }).join('');
}

/* ---------- render: sections ---------- */

function renderSections() {
  const el = document.getElementById('sections');
  el.innerHTML = DIMENSIONS.map(dim => `
    <section class="dim" id="dim-${dim.id}" data-dim="${dim.id}">
      <header class="dim-head">
        <h2>${dim.title}</h2>
        <p>${dim.blurb}</p>
      </header>
      ${dim.accentGrid ? renderAccentGrid(dim) : `
      <div class="variants ${dim.options.length > 6 ? 'dense' : ''}">
        ${dim.options.map(opt => renderVariantCard(dim, opt)).join('')}
      </div>`}
    </section>`).join('');

  el.querySelectorAll('.variant').forEach(card => {
    card.addEventListener('click', () => {
      pick(card.closest('.dim').dataset.dim, card.dataset.key);
    });
  });

  wireAccentGrid(el);
}

function localVarStyle(opt, dim) {
  let vars = { ...(opt.vars || {}) };
  if (opt.modeCard) {
    const m = opt.modeCard === 'split' ? currentMode() : opt.modeCard;
    Object.assign(vars, paletteVars(state.picks.neutrals, m));
  }
  if (opt.neutralsCard) {
    Object.assign(vars, paletteVars(opt.neutralsCard, currentMode()));
  }
  return Object.entries(vars).map(([k, v]) => `${k}:${v}`).join(';').replaceAll('"', '&quot;');
}

function renderVariantCard(dim, opt) {
  const spec = typeof dim.specimen === 'function' ? dim.specimen(opt) : '';
  return `
    <button type="button" class="variant" data-key="${opt.key}" style="${localVarStyle(opt, dim)}">
      <span class="variant-spec">${spec}</span>
      <span class="variant-meta">
        <span class="variant-label">${opt.label}</span>
        <span class="variant-note">${opt.note || ''}</span>
      </span>
      <span class="variant-check" aria-hidden="true">✓</span>
    </button>`;
}

function renderAccentGrid(dim) {
  return `
    <div class="accent-grid">
      ${dim.options.map(o => `
        <button type="button" class="accent-swatch" data-accent="${o.key}" title="${o.label}">
          <span class="accent-dot" style="${o.swatch ? `background:${o.swatch}` : 'background:var(--text)'}"></span>
          <span class="accent-name">${o.label}</span>
        </button>`).join('')}
      <label class="accent-swatch accent-custom">
        <input type="color" id="customAccent" value="#ff4d00">
        <span class="accent-name">custom…</span>
      </label>
    </div>
    <div class="accent-demo">
      <a class="spec-link">a text link</a>
      <button class="btn primary spec-noop">Primary</button>
      <span class="chip">chip</span>
      <span class="accent-hex" id="accentHex"></span>
    </div>`;
}

function wireAccentGrid(el) {
  el.querySelectorAll('[data-accent]').forEach(b => b.addEventListener('click', () => {
    pick('accent', b.dataset.accent);
  }));
  const input = el.querySelector('#customAccent');
  if (input) input.addEventListener('input', () => {
    const hex = input.value;
    state.customAccent = { hex, ink: bestInk(hex), label: `custom ${hex}` };
    pick('accent', 'custom');
  });
}

function bestInk(hex) {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255, g = (n >> 8) & 255, b = n & 255;
  const lum = 0.2126 * r + 0.7152 * g + 0.0722 * b;
  return lum > 150 ? '#111111' : '#ffffff';
}

/* ---------- selection ---------- */

function pick(dimId, key) {
  state.picks[dimId] = key;
  state.touched.add(dimId);
  applyState();
}

function markSelections() {
  document.querySelectorAll('.dim').forEach(sec => {
    const dimId = sec.dataset.dim;
    sec.querySelectorAll('.variant').forEach(c =>
      c.classList.toggle('selected', c.dataset.key === state.picks[dimId]));
    sec.querySelectorAll('[data-accent]').forEach(c =>
      c.classList.toggle('selected', c.dataset.accent === state.picks[dimId]));
  });
  const hexEl = document.getElementById('accentHex');
  if (hexEl) hexEl.textContent = accentInfo().hex;
  // mode/neutrals variant cards depend on current global state — refresh their inline vars
  document.querySelectorAll('.dim').forEach(sec => {
    const dim = DIMENSIONS.find(d => d.id === sec.dataset.dim);
    if (!dim || !dim.options.some(o => o.modeCard || o.neutralsCard)) return;
    sec.querySelectorAll('.variant').forEach(card => {
      const opt = dim.options.find(o => o.key === card.dataset.key);
      if (opt) card.setAttribute('style', localVarStyle(opt, dim).replaceAll('&quot;', '"'));
    });
  });
}

/* ---------- preview (the fake app) ---------- */

function renderPreview() {
  const days = [3, 5, 2, 6, 4, 7, 5];
  const max = Math.max(...days);
  const sig = state.picks.signature;
  document.getElementById('appMock').innerHTML = `
    <div class="mock-window">
      <div class="mock-nav">
        <span class="mock-brand">officetracker</span>
        <span class="mock-tabs"><span class="on">Week</span><span>Month</span><span>Year</span></span>
      </div>
      <div class="mock-body">
        <div class="mock-h">Where do you work?</div>
        <p class="mock-sub">Office attendance, tracked automatically. <a class="spec-link">How it works</a></p>
        <div class="mock-stats">
          <div class="stat-tile">
            <span class="stat-label">Office days</span>
            <span class="stat-value">14</span>
            <span class="stat-delta">▲ 3 vs last month</span>
          </div>
          <div class="stat-tile">
            <span class="stat-label">Longest streak</span>
            <span class="stat-value">6</span>
            <span class="stat-delta">▲ personal best</span>
          </div>
        </div>
        <div class="mock-chart" role="img" aria-label="Office days per week, last 7 weeks">
          ${days.map(v => `<span class="bar" style="--v:${(v / max * 100).toFixed(0)}%" title="${v} days"></span>`).join('')}
        </div>
        <div class="mock-card">
          <span class="mock-card-title">Monthly recap</span>
          <p class="mock-prose">You made it in fourteen times in June — your best month since March. Tuesdays are still the work-from-home anchor, and the Friday coffee-run streak is now six weeks old. The long Sydney trip barely dented the average.</p>
          <p class="mock-prose muted">If the pattern holds, July lands around sixteen office days. <a class="spec-link">See the full breakdown</a>, or lower the target and take the win.</p>
        </div>
        <div class="mock-card">
          <div class="mock-card-head">
            <span class="mock-card-title">This week</span>
            <span class="chip">on track</span>
          </div>
          <table class="mock-table">
            <tr><td>Mon</td><td>Office</td><td class="num">9:12</td></tr>
            <tr><td>Tue</td><td>Home</td><td class="num">—</td></tr>
            <tr><td>Wed</td><td>Office</td><td class="num">8:47</td></tr>
          </table>
          <div class="mock-form">
            <input class="input" value="Add a note…" readonly>
            <button class="btn primary spec-noop">Save</button>
            <button class="btn spec-noop">Skip</button>
          </div>
        </div>
        ${sig === 'footer' ? `<div class="sig-footer">bailey · 2026</div>` : ''}
      </div>
      ${sig === 'glyph' ? `<div class="sig-glyph">b.</div>` : ''}
    </div>`;
}

/* ---------- export ---------- */

function resolvedTokens() {
  const acc = accentInfo();
  const out = {};
  for (const dim of DIMENSIONS) {
    const key = state.picks[dim.id];
    const opt = optionByKey(dim.id, key);
    out[dim.id] = {
      pick: key,
      label: key === 'custom' ? acc.label : (opt ? opt.label : key),
      touched: state.touched.has(dim.id),
      ...(opt && opt.vars ? { vars: opt.vars } : {}),
    };
  }
  out.accent.hex = acc.hex;
  out.accent.ink = acc.ink;
  return out;
}

function buildProse() {
  const t = resolvedTokens();
  const line = (name, dimId, extra = '') =>
    `${name}: ${t[dimId].label}${extra}${t[dimId].touched ? '' : '  (untouched default)'}`;
  const untouched = DIMENSIONS.filter(d => !state.touched.has(d.id)).map(d => d.title);
  return [
    `BAILEY'S HOUSE STYLE — taste export, ${new Date().toISOString().slice(0, 10)}`,
    state.preset ? `started from preset: ${PRESETS[state.preset].label}` : `started from scratch`,
    ``,
    line('display type', 'display'),
    line('body type', 'body'),
    line('mono type', 'mono'),
    line('headings', 'headings'),
    line('ui casing', 'casing'),
    line('color mode', 'mode'),
    line('neutrals', 'neutrals'),
    line('accent', 'accent', ` — ${t.accent.hex}`),
    line('accent intensity', 'accentUse'),
    line('corner radius', 'radius'),
    line('borders', 'borders'),
    line('shadows', 'shadow'),
    line('density', 'density'),
    line('texture', 'texture'),
    line('surface fill', 'fill'),
    line('motion', 'motion'),
    line('signature', 'signature'),
    ``,
    untouched.length ? `no strong preference (left at default): ${untouched.join(', ')}` : `every dimension was an explicit choice.`,
    state.notes.trim() ? `\nnotes: ${state.notes.trim()}` : ``,
    ``,
    `state url: ${location.href}`,
  ].filter(l => l !== null).join('\n');
}

function buildJson() {
  return JSON.stringify({
    version: 1,
    exported: new Date().toISOString(),
    preset: state.preset,
    notes: state.notes.trim() || undefined,
    tokens: resolvedTokens(),
  }, null, 2);
}

function openExport() {
  const dlg = document.getElementById('exportDialog');
  const refresh = () => {
    document.getElementById('exportProse').textContent = buildProse();
    document.getElementById('exportJson').textContent = buildJson();
  };
  const notes = document.getElementById('exportNotes');
  notes.value = state.notes;
  notes.oninput = () => { state.notes = notes.value; persist(); refresh(); };
  refresh();
  dlg.showModal();
}

/* ---------- persistence: hash + localStorage ---------- */

function syncHash() {
  // only explicit choices go in the url — untouched defaults stay out so a
  // shared/reloaded link doesn't claim dimensions the user never picked
  const parts = Object.entries(state.picks)
    .filter(([k]) => state.touched.has(k))
    .map(([k, v]) => `${k}=${v}`);
  if (state.picks.accent === 'custom' && state.customAccent)
    parts.push(`hex=${state.customAccent.hex.slice(1)}`);
  history.replaceState(null, '', parts.length ? '#' + parts.join(';') : location.pathname);
}

function readHash() {
  if (!location.hash || location.hash.length < 2) return false;
  const pairs = location.hash.slice(1).split(';').map(p => p.split('='));
  let any = false;
  for (const [k, v] of pairs) {
    if (k === 'hex') {
      state.customAccent = { hex: '#' + v, ink: bestInk('#' + v), label: `custom #${v}` };
    } else if (DIMENSIONS.some(d => d.id === k) && (v === 'custom' || optionByKey(k, v))) {
      state.picks[k] = v;
      state.touched.add(k);
      any = true;
    }
  }
  return any;
}

function persist() {
  localStorage.setItem('taste-state', JSON.stringify({
    picks: state.picks, touched: [...state.touched], preset: state.preset,
    customAccent: state.customAccent, notes: state.notes,
  }));
}

function restore() {
  try {
    const raw = localStorage.getItem('taste-state');
    if (!raw) return;
    const s = JSON.parse(raw);
    Object.assign(state.picks, s.picks || {});
    state.touched = new Set(s.touched || []);
    state.preset = s.preset || null;
    state.customAccent = s.customAccent || null;
    state.notes = s.notes || '';
  } catch { /* fresh start */ }
}

/* ---------- top actions ---------- */

function wireActions() {
  document.getElementById('flipMode').addEventListener('click', () => {
    state.previewMode = currentMode() === 'dark' ? 'light' : 'dark';
    applyState();
  });
  document.getElementById('shuffle').addEventListener('click', () => {
    for (const dim of DIMENSIONS) {
      const opts = dim.options;
      state.picks[dim.id] = opts[Math.floor(Math.random() * opts.length)].key;
      state.touched.add(dim.id);
    }
    state.preset = null;
    renderPresets();
    applyState();
  });
  document.getElementById('reset').addEventListener('click', () => {
    state.picks = { ...DEFAULTS };
    state.touched = new Set();
    state.preset = null;
    state.previewMode = null;
    state.customAccent = null;
    renderPresets();
    applyState();
  });
  document.getElementById('export').addEventListener('click', openExport);
  document.getElementById('closeExport').addEventListener('click', () =>
    document.getElementById('exportDialog').close());
  document.querySelectorAll('[data-copy]').forEach(b => b.addEventListener('click', async () => {
    const text = b.dataset.copy === 'prose' ? buildProse() : buildJson();
    await navigator.clipboard.writeText(text);
    const old = b.textContent; b.textContent = 'copied ✓';
    setTimeout(() => { b.textContent = old; }, 1200);
  }));
  document.getElementById('previewFab').addEventListener('click', () =>
    document.getElementById('preview').classList.toggle('open'));

  // rail active highlight
  const obs = new IntersectionObserver(entries => {
    for (const e of entries) {
      if (e.isIntersecting) {
        document.querySelectorAll('.rail-item').forEach(i =>
          i.classList.toggle('active', i.dataset.dim === e.target.dataset.dim));
      }
    }
  }, { rootMargin: '-20% 0px -70% 0px' });
  document.querySelectorAll('.dim').forEach(s => obs.observe(s));
}

/* ---------- boot ---------- */

restore();
readHash();
renderPresets();
renderSections();
wireActions();
applyState();

})();
