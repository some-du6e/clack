// icons.js: hand-drawn 20x20 SVG icons (ui-core). Not Slack's assets; drawn to match their weight and size.

const S = (body, extra = '') =>
  `<svg viewBox="0 0 20 20" width="20" height="20" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"${extra}>${body}</svg>`;
const F = (body) => `<svg viewBox="0 0 20 20" width="20" height="20" fill="currentColor" aria-hidden="true">${body}</svg>`;

function gearPath() {
  // 8-tooth gear outline around (10,10).
  const teeth = 8, ro = 7.4, ri = 5.6, pts = [];
  for (let i = 0; i < teeth; i++) {
    const a = (i / teeth) * Math.PI * 2;
    const w = Math.PI / teeth * 0.52;
    for (const [ang, r] of [[a - w * 1.05, ri], [a - w * 0.6, ro], [a + w * 0.6, ro], [a + w * 1.05, ri]]) {
      pts.push(`${(10 + r * Math.cos(ang)).toFixed(2)} ${(10 + r * Math.sin(ang)).toFixed(2)}`);
    }
  }
  return `M${pts.join('L')}Z`;
}

const bubble = 'M10 3.25c3.9 0 7 2.6 7 5.9s-3.1 5.9-7 5.9c-.8 0-1.6-.1-2.3-.3L4 16.5l.9-3.1C3.7 12.3 3 10.8 3 9.15c0-3.3 3.1-5.9 7-5.9z';

export const icons = {
  back: S('<path d="M16 10H4.5M9 5 4 10l5 5"/>'),
  forward: S('<path d="M4 10h11.5M11 5l5 5-5 5"/>'),
  history: S('<circle cx="10" cy="10" r="7"/><path d="M10 6.25V10l2.5 2"/>'),
  search: S('<circle cx="8.75" cy="8.75" r="5.25"/><path d="m12.75 12.75 3.75 3.75"/>'),
  help: S('<circle cx="10" cy="10" r="7"/><path d="M8 8.1a2 2 0 1 1 2.6 1.9c-.4.15-.6.5-.6.9v.6"/><circle cx="10" cy="13.6" r=".4" fill="currentColor"/>'),
  home: S('<path d="M3.75 8.6 10 3.5l6.25 5.1v7.15c0 .41-.34.75-.75.75H12.5v-4.75h-5v4.75h-3c-.41 0-.75-.34-.75-.75z"/>'),
  homeFilled: F('<path d="M9.53 2.9a.75.75 0 0 1 .94 0l6.25 5.1c.18.14.28.36.28.6v7.15c0 .83-.67 1.5-1.5 1.5H12.5a.75.75 0 0 1-.75-.75v-4h-3.5v4a.75.75 0 0 1-.75.75h-3a1.5 1.5 0 0 1-1.5-1.5V8.6c0-.24.1-.46.28-.6z"/>'),
  dms: S('<path d="M7.5 3.5h7a2.25 2.25 0 0 1 2.25 2.25v4.5c0 1.24-1 2.25-2.25 2.25h-.5v2.25l-2.75-2.25H7.5a2.25 2.25 0 0 1-2.25-2.25v-4.5C5.25 4.5 6.26 3.5 7.5 3.5z"/><path d="M5.25 7H4.5a1.25 1.25 0 0 0-1.25 1.25v4c0 .69.56 1.25 1.25 1.25H5v2l2.25-2h2"/>'),
  dmsFilled: F('<path d="M7.5 2.75h7a3 3 0 0 1 3 3v4.5a3 3 0 0 1-2.25 2.9v1.6a.75.75 0 0 1-1.22.58l-2.55-2.08H7.5a3 3 0 0 1-3-3v-4.5a3 3 0 0 1 3-3"/><path d="M3.75 6.6v5.65c0 .28.22.5.5.5h.75v1.6l1.87-1.6H10l1 .75H7.6l-2.72 2.33A.75.75 0 0 1 3.75 15.3v-1.95A1.75 1.75 0 0 1 2.5 11.6v-3.4c0-.7.5-1.3 1.25-1.6"/>'),
  activity: S('<path d="M10 3.25a4.5 4.5 0 0 0-4.5 4.5v2.9L4 13.75h12l-1.5-3.1v-2.9a4.5 4.5 0 0 0-4.5-4.5zM8 16.25a2.1 2.1 0 0 0 4 0"/>'),
  activityFilled: F('<path d="M10 2.5a5.25 5.25 0 0 0-5.25 5.25v2.73L3.32 13.4a.75.75 0 0 0 .68 1.1h12a.75.75 0 0 0 .68-1.1l-1.43-2.92V7.75C15.25 4.85 12.9 2.5 10 2.5M7.4 15.75a2.75 2.75 0 0 0 5.2 0z"/>'),
  files: S('<rect x="6.25" y="3.25" width="10.5" height="10.5" rx="2.25"/><path d="M13.75 16.75H5.5a2.25 2.25 0 0 1-2.25-2.25V6.25"/>'),
  filesFilled: F('<rect x="5.5" y="2.5" width="12" height="12" rx="3"/><path d="M2.5 6.25a.75.75 0 0 1 1.5 0v8.25c0 .83.67 1.5 1.5 1.5h8.25a.75.75 0 0 1 0 1.5H5.5a3 3 0 0 1-3-3z"/>'),
  later: S('<path d="M5.75 3.25h8.5c.41 0 .75.34.75.75v12.5L10 13.25l-5 3.25V4c0-.41.34-.75.75-.75z"/>'),
  todos: S('<circle cx="10" cy="10" r="7"/><path d="m7 10.25 2 2 4-4.5"/>'),
  todosFilled: F('<path d="M10 2.25a7.75 7.75 0 1 0 0 15.5 7.75 7.75 0 0 0 0-15.5m3.56 6.07-4 4.5a.75.75 0 0 1-1.09.03l-2-2a.75.75 0 1 1 1.06-1.06l1.44 1.44 3.47-3.9a.75.75 0 1 1 1.12 1z"/>'),
  more: S('<circle cx="6" cy="6" r="2.25"/><circle cx="14" cy="6" r="2.25"/><circle cx="6" cy="14" r="2.25"/><path d="M14 11.5v5M11.5 14h5"/>'),
  plus: S('<path d="M10 4.25v11.5M4.25 10h11.5"/>'),
  plusSmall: S('<path d="M10 5.5v9M5.5 10h9"/>'),
  moon: S('<path d="M15.75 12.4A6.75 6.75 0 0 1 7.6 4.25 6.75 6.75 0 1 0 15.75 12.4z"/>'),
  sun: S('<circle cx="10" cy="10" r="3.25"/><path d="M10 2.75v1.5M10 15.75v1.5M2.75 10h1.5M15.75 10h1.5M4.9 4.9l1.05 1.05M14.05 14.05l1.05 1.05M4.9 15.1l1.05-1.05M14.05 5.95l1.05-1.05"/>'),
  chevronDown: S('<path d="m5.5 8 4.5 4.5L14.5 8"/>'),
  chevronRight: S('<path d="m8 5.5 4.5 4.5L8 14.5"/>'),
  chevronUp: S('<path d="m5.5 12 4.5-4.5 4.5 4.5"/>'),
  caretDown: F('<path d="M6.2 8.2a.6.6 0 0 1 .45-1h6.7a.6.6 0 0 1 .45 1l-3.35 3.7a.6.6 0 0 1-.9 0z"/>'),
  caretRight: F('<path d="M8.2 6.2a.6.6 0 0 0-1 .45v6.7a.6.6 0 0 0 1 .45l3.7-3.35a.6.6 0 0 0 0-.9z"/>'),
  gear: S(`<path d="${gearPath()}"/><circle cx="10" cy="10" r="2.4"/>`),
  compose: S('<path d="M9.25 4H5.5A1.75 1.75 0 0 0 3.75 5.75v8.75c0 .97.78 1.75 1.75 1.75h8.75c.97 0 1.75-.78 1.75-1.75v-3.75"/><path d="M14.4 3.35a1.3 1.3 0 0 1 1.85 0l.4.4a1.3 1.3 0 0 1 0 1.85L10.4 11.85 7.75 12.25l.4-2.65z"/>'),
  filter: S('<path d="M3.5 5.25h13M3.5 9.25h5M3.5 13.25h4"/><circle cx="12.75" cy="12" r="2.75"/><path d="m14.8 14.05 1.95 1.95"/>'),
  threads: S(`<path d="${bubble}"/><path d="M7 8.25h6M7 11h4"/>`),
  headphones: S('<path d="M3.75 13.5V10a6.25 6.25 0 0 1 12.5 0v3.5"/><path d="M3.75 12.25h1.5c.69 0 1.25.56 1.25 1.25v1.75c0 .69-.56 1.25-1.25 1.25H5a1.25 1.25 0 0 1-1.25-1.25zM16.25 12.25h-1.5c-.69 0-1.25.56-1.25 1.25v1.75c0 .69.56 1.25 1.25 1.25H15c.69 0 1.25-.56 1.25-1.25z"/>'),
  drafts: S('<path d="m3.25 9.6 13.25-6.1-3.6 13.25-3.15-4.85z"/><path d="m9.75 11.9 6.75-8.4"/>'),
  send: F('<path d="M3.2 2.4a.75.75 0 0 0-1.03.86l1.65 6.08a.5.5 0 0 0 .42.37l6.3.79-6.3.79a.5.5 0 0 0-.42.37l-1.65 6.08a.75.75 0 0 0 1.03.86l14.5-7.06a.75.75 0 0 0 0-1.35z"/>'),
  saved: S('<path d="M5.75 3.25h8.5c.41 0 .75.34.75.75v12.5L10 13.25l-5 3.25V4c0-.41.34-.75.75-.75z"/>'),
  savedFilled: F('<path d="M5.75 2.5h8.5c.83 0 1.5.67 1.5 1.5v12.5a.75.75 0 0 1-1.16.63L10 14.15l-4.59 2.98A.75.75 0 0 1 4.25 16.5V4c0-.83.67-1.5 1.5-1.5"/>'),
  directories: S('<rect x="4.25" y="3.25" width="11.5" height="13.5" rx="1.75"/><circle cx="10" cy="8.5" r="1.9"/><path d="M6.9 13.5a3.3 3.3 0 0 1 6.2 0"/>'),
  hash: S('<path d="M8.25 3.5 6.75 16.5M13.25 3.5l-1.5 13M4.25 7.5h12M3.75 12.5h12"/>'),
  hashSmall: S('<path d="M8.5 4.5 7.25 15.5M12.75 4.5 11.5 15.5M5 8h10.5M4.5 12h10.5"/>', ' stroke-width="1.6"'),
  lock: S('<rect x="4.75" y="8.75" width="10.5" height="8" rx="1.75"/><path d="M7 8.75V6.5a3 3 0 0 1 6 0v2.25"/>'),
  star: S('<path d="m10 3.25 2.05 4.2 4.6.65-3.33 3.25.8 4.6L10 13.78 5.88 15.95l.8-4.6L3.35 8.1l4.6-.65z"/>'),
  starFilled: F('<path d="M10.67 2.92a.75.75 0 0 0-1.34 0L7.45 6.77l-4.21.6a.75.75 0 0 0-.42 1.29l3.05 2.97-.72 4.2a.75.75 0 0 0 1.09.79L10 14.63l3.77 1.99a.75.75 0 0 0 1.09-.79l-.72-4.2 3.05-2.97a.75.75 0 0 0-.42-1.28l-4.21-.61z"/>'),
  bell: S('<path d="M10 3.25a4.5 4.5 0 0 0-4.5 4.5v2.9L4 13.75h12l-1.5-3.1v-2.9a4.5 4.5 0 0 0-4.5-4.5zM8 16.25a2.1 2.1 0 0 0 4 0"/>'),
  bellOff: S('<path d="M14.5 10.6V7.75a4.5 4.5 0 0 0-7.6-3.25M5.5 7.75v2.9L4 13.75h9.75M8 16.25a2.1 2.1 0 0 0 4 0M3.5 3.5l13 13"/>'),
  kebab: F('<circle cx="10" cy="4.5" r="1.5"/><circle cx="10" cy="10" r="1.5"/><circle cx="10" cy="15.5" r="1.5"/>'),
  dots: F('<circle cx="4.5" cy="10" r="1.5"/><circle cx="10" cy="10" r="1.5"/><circle cx="15.5" cy="10" r="1.5"/>'),
  close: S('<path d="m5 5 10 10M15 5 5 15"/>'),
  emoji: S('<circle cx="10" cy="10" r="7"/><path d="M7 11.75a3.5 3.5 0 0 0 6 0"/><circle cx="7.6" cy="8.2" r=".6" fill="currentColor"/><circle cx="12.4" cy="8.2" r=".6" fill="currentColor"/>'),
  addReaction: S('<path d="M16.9 9.2a7 7 0 1 1-6.1-6.15"/><path d="M7 11.75a3.5 3.5 0 0 0 6 0"/><circle cx="7.6" cy="8.2" r=".6" fill="currentColor"/><circle cx="12.4" cy="8.2" r=".6" fill="currentColor"/><path d="M15.25 2.25v5M12.75 4.75h5"/>'),
  reply: S(`<path d="${bubble}"/>`),
  forward: S('<path d="M11.25 4.5 16.5 9l-5.25 4.5"/><path d="M16.25 9H9.5a5.25 5.25 0 0 0-5.25 5.25v1.5"/>'),
  bookmark: S('<path d="M5.75 3.25h8.5c.41 0 .75.34.75.75v12.5L10 13.25l-5 3.25V4c0-.41.34-.75.75-.75z"/>'),
  edit: S('<path d="M13.4 3.6a1.75 1.75 0 0 1 2.5 0l.5.5a1.75 1.75 0 0 1 0 2.5L7.5 15.5l-3.75 1 1-3.75z"/><path d="m12 5 3 3"/>'),
  trash: S('<path d="M4 5.75h12M8.25 5.75V4.5c0-.55.45-1 1-1h1.5c.55 0 1 .45 1 1v1.25M5.5 5.75l.7 9.6c.06.8.73 1.4 1.53 1.4h4.54c.8 0 1.47-.6 1.53-1.4l.7-9.6"/>'),
  bold: S('<path d="M6.25 3.75h4.6a3 3 0 0 1 0 6H6.25zM6.25 9.75h5.35a3.25 3.25 0 0 1 0 6.5H6.25z"/>', ' stroke-width="2.1"'),
  italic: S('<path d="M9 3.75h5.5M5.5 16.25H11M11.75 3.75l-3.5 12.5"/>', ' stroke-width="1.7"'),
  underline: S('<path d="M6 3.5v5.75a4 4 0 0 0 8 0V3.5M5 16.75h10"/>', ' stroke-width="1.7"'),
  strike: S('<path d="M3.75 10h12.5M13.4 6.4c-.45-1.45-1.8-2.4-3.5-2.4-2.1 0-3.65 1.2-3.65 2.85 0 .95.5 1.65 1.3 2.15M7 13.4c.4 1.6 1.85 2.6 3.35 2.6 2.15 0 3.65-1.2 3.65-2.85 0-.45-.1-.85-.3-1.2"/>'),
  link: S('<path d="M8.6 11.4a3 3 0 0 0 4.25 0l2.4-2.4a3 3 0 0 0-4.25-4.25l-.8.8"/><path d="M11.4 8.6a3 3 0 0 0-4.25 0l-2.4 2.4A3 3 0 0 0 9 15.25l.8-.8"/>'),
  ol: S('<path d="M9 5.5h7.5M9 10h7.5M9 14.5h7.5"/><path d="M3.75 4.25 4.75 3.75v4" stroke-width="1.25"/><path d="M3.6 11.6c.15-.6.6-.95 1.15-.95.6 0 1.05.4 1.05.95 0 .9-2.2 1.9-2.2 2.9h2.3" stroke-width="1.25"/>'),
  ul: S('<path d="M8.5 5.5h8M8.5 10h8M8.5 14.5h8"/><circle cx="4.5" cy="5.5" r=".9" fill="currentColor"/><circle cx="4.5" cy="10" r=".9" fill="currentColor"/><circle cx="4.5" cy="14.5" r=".9" fill="currentColor"/>'),
  quote: S('<path d="M4.5 4v12M8.5 5.5h7.5M8.5 10h7.5M8.5 14.5h5"/>'),
  code: S('<path d="M7.25 5.75 3 10l4.25 4.25M12.75 5.75 17 10l-4.25 4.25"/>'),
  codeBlock: S('<path d="M5 3.75h10c.69 0 1.25.56 1.25 1.25v10c0 .69-.56 1.25-1.25 1.25H5c-.69 0-1.25-.56-1.25-1.25V5c0-.69.56-1.25 1.25-1.25z"/><path d="M8.25 7.75 6 10l2.25 2.25M11.75 7.75 14 10l-2.25 2.25"/>'),
  at: S('<circle cx="10" cy="10" r="3"/><path d="M13 7v3.75a2.25 2.25 0 0 0 4.25 1V10A7.25 7.25 0 1 0 13.5 16.3"/>'),
  video: S('<rect x="2.75" y="5.25" width="10.5" height="9.5" rx="2"/><path d="m13.25 8.75 4-2.25v7l-4-2.25"/>'),
  mic: S('<rect x="7.25" y="2.75" width="5.5" height="9" rx="2.75"/><path d="M4.75 9.5a5.25 5.25 0 0 0 10.5 0M10 14.75v2.5"/>'),
  slash: S('<rect x="3.25" y="3.25" width="13.5" height="13.5" rx="2.5"/><path d="m11.75 6.5-3.5 7"/>'),
  formatting: `<svg viewBox="0 0 20 20" width="20" height="20" aria-hidden="true"><text x="0.5" y="14.5" font-family="Slack-Lato, Lato, sans-serif" font-size="15" font-weight="400" fill="currentColor" letter-spacing="-.3">Aa</text><path d="M1 17.6h18" stroke="currentColor" stroke-width="1.3"/></svg>`,
  attach: S('<path d="M10 4.25v11.5M4.25 10h11.5"/>'),
  messages: F(`<path d="M10 2.5c4.24 0 7.75 2.9 7.75 6.65S14.24 15.8 10 15.8c-.76 0-1.5-.09-2.2-.27l-3.94 1.6a.6.6 0 0 1-.8-.73l.88-3.03C2.85 12.25 2.25 10.75 2.25 9.15 2.25 5.4 5.76 2.5 10 2.5"/>`),
  canvas: S('<path d="M5.5 3.25h6.25l3.5 3.5v9.25c0 .41-.34.75-.75.75h-9a.75.75 0 0 1-.75-.75V4c0-.41.34-.75.75-.75z"/><path d="M11.5 3.5V7h3.5M7.5 10.5h5M7.5 13.25h3.5"/>'),
  list: S('<rect x="3.25" y="3.25" width="13.5" height="13.5" rx="2.25"/><path d="M8 7.25h5.5M8 10h5.5M8 12.75h5.5"/><circle cx="5.9" cy="7.25" r=".5" fill="currentColor"/><circle cx="5.9" cy="10" r=".5" fill="currentColor"/><circle cx="5.9" cy="12.75" r=".5" fill="currentColor"/>'),
  folder: S('<path d="M3.25 6V5c0-.69.56-1.25 1.25-1.25h3.3l1.7 1.75h6c.69 0 1.25.56 1.25 1.25V15c0 .69-.56 1.25-1.25 1.25h-11c-.69 0-1.25-.56-1.25-1.25z"/>'),
  pin: S('<path d="M12 3.25 16.75 8l-2 .75-2.75 2.75-.25 3L10 16.25 3.75 10 5.5 8.25l3-.25 2.75-2.75z"/><path d="m6.75 13.25-3.5 3.5"/>'),
  eye: S('<path d="M2.25 10S5 4.75 10 4.75 17.75 10 17.75 10 15 15.25 10 15.25 2.25 10 2.25 10z"/><circle cx="10" cy="10" r="2.5"/>'),
  user: S('<circle cx="10" cy="7" r="3.25"/><path d="M3.75 16.5c.75-3 3.25-4.75 6.25-4.75s5.5 1.75 6.25 4.75"/>'),
  users: S('<circle cx="7.5" cy="7.25" r="2.75"/><path d="M2.5 16c.6-2.6 2.6-4 5-4s4.4 1.4 5 4"/><path d="M12.5 4.75a2.6 2.6 0 0 1 0 5M14.25 12.25c1.6.45 2.75 1.7 3.25 3.75"/>'),
  info: S('<circle cx="10" cy="10" r="7"/><path d="M10 9v4.5"/><circle cx="10" cy="6.5" r=".5" fill="currentColor"/>'),
  check: S('<path d="m4.5 10.5 3.5 3.5 7.5-8"/>'),
  arrowDown: S('<path d="M10 4v12M5 11l5 5 5-5"/>'),
  arrowUp: S('<path d="M10 16V4M5 9l5-5 5 5"/>'),
  copyLink: S('<path d="M8.6 11.4a3 3 0 0 0 4.25 0l2.4-2.4a3 3 0 0 0-4.25-4.25l-.8.8"/><path d="M11.4 8.6a3 3 0 0 0-4.25 0l-2.4 2.4A3 3 0 0 0 9 15.25l.8-.8"/>'),
  apps: S('<rect x="3.5" y="3.5" width="5" height="5" rx="1.25"/><rect x="11.5" y="3.5" width="5" height="5" rx="1.25"/><rect x="3.5" y="11.5" width="5" height="5" rx="1.25"/><rect x="11.5" y="11.5" width="5" height="5" rx="1.25"/>'),
  messageAdd: S('<path d="M10 4.5v11M4.5 10h11"/>'),
  paragraph: S('<path d="M8.5 16.5V3.5h6.5M12.25 3.5v13M8.5 3.5H8a3.6 3.6 0 0 0 0 7.2h.5"/>'),
  paragraphOld: S('<path d="M9 16.25V3.75h6M12.5 3.75v12.5M9 3.75H8a3.5 3.5 0 0 0 0 7h1"/>'),
  emojiSmall: S('<circle cx="10" cy="10" r="6.25"/><path d="M7.4 11.5a3 3 0 0 0 5.2 0"/><circle cx="7.9" cy="8.4" r=".55" fill="currentColor"/><circle cx="12.1" cy="8.4" r=".55" fill="currentColor"/>'),
  sparkle: S('<path d="M10 3c.4 3.5 1.6 4.8 5 5.25-3.4.45-4.6 1.75-5 5.25-.4-3.5-1.6-4.8-5-5.25C8.4 7.8 9.6 6.5 10 3zM15 12.5c.2 1.5.75 2.05 2 2.25-1.25.2-1.8.75-2 2.25-.2-1.5-.75-2.05-2-2.25 1.25-.2 1.8-.75 2-2.25z"/>'),
  hashBox: S('<rect x="3.25" y="3.25" width="13.5" height="13.5" rx="3"/><path d="M8.6 6.5 7.6 13.5M12.4 6.5l-1 7M6.25 8.6h7.5M6 11.4h7.5"/>', ' stroke-width="1.3"'),
  bubble: S(`<path d="${bubble}"/>`),
  appsDots: S('<circle cx="6.5" cy="6.5" r="2.25"/><circle cx="13.5" cy="6.5" r="2.25"/><circle cx="6.5" cy="13.5" r="2.25"/><circle cx="13.5" cy="13.5" r="2.25"/>'),
  slackbot: F('<circle cx="10" cy="10" r="8"/>'),
};

// icon() returns an icon at a given pixel size, for inline use in templates.
export function icon(name, size = 20, cls = '') {
  const s = icons[name] || '';
  return s.replace('<svg ', `<svg${cls ? ` class="${cls}"` : ''} `).replace('width="20" height="20"', `width="${size}" height="${size}"`);
}
