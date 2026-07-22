// Non-blocking, dismissible error banner for per-source failures.
export default function ErrorBanner({ message, onDismiss }) {
  if (!message) return null;
  return (
    <div className="error-banner" role="alert">
      <span>{message}</span>
      {onDismiss ? (
        <button className="link" onClick={onDismiss} aria-label="Dismiss">
          Dismiss
        </button>
      ) : null}
    </div>
  );
}
