import { createContext, useCallback, useContext, useMemo, useState } from 'react';

const SessionContext = createContext(null);

export function SessionProvider({ children }) {
  const [sessions, setSessions] = useState(new Map());

  const register = useCallback((sessionId, name) => {
    setSessions((prev) => new Map(prev).set(sessionId, { sessionId, name }));
  }, []);

  const unregister = useCallback((sessionId) => {
    setSessions((prev) => {
      const next = new Map(prev);
      next.delete(sessionId);
      return next;
    });
  }, []);

  const value = useMemo(
    () => ({ sessions, register, unregister }),
    [sessions, register, unregister],
  );

  return (
    <SessionContext.Provider value={value}>
      {children}
    </SessionContext.Provider>
  );
}

export function useSessions() {
  return useContext(SessionContext);
}
