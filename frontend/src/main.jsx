import React from 'react';
import { createRoot } from 'react-dom/client';
import App from './App.jsx';
import { SessionProvider } from './sessionContext.jsx';
import { DownloadCapabilityProvider } from './downloadCapabilityContext.jsx';
import { StreamingCapabilityProvider } from './streamingCapabilityContext.jsx';
import './styles.css';

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <SessionProvider>
      <DownloadCapabilityProvider>
        <StreamingCapabilityProvider>
          <App />
        </StreamingCapabilityProvider>
      </DownloadCapabilityProvider>
    </SessionProvider>
  </React.StrictMode>,
);
