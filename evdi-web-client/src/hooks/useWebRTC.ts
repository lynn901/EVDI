import { useCallback, useRef } from 'react'
import { SignalingClient } from '../utils/signaling'
import { useConnectionStore } from '../stores/connectionStore'
import type { SignalingMessage, SDPPayload, ICEPayload } from '../types/signaling'

export function useWebRTC() {
  const pcRef = useRef<RTCPeerConnection | null>(null)
  const signalingRef = useRef<SignalingClient | null>(null)
  const controlChannelRef = useRef<RTCDataChannel | null>(null)
  const bulkChannelRef = useRef<RTCDataChannel | null>(null)

  const {
    agentAddress,
    setConnectionState,
    setMediaStream,
    setError,
    nextSeq,
    setSendInputFn,
    reset,
  } = useConnectionStore()

  const connect = useCallback(async () => {
    try {
      setConnectionState('connecting')
      setError(null)

      const pc = new RTCPeerConnection({ iceServers: [] })
      pcRef.current = pc

      // Queue ICE candidates until remote description is set
      const pendingICE: ICEPayload[] = []

      // Merge all remote tracks into a single MediaStream.
      const combinedStream = new MediaStream()
      pc.ontrack = (event) => {
        console.log('[WebRTC] ontrack:', event.track.kind, 'streams:', event.streams.length)
        combinedStream.addTrack(event.track)
        const merged = new MediaStream(combinedStream.getTracks())
        console.log('[WebRTC] merged stream tracks:', merged.getTracks().map(t => t.kind))
        // Create a new reference so Zustand detects the change
        setMediaStream(merged)
      }

      pc.onconnectionstatechange = () => {
        console.log('[WebRTC] Connection state:', pc.connectionState)
        switch (pc.connectionState) {
          case 'connected':
            setConnectionState('connected')
            break
          case 'disconnected':
          case 'failed':
            setConnectionState('disconnected')
            setError('连接已断开')
            break
        }
      }

      pc.oniceconnectionstatechange = () => {
        console.log('[WebRTC] ICE connection state:', pc.iceConnectionState)
      }

      pc.addTransceiver('video', { direction: 'recvonly' })
      pc.addTransceiver('audio', { direction: 'recvonly' })

      const controlChannel = pc.createDataChannel('control', { ordered: true })
      const bulkChannel = pc.createDataChannel('bulk', { ordered: true })
      controlChannelRef.current = controlChannel
      bulkChannelRef.current = bulkChannel

      const signaling = new SignalingClient()
      signalingRef.current = signaling

      // Store sendInput function in Zustand so VideoCanvas can use it
      setSendInputFn((msgType: string, payload: unknown) => {
        if (signaling && signaling.isOpen()) {
          signaling.send({
            type: msgType as SignalingMessage['type'],
            data: {
              v: 1,
              type: msgType,
              ts: Date.now(),
              seq: nextSeq(),
              payload,
            },
          })
        }
      })

      // Wait for WebSocket to open before sending offer/ICE
      await signaling.connect(agentAddress, (msg: SignalingMessage) => {
        handleSignalingMessage(pc, msg, pendingICE)
      })

      pc.onicecandidate = (event) => {
        if (event.candidate) {
          signaling.send({
            type: 'ice',
            data: {
              candidate: event.candidate.candidate,
              sdpMid: event.candidate.sdpMid ?? '',
              sdpMLineIndex: event.candidate.sdpMLineIndex ?? 0,
            } satisfies ICEPayload,
          })
        }
      }

      const offer = await pc.createOffer()
      await pc.setLocalDescription(offer)
      signaling.send({
        type: 'offer',
        data: { sdp: offer.sdp ?? '', type: offer.type } satisfies SDPPayload,
      })
    } catch (err) {
      setError(`连接失败: ${err instanceof Error ? err.message : String(err)}`)
    }
  }, [agentAddress, setConnectionState, setMediaStream, setError, nextSeq, setSendInputFn])

  const disconnect = useCallback(() => {
    controlChannelRef.current?.close()
    bulkChannelRef.current?.close()
    pcRef.current?.close()
    signalingRef.current?.disconnect()
    pcRef.current = null
    signalingRef.current = null
    controlChannelRef.current = null
    bulkChannelRef.current = null
    reset()
  }, [reset])

  return { connect, disconnect }
}

async function handleSignalingMessage(
  pc: RTCPeerConnection,
  msg: SignalingMessage,
  pendingICE: ICEPayload[],
) {
  switch (msg.type) {
    case 'answer': {
      const data = msg.data as SDPPayload
      await pc.setRemoteDescription(new RTCSessionDescription({
        sdp: data.sdp,
        type: data.type as RTCSdpType,
      }))
      // Apply any queued ICE candidates
      for (const ice of pendingICE) {
        await pc.addIceCandidate(new RTCIceCandidate({
          candidate: ice.candidate,
          sdpMid: ice.sdpMid,
          sdpMLineIndex: ice.sdpMLineIndex,
        }))
      }
      pendingICE.length = 0
      break
    }
    case 'ice': {
      const data = msg.data as ICEPayload
      if (pc.remoteDescription) {
        await pc.addIceCandidate(new RTCIceCandidate({
          candidate: data.candidate,
          sdpMid: data.sdpMid,
          sdpMLineIndex: data.sdpMLineIndex,
        }))
      } else {
        // Queue until remote description is set
        pendingICE.push(data)
      }
      break
    }
  }
}
