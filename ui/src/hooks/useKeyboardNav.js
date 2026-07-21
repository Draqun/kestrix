import { useEffect, useRef } from 'react';

const KEY_MAP = {
  g: {
    d: '/',
    p: '/pipelines',
    r: '/projects',
    w: '/webhooks',
  },
};

export default function useKeyboardNav(navigate) {
  const buffer = useRef('');

  useEffect(() => {
    const handler = (e) => {
      if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.isContentEditable) return;

      buffer.current += e.key.toLowerCase();

      if (buffer.current.length > 2) {
        buffer.current = buffer.current.slice(-2);
      }

      if (buffer.current.length === 2) {
        const first = buffer.current[0];
        const second = buffer.current[1];
        const route = KEY_MAP[first]?.[second];
        if (route) {
          navigate(route);
          buffer.current = '';
        }
      }

      if (buffer.current.length === 2 && !KEY_MAP[buffer.current[0]]) {
        buffer.current = buffer.current.slice(-1);
      }
    };

    window.addEventListener('keydown', handler);
    return () => window.removeEventListener('keydown', handler);
  }, [navigate]);
}
