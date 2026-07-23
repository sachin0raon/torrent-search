import { useState } from 'react';

// Copies a magnet link to the clipboard and shows a transient "Copied" state.
export default function CopyButton({ value, label = 'Copy magnet', className = 'btn-solid' }) {
  const [copied, setCopied] = useState(false);

  async function onCopy() {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Fallback: no-op if clipboard is unavailable.
    }
  }

  return (
    <button className={className} onClick={onCopy} disabled={!value}>
      {copied ? 'Copied ✓' : label}
    </button>
  );
}
