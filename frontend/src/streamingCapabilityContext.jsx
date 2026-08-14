import { createContext, useContext, useEffect, useState } from 'react';
import { api } from './api/client.js';

const StreamingCapabilityContext = createContext(true);

export function StreamingCapabilityProvider({ children }) {
  const [enabled, setEnabled] = useState(true);

  useEffect(() => {
    let active = true;
    api
      .getConfig()
      .then((c) => {
        if (active && c?.enable_streaming !== undefined) {
          setEnabled(!!c.enable_streaming);
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  return (
    <StreamingCapabilityContext.Provider value={enabled}>
      {children}
    </StreamingCapabilityContext.Provider>
  );
}

export function useStreamingEnabled() {
  return useContext(StreamingCapabilityContext);
}
