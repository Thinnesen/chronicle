// BrandMark is the logo: a small graph of three nodes.
export function BrandMark({ size = 22 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
      <rect x="1" y="1" width="30" height="30" rx="8" fill="var(--accent)" />
      <path d="M10 16h4c2 0 3-6 8-6M14 16c2 0 3 6 8 6" fill="none" stroke="var(--accent-ink)" strokeWidth="2.2" strokeLinecap="round" />
      <circle cx="9" cy="16" r="3.2" fill="var(--accent-ink)" />
      <circle cx="23" cy="10" r="3.2" fill="var(--accent-ink)" />
      <circle cx="23" cy="22" r="3.2" fill="var(--accent-ink)" />
    </svg>
  );
}
