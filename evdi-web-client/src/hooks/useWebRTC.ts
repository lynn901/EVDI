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
    reset,
  } = useConnectionStore()

  const connect = useCallback(async () => {
    try {
      setConnectionState('connecting')
      setError(null)

      const pc = new RTCPeerConnection({ iceServers: [] })
      pcRef.current = pc

      pc.ontrack = (event) => {
        if (event.streams.length > 0) {
          setMediaStream(event.streams[0])
        }
      }

      pc.onconnectionstatechange = () => {
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

      pc.addTransceiver('video', { direction: 'recvonly' })
      pc.addTransceiver('audio', { direction: 'recvonly' })

      const controlChannel = pc.createDataChannel('control', { ordered: true })
      const bulkChannel = pc.createDataChannel('bulk', { ordered: true })
      controlChannelRef.current = controlChannel
      bulkChannelRef.current = bulkChannel

      const signaling = new SignalingClient()
      signalingRef.current = signaling

      signaling.connect(agentAddress, (msg: SignalingMessage) => {
        handleSignalingMessage(pc, signaling, msg)
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
  }, [agentAddress, setConnectionState, setMediaStream, setError, nextSeq])

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

  const sendDataChannelMessage = useCallback((channel: 'control' | 'bulk', msgType: string, payload: unknown) => {
    const ch = channel === 'control' ? controlChannelRef.current : bulkChannelRef.current
    if (ch?.readyState === 'open') {
      ch.send(JSON.stringify({
        v: 1,
        type: msgType,
        ts: Date.now(),
        seq: nextSeq(),
        payload,
      }))
    }
  }, [nextSeq])

  return { connect, disconnect, sendDataChannelMessage }
}

async function handleSignalingMessage(
  pc: RTCPeerConnection,
  _signaling: SignalingClient,
  msg: SignalingMessage,
) {
  switch (msg.type) {
    case 'answer': {
      const data = msg.data as SDPPayload
      await pc.setRemoteDescription(new RTCSessionDescription({
        sdp: data.sdp,
        type: data.type as RTCSdpType,
      }))
      break
    }
    case 'ice': {
      const data = msg.data as ICEPayload
      await pc.addIceCandidate(new RTCIceCandidate({
        candidate: data.candidate,
        sdpMid: data.sdpMid,
        sdpMLineIndex: data.sdpMLineIndex,
      }))
      break
    }
  }
}
