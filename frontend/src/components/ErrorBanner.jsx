import { AlertCircle, X } from 'lucide-react';

// Non-blocking, dismissible error banner for per-source failures.
export default function ErrorBanner({ message, onDismiss }) {
  if (!message) return null;
  return (
    <div className="error-banner" role="alert">
      <AlertCircle size={15} style={{ flex: 'none' }} />
      <span>{message}</span>
      {onDismiss ? (
        <button className="link" onClick={onDismiss} aria-label="Dismiss">
          <X size={14} />
        </button>
      ) : null}
    </div>
  );
}
