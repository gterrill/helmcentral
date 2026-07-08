import { Skeleton } from 'helmcentral-dashboard'

export function TextLines() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, width: 220 }}>
      <Skeleton style={{ height: 12, width: '80%' }} />
      <Skeleton style={{ height: 12, width: '95%' }} />
      <Skeleton style={{ height: 12, width: '60%' }} />
    </div>
  )
}

export function AvatarWithText() {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
      <Skeleton style={{ height: 40, width: 40, borderRadius: '9999px' }} />
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        <Skeleton style={{ height: 12, width: 140 }} />
        <Skeleton style={{ height: 10, width: 90 }} />
      </div>
    </div>
  )
}

export function CardPlaceholder() {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, width: 260 }}>
      <Skeleton style={{ height: 120, width: '100%' }} />
      <Skeleton style={{ height: 14, width: '70%' }} />
      <Skeleton style={{ height: 14, width: '40%' }} />
    </div>
  )
}
