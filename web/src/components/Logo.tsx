/**
 * The Argus mark: Argus Panoptes, the watchman whose hundred eyes never all
 * slept. One great eye inside a ring of eight lesser ones, drawn as a seal.
 * It paints in currentColor plus one accent, so it works on either theme.
 */
export function Logo({ size = 30, title }: { size?: number; title?: string }) {
  const eyes = [
    [27.6, 16],
    [24.2, 7.8],
    [16, 4.4],
    [7.8, 7.8],
    [4.4, 16],
    [7.8, 24.2],
    [16, 27.6],
    [24.2, 24.2],
  ] as const

  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 32 32"
      fill="none"
      role="img"
      aria-label={title ?? 'Argus'}
    >
      <circle cx="16" cy="16" r="14.5" stroke="var(--accent)" strokeWidth="1.1" opacity="0.55" />
      {eyes.map(([cx, cy]) => (
        <circle key={`${cx}-${cy}`} cx={cx} cy={cy} r="1.3" fill="var(--accent-ink)" />
      ))}
      <path
        d="M9 16 Q16 10.2 23 16 Q16 21.8 9 16 Z"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <circle cx="16" cy="16" r="2.4" fill="currentColor" />
    </svg>
  )
}
