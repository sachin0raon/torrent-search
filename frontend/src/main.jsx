import React from 'react';
import { createRoot } from 'react-dom/client';
import { AuthProvider } from './auth/AuthContext.jsx';
import AuthRoot from './auth/AuthRoot.jsx';
import { SessionProvider } from './sessionContext.jsx';
import { DownloadCapabilityProvider } from './downloadCapabilityContext.jsx';
import { StreamingCapabilityProvider } from './streamingCapabilityContext.jsx';
import { QbtActiveStreamsProvider } from './qbtActiveStreamsContext.jsx';
import './styles.css';

createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <AuthProvider>
      <SessionProvider>
        <DownloadCapabilityProvider>
          <StreamingCapabilityProvider>
            <QbtActiveStreamsProvider>
              <AuthRoot />
            </QbtActiveStreamsProvider>
          </StreamingCapabilityProvider>
        </DownloadCapabilityProvider>
      </SessionProvider>
    </AuthProvider>
  </React.StrictMode>,
);
