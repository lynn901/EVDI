import type { SignalingMessage } from '../types/signaling'

export class SignalingClient {
  private ws: WebSocket | null = null
  private onMessage: ((msg: SignalingMessage) => void) | null = null

  connect(url: string, onMessage: (msg: SignalingMessage) => void): Promise<void> {
    this.onMessage = onMessage

    return new Promise((resolve, reject) => {
      this.ws = new WebSocket(url)

      this.ws.onopen = () => {
        console.log('[Signaling] WebSocket connected')
        resolve()
      }

      this.ws.onerror = () => {
        reject(new Error('WebSocket connection failed'))
      }

      this.ws.onmessage = (event) => {
        try {
          const msg: SignalingMessage = JSON.parse(event.data)
          console.log('[Signaling] Received:', msg.type)
          this.onMessage?.(msg)
        } catch {
          console.error('Failed to parse signaling message')
        }
      }

      this.ws.onclose = () => {
        console.log('[Signaling] WebSocket closed')
      }
    })
  }

  send(msg: SignalingMessage): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(msg))
      console.log('[Signaling] Sent:', msg.type)
    } else {
      console.warn('[Signaling] Cannot send, ws state:', this.ws?.readyState)
    }
  }

  isOpen(): boolean {
    return this.ws?.readyState === WebSocket.OPEN
  }

  disconnect(): void {
    this.onMessage = null
    this.ws?.close()
    this.ws = null
  }
}
