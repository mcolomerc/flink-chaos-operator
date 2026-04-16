import { useEffect } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { useAppStore } from '../store'

/**
 * Subscribes to the /api/events Server-Sent Events stream.
 * On receiving a `chaosrun_update` event, invalidates the topology and
 * chaosRuns React Query caches so they refetch immediately rather than
 * waiting for the next 5-second poll.
 */
export function useSSE() {
  const queryClient = useQueryClient()
  const selectedDeploymentName = useAppStore((s) => s.selectedDeploymentName)

  useEffect(() => {
    const es = new EventSource('/api/events')

    es.onmessage = (event: MessageEvent) => {
      try {
        const msg = JSON.parse(event.data as string) as { type: string }
        if (msg.type === 'chaosrun_update') {
          void queryClient.invalidateQueries({ queryKey: ['topology', selectedDeploymentName] })
          void queryClient.invalidateQueries({ queryKey: ['chaosRuns', selectedDeploymentName ?? undefined] })
        }
      } catch {
        // ignore malformed frames
      }
    }

    // EventSource reconnects automatically on error — no action needed.
    es.onerror = () => {}

    return () => {
      es.close()
    }
  }, [queryClient, selectedDeploymentName])
}
