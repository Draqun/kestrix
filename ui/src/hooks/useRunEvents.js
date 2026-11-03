import { useEffect, useRef, useState } from 'react';

const API_BASE = import.meta.env?.VITE_API_URL || '';

// Event names this hook listens for, each paired with the handler prop
// that receives its parsed data - status/task/trace/complete repeat the
// same check-active/JSON.parse/dispatch block, so a single loop replaces
// four copies of it (see REVIEW.md R11).
const EVENT_HANDLER_PROPS = {
  status: 'onStatus',
  task: 'onTask',
  trace: 'onTrace',
  complete: 'onComplete',
};

/**
 * useRunEvents manages a Server-Sent Events (SSE) connection to stream live
 * run status, task transitions, and trace output chunks.
 *
 * Automatically marks isFallback true if EventSource is unsupported,
 * fails to connect, or drops.
 */
export default function useRunEvents({
  projectName,
  runId,
  enabled = true,
  onOpen,
  onStatus,
  onTask,
  onTrace,
  onComplete,
  onError,
}) {
  const [isConnected, setIsConnected] = useState(false);
  const [isFallback, setIsFallback] = useState(false);

  const handlersRef = useRef({});
  handlersRef.current = { onOpen, onStatus, onTask, onTrace, onComplete, onError };

  useEffect(() => {
    if (!enabled || !projectName || !runId) {
      setIsConnected(false);
      return;
    }

    if (typeof window === 'undefined' || typeof window.EventSource === 'undefined') {
      setIsConnected(false);
      setIsFallback(true);
      return;
    }

    let active = true;
    const url = `${API_BASE}/api/projects/${encodeURIComponent(projectName)}/runs/${runId}/events`;
    let es;
    try {
      es = new window.EventSource(url, { withCredentials: true });
    } catch (err) {
      setIsConnected(false);
      setIsFallback(true);
      handlersRef.current.onError?.(err);
      return;
    }

    es.onopen = () => {
      if (!active) return;
      setIsConnected(true);
      setIsFallback(false);
      // Events emitted before this subscribe registered, or during any gap
      // this open is recovering from (a slow-consumer drop, the write-
      // timeout fix, a network blip), are never replayed - there is no
      // Last-Event-ID or buffer. onOpen lets the caller resync from the
      // REST snapshot on every open and reopen, not just the first one
      // (see REVIEW.md R5).
      handlersRef.current.onOpen?.();
    };

    for (const [eventName, propName] of Object.entries(EVENT_HANDLER_PROPS)) {
      es.addEventListener(eventName, (e) => {
        if (!active) return;
        let data;
        try {
          data = JSON.parse(e.data);
        } catch (err) {
          console.error(`Failed to parse SSE ${eventName} event`, err);
          return;
        }
        handlersRef.current[propName]?.(data);
        if (eventName === 'complete') {
          es.close();
          setIsConnected(false);
        }
      });
    }

    es.onerror = (e) => {
      if (!active) return;
      setIsConnected(false);
      setIsFallback(true);
      handlersRef.current.onError?.(e);
      if (es.readyState === window.EventSource.CLOSED) {
        es.close();
      }
    };

    return () => {
      active = false;
      es.close();
      setIsConnected(false);
    };
  }, [enabled, projectName, runId]);

  return { isConnected, isFallback };
}
