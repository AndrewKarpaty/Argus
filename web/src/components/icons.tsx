/** Inline stroke icons. Decorative by default; pass a label to announce one. */
type Props = { size?: number; className?: string; label?: string }

function base(label?: string) {
  return label
    ? ({ role: 'img', 'aria-label': label } as const)
    : ({ 'aria-hidden': true } as const)
}

const stroke = {
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.6,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
} as const

export function IncidentIcon({ size = 16, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" className={className} {...stroke} {...base(label)}>
      <path d="M8 1.8 1.8 13h12.4L8 1.8Z" />
      <path d="M8 6.4v3.1" />
      <path d="M8 11.4h0" />
    </svg>
  )
}

export function TopologyIcon({ size = 16, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" className={className} {...stroke} {...base(label)}>
      <circle cx="3.4" cy="8" r="1.9" />
      <circle cx="12.6" cy="4" r="1.9" />
      <circle cx="12.6" cy="12" r="1.9" />
      <path d="M5.1 7.1 10.9 4.6" />
      <path d="M5.1 8.9 10.9 11.4" />
    </svg>
  )
}

export function CheckIcon({ size = 14, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" className={className} {...stroke} {...base(label)}>
      <circle cx="7" cy="7" r="5.6" />
      <path d="M4.4 7.2 6.2 9l3.4-4" />
    </svg>
  )
}

export function DashIcon({ size = 14, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" className={className} {...stroke} {...base(label)}>
      <circle cx="7" cy="7" r="5.6" />
      <path d="M4.4 7h5.2" />
    </svg>
  )
}

export function CrossIcon({ size = 14, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" className={className} {...stroke} {...base(label)}>
      <circle cx="7" cy="7" r="5.6" />
      <path d="M5 5l4 4" />
      <path d="M9 5l-4 4" />
    </svg>
  )
}

export function SpinnerIcon({ size = 14, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" className={className} {...stroke} {...base(label)}>
      <path d="M12.3 7a5.3 5.3 0 1 1-1.8-4" />
    </svg>
  )
}

export function PlusIcon({ size = 16, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" className={className} {...stroke} strokeWidth={1.8} {...base(label)}>
      <path d="M8 3.2v9.6" />
      <path d="M3.2 8h9.6" />
    </svg>
  )
}

export function ArrowRightIcon({ size = 16, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" className={className} {...stroke} {...base(label)}>
      <path d="M3.2 8h9.6" />
      <path d="M9.2 4.4 12.8 8l-3.6 3.6" />
    </svg>
  )
}

export function ArrowLeftIcon({ size = 14, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 14 14" className={className} {...stroke} {...base(label)}>
      <path d="M8.4 3.2 4.6 7l3.8 3.8" />
    </svg>
  )
}

export function RefreshIcon({ size = 16, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" className={className} {...stroke} {...base(label)}>
      <path d="M13.6 8a5.6 5.6 0 1 1-1.9-4.2" />
      <path d="M13.4 2.2v3.2h-3.2" />
    </svg>
  )
}

export function ThemeIcon({ size = 16, className, label }: Props) {
  return (
    <svg width={size} height={size} viewBox="0 0 16 16" className={className} {...stroke} {...base(label)}>
      <circle cx="8" cy="8" r="3.4" />
      <path d="M8 1.4v1.6" />
      <path d="M8 13v1.6" />
      <path d="M1.4 8h1.6" />
      <path d="M13 8h1.6" />
      <path d="M3.3 3.3l1.2 1.2" />
      <path d="M11.5 11.5l1.2 1.2" />
      <path d="M12.7 3.3l-1.2 1.2" />
      <path d="M4.5 11.5l-1.2 1.2" />
    </svg>
  )
}
