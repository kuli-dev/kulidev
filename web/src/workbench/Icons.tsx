// Small line icons for the activity bar (24px grid, currentColor).
const P = { fill: "none", stroke: "currentColor", strokeWidth: 1.6, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };

export const Icon = {
  files: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5" /></svg>),
  search: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><circle cx="11" cy="11" r="6.5" /><path d="m20 20-4.2-4.2" /></svg>),
  map: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><rect x="3" y="4" width="6" height="5" /><rect x="15" y="4" width="6" height="5" /><rect x="9" y="15" width="6" height="5" /><path d="M6 9v3h12V9M12 12v3" /></svg>),
  docs: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><path d="M4 5.5A1.5 1.5 0 0 1 5.5 4H11v16H5.5A1.5 1.5 0 0 1 4 18.5zM20 5.5A1.5 1.5 0 0 0 18.5 4H13v16h5.5a1.5 1.5 0 0 0 1.5-1.5z" /></svg>),
  issues: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><path d="M12 3 2.5 20h19z" /><path d="M12 10v4M12 17.2v.3" /></svg>),
  chat: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><path d="M4 5h16v11H9l-5 4z" /><path d="M8 9.5h8M8 12.5h5" /></svg>),
  agent: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><rect x="5" y="8" width="14" height="11" rx="2" /><path d="M12 4v4M9 13h.01M15 13h.01M9.5 16.5h5" /></svg>),
  home: () => (<svg viewBox="0 0 24 24" width="24" height="24" {...P}><path d="M4 11 12 4l8 7v9H4z" /><path d="M10 20v-5h4v5" /></svg>),
  close: () => (<svg viewBox="0 0 16 16" width="14" height="14" {...P}><path d="m4 4 8 8M12 4l-8 8" /></svg>),
  chevron: ({ open }: { open: boolean }) => (
    <svg viewBox="0 0 16 16" width="14" height="14" {...P} style={{ transform: open ? "rotate(90deg)" : undefined }}><path d="m6 4 4 4-4 4" /></svg>
  ),
};
